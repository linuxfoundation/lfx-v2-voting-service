// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/mappings"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service/eventsync"
	"github.com/nats-io/nats.go/jetstream"
)

// errKey is the log attribute name for errors.
const errKey = "error"

const (
	V1ObjectsBucket = "v1-objects"

	// TombstoneMarker is written to a v1-mappings key after successful deletion,
	// preventing duplicate delete events if the same KV delete is redelivered.
	// It is exported for tools that read v1-mappings.
	TombstoneMarker = mappings.TombstoneMarker
)

// kvMessageHandler is the NATS adapter in front of the sync module. It turns a JetStream
// message into an eventsync.Entry, then acknowledges the message or NAKs it with
// exponential backoff according to the Outcome.
func kvMessageHandler(
	ctx context.Context,
	msg jetstream.Msg,
	syncer *eventsync.Syncer,
	logger *slog.Logger,
) {
	headers := msg.Headers()
	subject := msg.Subject()

	// Extract key from the subject ($KV.{bucket}.{key})
	key := ""
	if len(subject) > len(fmt.Sprintf("$KV.%s.", V1ObjectsBucket)) {
		key = subject[len(fmt.Sprintf("$KV.%s.", V1ObjectsBucket)):]
	}

	// Determine operation from headers
	operation := eventsync.OpPut // Default to PUT
	switch headers.Get("KV-Operation") {
	case "DEL", "PURGE":
		operation = eventsync.OpDelete
	}

	outcome := syncer.Sync(ctx, eventsync.Entry{Key: key, Op: operation, Value: msg.Data()})

	if outcome == eventsync.Retry {
		// Get message metadata to determine retry attempt number
		metadata, err := msg.Metadata()
		if err != nil {
			logger.With("error", err, "key", key).Warn("failed to get message metadata, using default delay")
			metadata = &jetstream.MsgMetadata{NumDelivered: 1}
		}

		// Calculate exponential backoff delay based on delivery attempt
		// Attempts: 1st retry = 2s, 2nd retry = 10s, 3rd+ retry = 20s
		var delay time.Duration
		switch metadata.NumDelivered {
		case 1:
			delay = 2 * time.Second
		case 2:
			delay = 10 * time.Second
		default:
			// This case won't be hit if there is a consumer max delivery of 3 or less
			delay = 20 * time.Second
		}

		// NAK the message with exponential backoff delay
		// This allows time for parent objects (e.g., votes) to be stored before retrying child objects (e.g., vote responses)
		if err := msg.NakWithDelay(delay); err != nil {
			logger.With("error", err, "key", key).Error("failed to NAK KV JetStream message for retry")
		} else {
			logger.With("key", key, "attempt", metadata.NumDelivered, "delay_seconds", delay.Seconds()).Debug("NAKed KV message for retry with exponential backoff")
		}
	} else {
		// Acknowledge the message
		if err := msg.Ack(); err != nil {
			logger.With("error", err, "key", key).Error("failed to acknowledge KV JetStream message")
		}
	}
}
