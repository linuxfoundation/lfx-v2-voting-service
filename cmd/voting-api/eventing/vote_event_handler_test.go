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
	publishVoteErr               error
	publishVoteResponseErr       error
	publishVoteResultErr         error
	publishedVotes               []*domain.VoteData
	publishedVoteActions         []string
	publishedVoteResponses       []*domain.VoteResponseData
	publishedVoteResponseActions []string
	publishedVoteResults         []*domain.PollResultData
	publishedVoteResultActions   []string
}

func (m *mockEventPublisher) PublishVoteEvent(ctx context.Context, action string, vote *domain.VoteData) error {
	m.publishedVotes = append(m.publishedVotes, vote)
	m.publishedVoteActions = append(m.publishedVoteActions, action)
	return m.publishVoteErr
}

func (m *mockEventPublisher) PublishVoteResponseEvent(ctx context.Context, action string, voteResponse *domain.VoteResponseData) error {
	m.publishedVoteResponses = append(m.publishedVoteResponses, voteResponse)
	m.publishedVoteResponseActions = append(m.publishedVoteResponseActions, action)
	return m.publishVoteResponseErr
}

func (m *mockEventPublisher) PublishVoteResultEvent(ctx context.Context, action string, pollResult *domain.PollResultData) error {
	m.publishedVoteResults = append(m.publishedVoteResults, pollResult)
	m.publishedVoteResultActions = append(m.publishedVoteResultActions, action)
	return m.publishVoteResultErr
}

func (m *mockEventPublisher) Close() error {
	return nil
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
			"end_time_timezone":                "America/New_York",
			"early_end_time":                   "2024-12-30T10:00:00Z",
			"status":                           "ended",
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
			"poll_comment_prompts": []interface{}{
				map[string]interface{}{
					"prompt_id": "prompt-1",
					"prompt":    "Why did you vote this way?",
				},
			},
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
		assert.Equal(t, "ended", result.Status)
		assert.Equal(t, "project-sfid", result.ProjectID)
		assert.Equal(t, "project-sfid", result.ProjectUID) // NoOp mapper returns same value
		assert.Equal(t, "committee-sfid", result.CommitteeID)
		assert.Equal(t, "committee-sfid", result.CommitteeUID) // NoOp mapper returns same value
		assert.Equal(t, 10, result.TotalVotingRequestInvitations)
		assert.Equal(t, 5, result.NumResponseReceived)
		assert.Equal(t, 1, result.NumWinners)
		assert.True(t, result.AllowAbstain)
		assert.Equal(t, "2024-12-30T10:00:00Z", result.EarlyEndTime)
		assert.Equal(t, "America/New_York", result.EndTimeTimezone)

		require.Len(t, result.PollCommentPrompts, 1)
		assert.Equal(t, "prompt-1", result.PollCommentPrompts[0].PromptID)
		assert.Equal(t, "Why did you vote this way?", result.PollCommentPrompts[0].Prompt)
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
		assert.Empty(t, result.EarlyEndTime)
		assert.Empty(t, result.EndTimeTimezone)
	})

	t.Run("drops zero-value early_end_time from raw DynamoDB", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":        "poll-zero",
			"name":           "Scheduled Close Vote",
			"status":         "ended",
			"project_id":     "project-sfid",
			"end_time":       "2026-02-15T23:59:59Z",
			"early_end_time": "0001-01-01T00:00:00Z",
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.EarlyEndTime)
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

	t.Run("forwards end_time_timezone from raw DynamoDB record to published VoteData", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id":           "poll-tz",
			"name":              "Timezone Vote",
			"status":            "active",
			"project_id":        "project-sfid",
			"end_time":          "2026-02-15T23:59:59Z",
			"end_time_timezone": "America/New_York",
			"poll_questions":    []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteUpdate(ctx, "itx-poll.poll-tz", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		require.Len(t, mockPublisher.publishedVotes, 1)
		assert.Equal(t, "America/New_York", mockPublisher.publishedVotes[0].EndTimeTimezone)
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

	t.Run("returns true for domain ErrorTypeUnavailable", func(t *testing.T) {
		err := domain.NewUnavailableError("id mapper temporarily overloaded")
		result := isTransientError(err)
		assert.True(t, result)
	})
}
