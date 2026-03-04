// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	V1ObjectsBucket = "v1-objects"
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
	return V1ObjectsBucket
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

	// Extract key from the subject ($KV.{bucket}.{key})
	key := ""
	if len(subject) > len(fmt.Sprintf("$KV.%s.", V1ObjectsBucket)) {
		key = subject[len(fmt.Sprintf("$KV.%s.", V1ObjectsBucket)):]
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
		return handleKVDelete(ctx, entry, publisher, idMapper, mappingsKV, logger)
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
		logger.With("key", key, "prefix", prefix).Debug("skipping update - unsupported type")
		return false
	}
}

// handleKVDelete processes DELETE and PURGE operations
func handleKVDelete(
	ctx context.Context,
	entry jetstream.KeyValueEntry,
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	key := entry.Key()
	logger.With("key", key, "operation", entry.Operation()).Debug("received delete/purge operation")

	// Extract key prefix (before first period)
	parts := strings.SplitN(key, ".", 2)
	if len(parts) < 2 {
		logger.With("key", key).Warn("skipping delete - invalid key format")
		return false // Permanent error, ACK and skip
	}

	prefix := parts[0]
	uid := parts[1] // The UID is everything after the first period

	// Route to appropriate delete handler based on prefix
	switch prefix {
	case "itx-poll":
		return handleVoteDelete(ctx, uid, publisher, mappingsKV, logger)
	case "itx-poll-vote":
		return handleVoteResponseDelete(ctx, uid, publisher, mappingsKV, logger)
	default:
		logger.With("key", key, "prefix", prefix).Debug("skipping delete - unsupported type")
		return false // ACK unsupported types
	}
}
