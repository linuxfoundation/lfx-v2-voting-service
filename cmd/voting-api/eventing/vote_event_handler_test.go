// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEventPublisher is a mock implementation of EventPublisher for testing
type mockEventPublisher struct {
	publishVoteErr         error
	publishVoteResponseErr error
	publishedVotes         []*domain.VoteData
	publishedVoteResponses []*domain.VoteResponseData
}

func (m *mockEventPublisher) PublishVoteEvent(ctx context.Context, action string, vote *domain.VoteData) error {
	m.publishedVotes = append(m.publishedVotes, vote)
	return m.publishVoteErr
}

func (m *mockEventPublisher) PublishVoteResponseEvent(ctx context.Context, action string, voteResponse *domain.VoteResponseData) error {
	m.publishedVoteResponses = append(m.publishedVoteResponses, voteResponse)
	return m.publishVoteResponseErr
}

func (m *mockEventPublisher) Close() error {
	return nil
}

// mockUserLookup is a mock implementation of V1UserLookup for testing
type mockUserLookup struct {
	authSub string
	err     error
}

func (m *mockUserLookup) MapUsernameToAuthSub(_ context.Context, _ string) (string, error) {
	return m.authSub, m.err
}

func TestConvertMapToVoteData(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successful conversion with all fields", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":                          "poll-123",
			"name":                             "Test Vote",
			"description":                      "Test Description",
			"creation_time":                    "2024-01-01T00:00:00Z",
			"last_modified_time":               "2024-01-02T00:00:00Z",
			"end_time":                         "2024-12-31T23:59:59Z",
			"status":                           "active",
			"project_id":                       "project-sfid",
			"project_name":                     "Test Project",
			"committee_id":                     "committee-sfid",
			"committee_name":                   "Test Committee",
			"committee_type":                   "technical",
			"committee_voting_status":          true,
			"committee_filters":                []interface{}{"filter1", "filter2"},
			"total_voting_request_invitations": "10",
			"num_response_received":            "5",
			"poll_type":                        "single_choice",
			"pseudo_anonymity":                 false,
			"num_winners":                      "1",
			"allow_abstain":                    true,
			"poll_questions":                   []interface{}{},
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)

		assert.Equal(t, "poll-123", result.VoteUID)
		assert.Equal(t, "poll-123", result.PollID)
		assert.Equal(t, "Test Vote", result.Name)
		assert.Equal(t, "Test Description", result.Description)
		assert.Equal(t, "active", result.Status)
		assert.Equal(t, "project-sfid", result.ProjectID)
		assert.Equal(t, "project-sfid", result.ProjectUID) // NoOp mapper returns same value
		assert.Equal(t, "committee-sfid", result.CommitteeID)
		assert.Equal(t, "committee-sfid", result.CommitteeUID) // NoOp mapper returns same value
		assert.Equal(t, 10, result.TotalVotingRequestInvitations)
		assert.Equal(t, 5, result.NumResponseReceived)
		assert.Equal(t, 1, result.NumWinners)
		assert.True(t, result.AllowAbstain)
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":    "poll-456",
			"name":       "Minimal Vote",
			"status":     "draft",
			"project_id": "project-sfid",
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)

		assert.Equal(t, "poll-456", result.VoteUID)
		assert.Equal(t, "Minimal Vote", result.Name)
		assert.Equal(t, 0, result.TotalVotingRequestInvitations)
		assert.Equal(t, 0, result.NumResponseReceived)
	})

	t.Run("returns error for invalid numeric string", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":                          "poll-789",
			"name":                             "Test Vote",
			"project_id":                       "project-sfid",
			"total_voting_request_invitations": "invalid",
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid syntax")
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id": make(chan int), // Channels can't be marshaled to JSON
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to marshal v1Data to JSON")
	})
}

func TestHandleVoteUpdate(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successfully processes vote update", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id":        "poll-123",
			"name":           "Test Vote",
			"status":         "active",
			"project_id":     "project-sfid",
			"poll_questions": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteUpdate(ctx, "itx-poll.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
		assert.Equal(t, "poll-123", mockPublisher.publishedVotes[0].VoteUID)
	})

	t.Run("returns false for conversion error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id": make(chan int), // Invalid for JSON
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteUpdate(ctx, "itx-poll.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
		assert.Len(t, mockPublisher.publishedVotes, 0)
	})

	t.Run("returns false when project UID is missing", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id":        "poll-123",
			"name":           "Test Vote",
			"poll_questions": []interface{}{},
			// Missing project_id
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteUpdate(ctx, "itx-poll.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
		assert.Len(t, mockPublisher.publishedVotes, 0)
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id":        "poll-123",
			"name":           "Test Vote",
			"project_id":     "project-sfid",
			"poll_questions": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{
			publishVoteErr: errors.New("timeout error"),
		}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteUpdate(ctx, "itx-poll.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.True(t, shouldRetry) // Transient error, NAK for retry
	})
}

func TestHandleVoteDelete(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successfully processes vote delete", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		// Store a mapping first
		_, err := mappingsKV.Put(context.Background(), "vote.poll-123", []byte("1"))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 1)
		assert.Equal(t, "poll-123", mockPublisher.publishedVotes[0].VoteUID)

		// Verify mapping is tombstoned (not hard-deleted)
		entry, err := mappingsKV.Get(context.Background(), "vote.poll-123")
		require.NoError(t, err)
		assert.True(t, isTombstonedMapping(entry.Value()))
	})

	t.Run("skips publish when vote mapping is already tombstoned", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		// Pre-tombstone the mapping to simulate a redelivered delete
		_, err := mappingsKV.Put(context.Background(), "vote.poll-123", []byte(tombstoneMarker))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVotes, 0) // No duplicate event published
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		mockPublisher := &mockEventPublisher{
			publishVoteErr: errors.New("connection timeout"),
		}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.True(t, shouldRetry) // Transient error, NAK for retry
	})
}

func TestIsTransientError(t *testing.T) {
	t.Run("returns false for nil error", func(t *testing.T) {
		result := isTransientError(nil)
		assert.False(t, result)
	})

	t.Run("returns true for timeout error", func(t *testing.T) {
		err := errors.New("request timeout occurred")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for connection error", func(t *testing.T) {
		err := errors.New("connection refused")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for unavailable error", func(t *testing.T) {
		err := errors.New("service unavailable")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for deadline error", func(t *testing.T) {
		err := errors.New("deadline exceeded")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns false for permanent error", func(t *testing.T) {
		err := errors.New("invalid data format")
		result := isTransientError(err)
		assert.False(t, result)
	})
}
