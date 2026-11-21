// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventstate"
	"github.com/nats-io/nats.go/jetstream"
)

type deliveryFence struct {
	revision   uint64
	latest     func(context.Context) (uint64, error)
	superseded atomic.Bool
}

func (f *deliveryFence) Current(ctx context.Context) (bool, error) {
	if f.superseded.Load() {
		return false, nil
	}
	latest, err := f.latest(ctx)
	if err != nil {
		return false, domain.NewUnavailableError("source revision unavailable")
	}
	if latest < f.revision {
		return false, domain.NewUnavailableError("source revision cannot prove current delivery")
	}
	if latest > f.revision {
		f.superseded.Store(true)
		return false, nil
	}
	return true, nil
}

func (ep *EventProcessor) processMessage(ctx context.Context, msg jetstream.Msg) {
	metadata, err := msg.Metadata()
	if err != nil || metadata == nil || metadata.Sequence.Stream == 0 {
		ep.logger.WarnContext(ctx, "source revision unavailable; retrying")
		_ = msg.NakWithDelay(20 * time.Second)
		return
	}
	stop := ep.keepAlive(ctx, msg)
	defer stop()
	msg = progressMessage{Msg: msg, stop: stop}
	key := strings.TrimPrefix(msg.Subject(), "$KV."+V1ObjectsBucket+".")
	locker, ok := ep.state.(domain.ResourceLocker)
	if !ok {
		ep.logger.With("key", key).WarnContext(ctx, "resource locking unavailable; retrying")
		_ = msg.NakWithDelay(20 * time.Second)
		return
	}
	lockedCtx, release, err := locker.LockResource(ctx, key)
	if err != nil {
		ep.logger.With("key", key, "error", err).WarnContext(ctx, "resource lock unavailable; retrying")
		_ = msg.NakWithDelay(20 * time.Second)
		return
	}
	defer release()
	fence := &deliveryFence{revision: metadata.Sequence.Stream, latest: func(ctx context.Context) (uint64, error) {
		latest, err := eventstate.ReadSourceMessage(ctx, ep.jsInstance, ep.config.StreamName, 0, "", msg.Subject())
		if err != nil {
			return 0, err
		}
		return latest.Sequence, nil
	}}
	ctx = domain.WithSourceFence(lockedCtx, fence)
	if err := domain.CheckSourceFence(ctx); err != nil {
		if errors.Is(err, domain.ErrSourceSuperseded) {
			_ = msg.Ack()
		} else {
			ep.logger.With("key", key, "error", err).WarnContext(ctx, "source freshness unavailable; retrying")
			_ = msg.NakWithDelay(20 * time.Second)
		}
		return
	}
	kvMessageHandler(ctx, msg, ep.syncer, ep.logger)
}

// keepAlive covers lock/source I/O and joins before ACK/NAK or shutdown.
func (ep *EventProcessor) keepAlive(ctx context.Context, msg jetstream.Msg) func() {
	done := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done); <-exited }) }
	interval := ep.config.AckWait / 3
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		defer close(exited)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := msg.InProgress(); err != nil {
					ep.logger.With("subject", msg.Subject(), "error", err).WarnContext(ctx, "failed to extend active source delivery")
				}
			}
		}
	}()
	return stop
}

type progressMessage struct {
	jetstream.Msg
	stop func()
}

func (m progressMessage) Ack() error { m.stop(); return m.Msg.Ack() }
func (m progressMessage) NakWithDelay(delay time.Duration) error {
	m.stop()
	return m.Msg.NakWithDelay(delay)
}
