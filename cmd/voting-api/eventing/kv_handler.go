// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

// kvEntry implements a mock jetstream.KeyValueEntry interface for the handler
type kvEntry struct {
	key       string
	value     []byte
	operation jetstream.KeyValueOp
}

func (e *kvEntry) Key() string {
	return e.key
}

func (e *kvEntry) Value() []byte {
	return e.value
}

func (e *kvEntry) Operation() jetstream.KeyValueOp {
	return e.operation
}

func (e *kvEntry) Bucket() string {
	return "v1-objects"
}

func (e *kvEntry) Created() time.Time {
	return time.Now()
}

func (e *kvEntry) Delta() uint64 {
	return 0
}

func (e *kvEntry) Revision() uint64 {
	return 0
}

// kvMessageHandler processes KV update messages from the consumer
func kvMessageHandler(
	ctx context.Context,
	msg jetstream.Msg,
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) {
	// Parse the message as a KV entry
	headers := msg.Headers()
	subject := msg.Subject()

	// Extract key from the subject ($KV.v1-objects.{key})
	key := ""
	if len(subject) > len("$KV.v1-objects.") {
		key = subject[len("$KV.v1-objects."):]
	}

	// Determine operation from headers
	operation := jetstream.KeyValuePut // Default to PUT
	if opHeader := headers.Get("KV-Operation"); opHeader != "" {
		switch opHeader {
		case "DEL":
			operation = jetstream.KeyValueDelete
		case "PURGE":
			operation = jetstream.KeyValuePurge
		}
	}

	// Create a mock KV entry for the handler
	entry := &kvEntry{
		key:       key,
		value:     msg.Data(),
		operation: operation,
	}

	// Process the KV entry and check if retry is needed
	shouldRetry := kvHandler(ctx, entry, publisher, idMapper, mappingsKV, logger)

	// Handle message acknowledgment based on retry decision
	if shouldRetry {
		// NAK the message to trigger retry
		if err := msg.Nak(); err != nil {
			logger.With("error", err, "key", key).Error("failed to NAK KV JetStream message for retry")
		} else {
			logger.With("key", key).Debug("NAKed KV message for retry")
		}
	} else {
		// Acknowledge the message
		if err := msg.Ack(); err != nil {
			logger.With("error", err, "key", key).Error("failed to acknowledge KV JetStream message")
		}
	}
}

// kvHandler routes KV entries by operation type
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK)
func kvHandler(
	ctx context.Context,
	entry jetstream.KeyValueEntry,
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	switch entry.Operation() {
	case jetstream.KeyValuePut:
		return handleKVPut(ctx, entry, publisher, idMapper, mappingsKV, logger)
	case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
		return handleKVDelete(ctx, entry, logger)
	default:
		logger.With("key", entry.Key(), "operation", entry.Operation()).Debug("ignoring unknown KV operation")
		return false // ACK unknown operations
	}
}

// handleKVPut processes PUT operations by routing to specific handlers based on key prefix
func handleKVPut(
	ctx context.Context,
	entry jetstream.KeyValueEntry,
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	key := entry.Key()
	value := entry.Value()

	// Unmarshal the data
	var v1Data map[string]interface{}
	if err := json.Unmarshal(value, &v1Data); err != nil {
		logger.With("error", err, "key", key).Error("failed to unmarshal KV data")
		return false // Permanent error, ACK and skip
	}

	// Extract key prefix (before first period)
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 0 {
		logger.With("key", key).Warn("invalid key format")
		return false // ACK invalid keys
	}
	prefix := parts[0]

	// Route to specific handlers based on prefix
	switch prefix {
	case "itx-poll":
		return handleVoteUpdate(ctx, key, v1Data, publisher, idMapper, mappingsKV, logger)
	case "itx-poll-vote":
		return handleVoteResponseUpdate(ctx, key, v1Data, publisher, idMapper, mappingsKV, logger)
	default:
		// Not a voting-related key, ACK and skip
		return false
	}
}

// handleKVDelete processes DELETE and PURGE operations
func handleKVDelete(
	ctx context.Context,
	entry jetstream.KeyValueEntry,
	logger *slog.Logger,
) bool {
	key := entry.Key()
	logger.With("key", key, "operation", entry.Operation()).Debug("received delete/purge operation")

	// For now, we don't process deletes - just ACK them
	// TODO: In the future, we could send delete events to indexer/FGA
	return false
}
