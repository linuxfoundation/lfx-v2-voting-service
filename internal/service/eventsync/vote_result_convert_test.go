// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIDMapper is a controllable IDMapper for testing error paths.
type mockIDMapper struct {
	projectErr   error
	committeeErr error
}

func (m *mockIDMapper) MapProjectV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, m.projectErr
}

func (m *mockIDMapper) MapProjectV1ToV2(_ context.Context, v1SFID string) (string, error) {
	if m.projectErr != nil {
		return "", m.projectErr
	}
	return v1SFID, nil
}

func (m *mockIDMapper) MapCommitteeV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, m.committeeErr
}

func (m *mockIDMapper) MapCommitteeV1ToV2(_ context.Context, v1SFID string) (string, error) {
	if m.committeeErr != nil {
		return "", m.committeeErr
	}
	return v1SFID, nil
}

func TestConvertMapToPollResultData(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successful conversion with all fields", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":            "poll-123",
			"committee_id":       "committee-sfid",
			"project_id":         "project-sfid",
			"status":             "Closed",
			"num_recipients":     "50",
			"num_votes_cast":     "30",
			"num_abstained":      "5",
			"poll_end_time":      "2024-12-31T23:59:59Z",
			"created_time":       "2024-01-01T00:00:00Z",
			"last_modified_time": "2024-12-31T12:00:00Z",
			"poll_questions_result": []interface{}{
				map[string]interface{}{
					"question_id": "q1",
					"prompt":      "Which do you prefer?",
					"choice_results": []interface{}{
						map[string]interface{}{
							"choice_id":   "c1",
							"choice_text": "Option A",
							"vote_count":  "20",
							"percentage":  float64(66.7),
						},
						map[string]interface{}{
							"choice_id":   "c2",
							"choice_text": "Option B",
							"vote_count":  float64(10),
							"percentage":  float64(33.3),
						},
					},
				},
			},
		}

		idMapper := &mockIDMapper{}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.NotNil(t, result)

		assert.Equal(t, "poll-123", result.VoteUID)
		assert.Equal(t, "poll-123", result.PollID)
		assert.Equal(t, "committee-sfid", result.CommitteeID)
		assert.Equal(t, "committee-sfid", result.CommitteeUID) // NoOp mapper
		assert.Equal(t, "project-sfid", result.ProjectID)
		assert.Equal(t, "project-sfid", result.ProjectUID) // NoOp mapper
		assert.Equal(t, "Closed", result.Status)
		assert.Equal(t, 50, result.NumRecipients)
		assert.Equal(t, 30, result.NumVotesCast)
		assert.Equal(t, 5, result.NumAbstained)
		assert.Equal(t, "2024-12-31T23:59:59Z", result.PollEndTime)
		assert.Equal(t, "2024-01-01T00:00:00Z", result.CreatedTime)
		assert.Equal(t, "2024-12-31T12:00:00Z", result.LastModifiedTime)

		require.Len(t, result.PollQuestionsResult, 1)
		q := result.PollQuestionsResult[0]
		assert.Equal(t, "q1", q.QuestionID)
		assert.Equal(t, "Which do you prefer?", q.Prompt)
		require.Len(t, q.ChoiceResults, 2)
		assert.Equal(t, "c1", q.ChoiceResults[0].ChoiceID)
		assert.Equal(t, 20, q.ChoiceResults[0].VoteCount) // string coercion
		assert.Equal(t, "c2", q.ChoiceResults[1].ChoiceID)
		assert.Equal(t, 10, q.ChoiceResults[1].VoteCount) // float64 coercion
	})

	t.Run("handles minimal fields", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id": "poll-456",
			"status":  "Open",
		}

		idMapper := &mockIDMapper{}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		assert.Equal(t, "poll-456", result.VoteUID)
		assert.Equal(t, 0, result.NumRecipients)
		assert.Equal(t, 0, result.NumVotesCast)
		assert.Empty(t, result.PollQuestionsResult)
	})

	t.Run("returns error for invalid vote_count", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id": "poll-789",
			"status":  "Closed",
			"poll_questions_result": []interface{}{
				map[string]interface{}{
					"question_id": "q1",
					"prompt":      "Pick one",
					"choice_results": []interface{}{
						map[string]interface{}{
							"choice_id":   "c1",
							"choice_text": "Option A",
							"vote_count":  "not-a-number",
							"percentage":  float64(100),
						},
					},
				},
			},
		}

		idMapper := &mockIDMapper{}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "vote_count")
	})

	t.Run("coerces string percentage from Meltano", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id": "poll-999",
			"status":  "Closed",
			"poll_questions_result": []interface{}{
				map[string]interface{}{
					"question_id": "q1",
					"prompt":      "Pick one",
					"choice_results": []interface{}{
						map[string]interface{}{
							"choice_id":   "c1",
							"choice_text": "Option A",
							"vote_count":  float64(20),
							"percentage":  "66.7",
						},
					},
				},
			},
		}

		idMapper := &mockIDMapper{}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err)
		require.Len(t, result.PollQuestionsResult[0].ChoiceResults, 1)
		assert.InDelta(t, 66.7, result.PollQuestionsResult[0].ChoiceResults[0].Percentage, 0.001)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id": make(chan int), // cannot be marshaled
		}

		idMapper := &mockIDMapper{}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to marshal v1Data to JSON")
	})

	t.Run("continues without CommitteeUID when committee mapping fails", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":      "poll-123",
			"committee_id": "committee-sfid",
			"project_id":   "project-sfid",
			"status":       "Closed",
		}

		idMapper := &mockIDMapper{committeeErr: errors.New("committee not found")}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		require.NoError(t, err) // committee failure is non-fatal
		assert.Equal(t, "poll-123", result.VoteUID)
		assert.Equal(t, "project-sfid", result.ProjectUID) // project mapping still works
		assert.Empty(t, result.CommitteeUID)               // omitted on committee error
	})

	t.Run("returns transient error when project mapping is unavailable", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "project-sfid",
			"status":     "Closed",
		}

		idMapper := &mockIDMapper{projectErr: domain.NewUnavailableError("id mapper temporarily overloaded")}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.True(t, isTransientError(err))
	})

	t.Run("returns permanent error when project mapping fails non-transiently", func(t *testing.T) {
		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "project-sfid",
			"status":     "Closed",
		}

		idMapper := &mockIDMapper{projectErr: errors.New("project not found in registry")}
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.False(t, isTransientError(err))
	})
}
