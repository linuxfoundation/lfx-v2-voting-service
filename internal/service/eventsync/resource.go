// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/vmihailenco/msgpack/v5"
)

// resourceHandler syncs one kind of v1 resource. It is the internal seam the Syncer
// routes through; each implementation is a resource[T] built from a spec.
type resourceHandler interface {
	// update syncs a decoded, non-deleted record.
	update(ctx context.Context, s *Syncer, key string, v1Data map[string]any) Outcome
	// remove syncs a delete. value is the raw record, empty for hard deletes and purges.
	remove(ctx context.Context, s *Syncer, uid string, value []byte) Outcome
}

// resource describes one kind of v1 resource. The shared update and delete flows in
// update and remove are written once; everything that differs between resources is a
// field here, so each divergence is visible as explicit policy.
type resource[T any] struct {
	// label names the resource in log messages, e.g. "vote response".
	label string
	// handlerName is the "handler" log attribute for updates; deletes append "_delete".
	handlerName string
	// kind is the mapping store kind.
	kind domain.MappingKind
	// uidLogKey is the log attribute that carries the resource UID.
	uidLogKey string
	// deleteUIDLogKey overrides uidLogKey for deletes; empty means use uidLogKey.
	deleteUIDLogKey string

	// convert turns a decoded v1 record into v2 data, mapping IDs on the way.
	convert func(ctx context.Context, v1Data map[string]any, idMapper domain.IDMapper, logger *slog.Logger) (*T, error)
	// retryOnConvertError reports whether a conversion error may be retried. When false,
	// every conversion error is permanent. When true, only transient errors are retried.
	retryOnConvertError bool
	// uid returns the resource UID from converted data; empty means the record is invalid.
	uid func(*T) string
	// missingUIDMessage is logged when uid returns empty.
	missingUIDMessage string

	// prepare runs the resource's own rules after the UID check and before the
	// create-vs-update decision: parent ordering, privacy, required fields. A non-nil
	// outcome ends the flow with that outcome.
	prepare func(ctx context.Context, s *Syncer, logger *slog.Logger, item *T) *Outcome

	// tombstoneIsCreate makes a tombstoned mapping count as a create. When false, any
	// existing mapping, tombstoned or not, counts as an update.
	tombstoneIsCreate bool

	// publish sends the item to the indexer (and FGA-sync where applicable).
	publish func(ctx context.Context, pub domain.EventPublisher, action string, item *T) error
	// afterPublish runs best-effort work after a successful publish and mapping write.
	afterPublish func(ctx context.Context, s *Syncer, logger *slog.Logger, action indexerConstants.MessageAction, item *T)

	// deleteItem builds the minimal item published for a delete. value is the raw
	// record, empty for hard deletes and purges.
	deleteItem func(ctx context.Context, logger *slog.Logger, uid string, value []byte) *T
}

func outcome(o Outcome) *Outcome { return &o }

func (r *resource[T]) update(ctx context.Context, s *Syncer, key string, v1Data map[string]any) Outcome {
	d := s.deps
	logger := d.Logger.With("key", key, "handler", r.handlerName)
	logger.DebugContext(ctx, fmt.Sprintf("processing %s update", r.label))

	item, err := r.convert(ctx, v1Data, d.IDMapper, logger)
	if err != nil {
		logger.With(errKey, err).ErrorContext(ctx, fmt.Sprintf("failed to convert v1Data to %s", r.label))
		if r.retryOnConvertError && isTransientError(err) {
			return Retry
		}
		return Ack
	}

	uid := r.uid(item)
	if uid == "" {
		logger.ErrorContext(ctx, r.missingUIDMessage)
		return Ack
	}
	logger = logger.With(r.uidLogKey, uid)

	if o := r.prepare(ctx, s, logger, item); o != nil {
		return *o
	}

	state := d.Mappings.State(ctx, r.kind, uid)
	exists := state == domain.MappingSynced || (state == domain.MappingTombstoned && !r.tombstoneIsCreate)
	action := indexerConstants.ActionCreated
	if exists {
		action = indexerConstants.ActionUpdated
	}

	if err := r.publish(ctx, d.Publisher, string(action), item); err != nil {
		logger.With(errKey, err).ErrorContext(ctx, fmt.Sprintf("failed to publish %s event", r.label))
		return retryIfTransient(err)
	}

	if err := d.Mappings.MarkSynced(ctx, r.kind, uid); err != nil {
		// Don't retry on mapping storage failures.
		logger.With(errKey, err).WarnContext(ctx, fmt.Sprintf("failed to store %s mapping", r.label))
	}

	if r.afterPublish != nil {
		r.afterPublish(ctx, s, logger, action, item)
	}

	logger.InfoContext(ctx, fmt.Sprintf("successfully synced %s", r.label))
	return Ack
}

func (r *resource[T]) remove(ctx context.Context, s *Syncer, uid string, value []byte) Outcome {
	d := s.deps
	uidKey := r.uidLogKey
	if r.deleteUIDLogKey != "" {
		uidKey = r.deleteUIDLogKey
	}
	logger := d.Logger.With(uidKey, uid, "handler", r.handlerName+"_delete")
	logger.DebugContext(ctx, fmt.Sprintf("processing %s delete", r.label))

	// Skip if already tombstoned: prevents duplicate delete events on redelivery.
	if d.Mappings.State(ctx, r.kind, uid) == domain.MappingTombstoned {
		logger.DebugContext(ctx, fmt.Sprintf("%s delete already processed, skipping", r.label))
		return Ack
	}

	item := r.deleteItem(ctx, logger, uid, value)
	if err := r.publish(ctx, d.Publisher, string(indexerConstants.ActionDeleted), item); err != nil {
		logger.With(errKey, err).ErrorContext(ctx, fmt.Sprintf("failed to publish %s delete event", r.label))
		return retryIfTransient(err)
	}

	// Tombstone the mapping instead of removing it, so redelivery is safely skipped.
	if err := d.Mappings.MarkTombstoned(ctx, r.kind, uid); err != nil {
		// Don't retry on mapping failures.
		logger.With(errKey, err).WarnContext(ctx, fmt.Sprintf("failed to tombstone %s mapping", r.label))
	}

	logger.InfoContext(ctx, fmt.Sprintf("successfully synced %s delete", r.label))
	return Ack
}

func retryIfTransient(err error) Outcome {
	if isTransientError(err) {
		return Retry
	}
	return Ack
}

// isTransientError determines if an error is transient and should be retried.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	// Domain errors with ErrorTypeUnavailable (e.g. from IDMapper) are always transient.
	if domain.GetErrorType(err) == domain.ErrorTypeUnavailable {
		return true
	}

	errStr := err.Error()
	// NATS publish errors, timeouts, connection issues
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "unavailable") || strings.Contains(errStr, "deadline") {
		return true
	}

	return false
}

// DecodeRecord unmarshals a KV record as JSON first, then msgpack.
func DecodeRecord(data []byte) (map[string]any, error) {
	var jsonResult map[string]any
	jsonErr := json.Unmarshal(data, &jsonResult)
	if jsonErr == nil {
		return jsonResult, nil
	}

	var msgpackResult map[string]any
	msgpackErr := msgpack.Unmarshal(data, &msgpackResult)
	if msgpackErr == nil {
		return msgpackResult, nil
	}

	return nil, fmt.Errorf("failed to decode KV data as JSON or msgpack: json: %w; msgpack: %w", jsonErr, msgpackErr)
}
