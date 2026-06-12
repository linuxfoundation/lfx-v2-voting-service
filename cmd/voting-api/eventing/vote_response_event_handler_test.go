// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertMapToVoteResponseData(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successful conversion with all fields", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"vote_id":            "vote-123",
			"poll_id":            "poll-456",
			"project_id":         "project-sfid",
			"vote_creation_time": "2024-01-01T00:00:00Z",
			"last_modified_time": "2024-06-01T12:00:00Z",
			"user_id":            "user-123",
			"user_email":         "test@example.com",
			"user_role":          "voter",
			"user_name":          "Test User",
			"username":           "testuser",
			"profile_picture":    "https://example.com/pic.jpg",
			"user_voting_status": "eligible",
			"user_org_id":        "org-123",
			"user_org_name":      "Test Org",
			"vote_status":        "submitted",
			"abstained":          false,
			"voter_removed":      false,
			"poll_answers":       []interface{}{},
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)

		assert.Equal(t, "vote-123", result.UID)
		assert.Equal(t, "vote-123", result.VoteID)
		assert.Equal(t, "poll-456", result.VoteUID)
		assert.Equal(t, "poll-456", result.PollID)
		assert.Equal(t, "project-sfid", result.ProjectID)
		assert.Equal(t, "project-sfid", result.ProjectUID) // NoOp mapper
		assert.Equal(t, "user-123", result.UserID)
		assert.Equal(t, "test@example.com", result.UserEmail)
		assert.Equal(t, "Test User", result.UserName)
		assert.Equal(t, "testuser", result.Username)
		assert.Equal(t, "submitted", result.VoteStatus)
		assert.False(t, result.Abstained)
		assert.Equal(t, "2024-06-01T12:00:00Z", result.LastModifiedTime)
	})

	t.Run("converts ranked choice answers from string to int", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"vote_id":    "vote-123",
			"poll_id":    "poll-456",
			"project_id": "project-sfid",
			"poll_answers": []interface{}{
				map[string]interface{}{
					"question_id": "q1",
					"prompt":      "Choose your favorite",
					"type":        "ranked_choice",
					"ranked_user_choice": []interface{}{
						map[string]interface{}{
							"choice_id":   "c1",
							"choice_text": "Option 1",
							"choice_rank": "1",
						},
						map[string]interface{}{
							"choice_id":   "c2",
							"choice_text": "Option 2",
							"choice_rank": "2",
						},
					},
				},
			},
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)

		require.Len(t, result.PollAnswers, 1)
		require.Len(t, result.PollAnswers[0].RankedUserChoice, 2)
		assert.Equal(t, 1, result.PollAnswers[0].RankedUserChoice[0].ChoiceRank)
		assert.Equal(t, 2, result.PollAnswers[0].RankedUserChoice[1].ChoiceRank)
	})

	t.Run("returns error for invalid choice_rank string", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"vote_id":    "vote-123",
			"poll_id":    "poll-456",
			"project_id": "project-sfid",
			"poll_answers": []interface{}{
				map[string]interface{}{
					"question_id": "q1",
					"prompt":      "Choose your favorite",
					"type":        "ranked_choice",
					"ranked_user_choice": []interface{}{
						map[string]interface{}{
							"choice_id":   "c1",
							"choice_text": "Option 1",
							"choice_rank": "invalid",
						},
					},
				},
			},
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid syntax")
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"vote_id": make(chan int), // Channels can't be marshaled to JSON
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to marshal v1Data to JSON")
	})
}

func TestHandleVoteResponseUpdate(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successfully processes vote response update", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Store parent vote in mappings KV to simulate it already being processed
		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"vote_id":      "vote-123",
			"poll_id":      "poll-456",
			"project_id":   "project-sfid",
			"user_id":      "user-123",
			"username":     "testuser",
			"vote_status":  "submitted",
			"poll_answers": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-123", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
		assert.Equal(t, "vote-123", mockPublisher.publishedVoteResponses[0].UID)
		assert.Equal(t, "testuser", mockPublisher.publishedVoteResponses[0].Username)
	})

	t.Run("handles empty username by publishing vote response without username", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"vote_id":      "vote-123",
			"poll_id":      "poll-456",
			"project_id":   "project-sfid",
			"user_id":      "user-123",
			"vote_status":  "submitted",
			"poll_answers": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-123", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
		assert.Equal(t, "", mockPublisher.publishedVoteResponses[0].Username)
	})

	t.Run("returns false for conversion error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"vote_id": make(chan int), // Invalid for JSON
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-123", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
		assert.Len(t, mockPublisher.publishedVoteResponses, 0)
	})

	t.Run("returns true when parent vote not found in mappings", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Don't store parent vote in mappings - simulate it not being processed yet

		v1Data := map[string]interface{}{
			"vote_id":      "vote-999",
			"poll_id":      "poll-999", // Use different poll_id to avoid test conflicts
			"project_id":   "project-sfid",
			"user_id":      "user-123",
			"vote_status":  "submitted",
			"poll_answers": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-999", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.True(t, shouldRetry) // Retry - parent vote not yet processed
		assert.Len(t, mockPublisher.publishedVoteResponses, 0)
	})

	t.Run("returns false when poll_id is missing", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		v1Data := map[string]interface{}{
			"vote_id": "vote-123",
			// Missing poll_id - can't check for parent vote
			"project_id":   "project-sfid",
			"poll_answers": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-123", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry) // Permanent error, ACK
		assert.Len(t, mockPublisher.publishedVoteResponses, 0)
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()

		// Store parent vote in mappings
		_, err := mappingsKV.Put(ctx, "vote.poll-456", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"vote_id":      "vote-123",
			"poll_id":      "poll-456",
			"project_id":   "project-sfid",
			"poll_answers": []interface{}{},
		}

		mockPublisher := &mockEventPublisher{
			publishVoteResponseErr: errors.New("connection timeout"),
		}
		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		shouldRetry := handleVoteResponseUpdate(ctx, "itx-poll-vote.vote-123", v1Data, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.True(t, shouldRetry) // Transient error, NAK for retry
	})
}

func TestHandleVoteResponseDelete(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successfully processes vote response delete", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		// Store a mapping first
		_, err := mappingsKV.Put(context.Background(), "vote_response.vote-123", []byte("1"))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteResponseDelete(ctx, "vote-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 1)
		assert.Equal(t, "vote-123", mockPublisher.publishedVoteResponses[0].UID)

		// Verify mapping is tombstoned (not hard-deleted)
		entry, err := mappingsKV.Get(context.Background(), "vote_response.vote-123")
		require.NoError(t, err)
		assert.True(t, isTombstonedMapping(entry.Value()))
	})

	t.Run("skips publish when vote response mapping is already tombstoned", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		// Pre-tombstone the mapping to simulate a redelivered delete
		_, err := mappingsKV.Put(context.Background(), "vote_response.vote-123", []byte(tombstoneMarker))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteResponseDelete(ctx, "vote-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResponses, 0) // No duplicate event published
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		mockPublisher := &mockEventPublisher{
			publishVoteResponseErr: errors.New("deadline exceeded"),
		}
		ctx := context.Background()

		logger := slog.Default()
		shouldRetry := handleVoteResponseDelete(ctx, "vote-123", mockPublisher, mappingsKV, logger)

		assert.True(t, shouldRetry) // Transient error, NAK for retry
	})
}
