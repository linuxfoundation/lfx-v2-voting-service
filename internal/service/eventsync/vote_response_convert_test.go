// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
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
			"comment_responses": []interface{}{
				map[string]interface{}{
					"prompt_id":    "prompt-1",
					"comment_text": "Because the incumbent earned it.",
				},
			},
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

		require.Len(t, result.CommentResponses, 1)
		assert.Equal(t, "prompt-1", result.CommentResponses[0].PromptID)
		assert.Equal(t, "Because the incumbent earned it.", result.CommentResponses[0].CommentText)
	})

	t.Run("lowercases mixed-case user_email for indexing (GH #3063)", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"vote_id":    "vote-123",
			"poll_id":    "poll-456",
			"project_id": "project-sfid",
			"user_email": "Jane.Doe@Example.org",
		}

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "jane.doe@example.org", result.UserEmail)
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
