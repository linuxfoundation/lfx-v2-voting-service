// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func setupTestKV(t *testing.T) (jetstream.KeyValue, func()) {
	ns, natsURL := startTestNATSServer(t)

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create the KV bucket
	_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket: V1MappingsBucket,
	})
	require.NoError(t, err)

	mappingsKV, err := js.KeyValue(context.Background(), V1MappingsBucket)
	require.NoError(t, err)

	cleanup := func() {
		nc.Close()
		ns.Shutdown()
	}

	return mappingsKV, cleanup
}

func TestKvHandler(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("handles PUT operation for vote", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{"poll_id":"poll-123","name":"Test","project_id":"proj-1","poll_questions":[]}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("handles PUT operation for vote response", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Store parent vote in mappings
		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     []byte(`{"vote_id":"vote-123","poll_id":"poll-456","project_id":"proj-1","poll_answers":[]}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
	})

	t.Run("handles DELETE operation for vote", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("handles DELETE operation for vote response", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
	})

	t.Run("ignores unsupported key prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "unsupported-prefix.some-id",
			value:     []byte(`{}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry) // ACK unsupported types
		assert.Len(t, mockPublisher.publishedVotes, 0)
		assert.Len(t, mockPublisher.publishedVoteResponses, 0)
	})

	t.Run("ignores unknown operation", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{}`),
			operation: jetstream.KeyValueOp(99), // Invalid operation
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry) // ACK unknown operations
		assert.Len(t, mockPublisher.publishedVotes, 0)
	})
}

func TestHandleKVPut(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("routes to vote handler for itx-poll prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{"poll_id":"poll-123","name":"Test","project_id":"proj-1","poll_questions":[]}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("routes to vote response handler for itx-poll-vote prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Store parent vote in mappings
		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     []byte(`{"vote_id":"vote-123","poll_id":"poll-456","project_id":"proj-1","poll_answers":[]}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
	})

	t.Run("decodes msgpack-encoded itx-poll entry and routes to vote handler", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		payload := map[string]any{
			"poll_id":        "poll-123",
			"name":           "Test",
			"project_id":     "proj-1",
			"poll_questions": []any{},
		}
		encoded, err := msgpack.Marshal(payload)
		require.NoError(t, err)

		// Verify msgpack bytes are not valid JSON (ensuring fallback path is exercised)
		var jsonCheck map[string]any
		assert.Error(t, json.Unmarshal(encoded, &jsonCheck), "msgpack bytes should not be valid JSON")

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     encoded,
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("decodes msgpack-encoded itx-poll-vote entry and routes to vote response handler", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Store parent vote in mappings
		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		payload := map[string]any{
			"vote_id":      "vote-123",
			"poll_id":      "poll-456",
			"project_id":   "proj-1",
			"poll_answers": []any{},
		}
		encoded, err := msgpack.Marshal(payload)
		require.NoError(t, err)

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     encoded,
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
	})

	t.Run("returns false when both JSON and msgpack decoding fail", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`invalid json`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
		assert.Len(t, mockPublisher.publishedVotes, 0)
	})

	t.Run("returns false for unsupported prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "other-prefix.id-123",
			value:     []byte(`{}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry) // ACK unsupported types
	})

	t.Run("soft delete: non-empty _sdc_deleted_at on itx-poll triggers vote delete", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{"poll_id":"poll-123","name":"Test","project_id":"proj-1","_sdc_deleted_at":"2024-01-01T00:00:00Z"}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		// Should publish a delete event, not an upsert
		assert.Len(t, mockPublisher.publishedVotes, 1)
		assert.Len(t, mockPublisher.publishedVoteResponses, 0)
	})

	t.Run("soft delete: non-empty _sdc_deleted_at on itx-poll-vote triggers vote response delete", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     []byte(`{"vote_id":"vote-123","poll_id":"poll-456","project_id":"proj-1","_sdc_deleted_at":"2024-01-01T00:00:00Z"}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
		assert.Len(t, mockPublisher.publishedVotes, 0)
	})

	t.Run("soft delete: null _sdc_deleted_at does not trigger delete path", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{"poll_id":"poll-123","name":"Test","project_id":"proj-1","poll_questions":[],"_sdc_deleted_at":null}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		// Should be treated as a normal upsert, not a delete
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("soft delete: empty string _sdc_deleted_at does not trigger delete path", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte(`{"poll_id":"poll-123","name":"Test","project_id":"proj-1","poll_questions":[],"_sdc_deleted_at":""}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		userLookup := &mockUserLookup{authSub: "auth0|testuser"}
		shouldRetry := handleKVPut(ctx, entry, mockPublisher, idMapper, userLookup, mappingsKV, logger)

		assert.False(t, shouldRetry)
		// Should be treated as a normal upsert, not a delete
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})
}

func TestHandleKVDelete(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("routes to vote delete handler for itx-poll prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll.poll-123",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleKVDelete(ctx, entry, mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
	})

	t.Run("routes to vote response delete handler for itx-poll-vote prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "itx-poll-vote.vote-123",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleKVDelete(ctx, entry, mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
	})

	t.Run("returns false for invalid key format", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "no-period-key",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleKVDelete(ctx, entry, mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
	})

	t.Run("returns false for unsupported prefix", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		entry := &kvEntry{
			key:       "unsupported.id-123",
			value:     []byte{},
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleKVDelete(ctx, entry, mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry) // ACK unsupported types
	})
}

func TestKvEntry(t *testing.T) {
	t.Run("Key returns the key", func(t *testing.T) {
		entry := &kvEntry{key: "test-key"}
		assert.Equal(t, "test-key", entry.Key())
	})

	t.Run("Value returns the value", func(t *testing.T) {
		value := []byte("test-value")
		entry := &kvEntry{value: value}
		assert.Equal(t, value, entry.Value())
	})

	t.Run("Operation returns the operation", func(t *testing.T) {
		entry := &kvEntry{operation: jetstream.KeyValuePut}
		assert.Equal(t, jetstream.KeyValuePut, entry.Operation())
	})

	t.Run("Bucket returns the bucket name", func(t *testing.T) {
		entry := &kvEntry{}
		assert.Equal(t, V1ObjectsBucket, entry.Bucket())
	})

	t.Run("Created returns a time", func(t *testing.T) {
		entry := &kvEntry{}
		created := entry.Created()
		assert.False(t, created.IsZero())
	})

	t.Run("Delta returns 0", func(t *testing.T) {
		entry := &kvEntry{}
		assert.Equal(t, uint64(0), entry.Delta())
	})

	t.Run("Revision returns 0", func(t *testing.T) {
		entry := &kvEntry{}
		assert.Equal(t, uint64(0), entry.Revision())
	})
}
