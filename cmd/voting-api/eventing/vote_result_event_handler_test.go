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
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

		idMapper := idmapper.NewNoOpMapper()
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

		idMapper := idmapper.NewNoOpMapper()
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

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid vote_count")
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

		idMapper := idmapper.NewNoOpMapper()
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

		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()
		logger := slog.Default()

		result, err := convertMapToPollResultData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to marshal v1Data to JSON")
	})
}

func TestHandleVoteResultUpdate(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("retries when parent vote mapping is missing", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Closed",
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.True(t, shouldRetry) // NAK — parent not yet present
		assert.Empty(t, mockPublisher.publishedVoteResults)
	})

	t.Run("retries when parent vote mapping is tombstoned", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-123", []byte(tombstoneMarker))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Closed",
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.True(t, shouldRetry) // tombstoned parent = unavailable
		assert.Empty(t, mockPublisher.publishedVoteResults)
	})

	t.Run("publishes created action when no prior mapping", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-123", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Closed",
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		require.Len(t, mockPublisher.publishedVoteResults, 1)
		assert.Equal(t, "poll-123", mockPublisher.publishedVoteResults[0].VoteUID)
		assert.Equal(t, "created", mockPublisher.publishedVoteResultActions[0])

		// Verify mapping was stored
		entry, err := mappingsKV.Get(ctx, "vote_result.poll-123")
		require.NoError(t, err)
		assert.Equal(t, "1", string(entry.Value()))
	})

	t.Run("publishes updated action when active mapping exists", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-123", []byte("1"))
		require.NoError(t, err)
		_, err = mappingsKV.Put(ctx, "vote_result.poll-123", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Closed",
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		require.Len(t, mockPublisher.publishedVoteResults, 1)
		assert.Equal(t, "updated", mockPublisher.publishedVoteResultActions[0])
	})

	t.Run("treats tombstoned vote_result mapping as created", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-123", []byte("1"))
		require.NoError(t, err)
		// Pre-tombstone the vote_result mapping (simulates re-create after delete)
		_, err = mappingsKV.Put(ctx, "vote_result.poll-123", []byte(tombstoneMarker))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Open",
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		require.Len(t, mockPublisher.publishedVoteResults, 1)
		// Tombstoned prior mapping must be treated as a new create, not an update
		assert.Equal(t, "created", mockPublisher.publishedVoteResultActions[0])
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-123", []byte("1"))
		require.NoError(t, err)

		v1Data := map[string]interface{}{
			"poll_id":    "poll-123",
			"project_id": "proj-1",
			"status":     "Closed",
		}

		mockPublisher := &mockEventPublisher{publishVoteResultErr: errors.New("connection timeout")}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.poll-123", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.True(t, shouldRetry)
	})

	t.Run("returns false and acks for conversion error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		v1Data := map[string]interface{}{
			"poll_id": make(chan int), // invalid
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		ctx := context.Background()
		logger := slog.Default()

		shouldRetry := handleVoteResultUpdate(ctx, "itx-poll-results.bad", v1Data, mockPublisher, idMapper, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Empty(t, mockPublisher.publishedVoteResults)
	})
}

func TestHandleVoteResultDelete(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successfully processes vote result delete", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote_result.poll-123", []byte("1"))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		logger := slog.Default()

		shouldRetry := handleVoteResultDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		require.Len(t, mockPublisher.publishedVoteResults, 1)
		assert.Equal(t, "poll-123", mockPublisher.publishedVoteResults[0].VoteUID)
		assert.Equal(t, "deleted", mockPublisher.publishedVoteResultActions[0])

		// Mapping should be tombstoned
		entry, err := mappingsKV.Get(ctx, "vote_result.poll-123")
		require.NoError(t, err)
		assert.True(t, isTombstonedMapping(entry.Value()))
	})

	t.Run("skips publish when already tombstoned", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote_result.poll-123", []byte(tombstoneMarker))
		require.NoError(t, err)

		mockPublisher := &mockEventPublisher{}
		logger := slog.Default()

		shouldRetry := handleVoteResultDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.False(t, shouldRetry)
		assert.Empty(t, mockPublisher.publishedVoteResults)
	})

	t.Run("returns true for transient publish error", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		mockPublisher := &mockEventPublisher{publishVoteResultErr: errors.New("connection timeout")}
		ctx := context.Background()
		logger := slog.Default()

		shouldRetry := handleVoteResultDelete(ctx, "poll-123", mockPublisher, mappingsKV, logger)

		assert.True(t, shouldRetry)
	})
}

func TestKvHandlerVoteResult(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("routes itx-poll-results PUT to vote result handler", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote.poll-789", []byte("1"))
		require.NoError(t, err)

		entry := &kvEntry{
			key:       "itx-poll-results.poll-789",
			value:     []byte(`{"poll_id":"poll-789","project_id":"proj-1","status":"Closed"}`),
			operation: jetstream.KeyValuePut,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResults, 1)
	})

	t.Run("routes itx-poll-results DELETE to vote result delete handler", func(t *testing.T) {
		mappingsKV, cleanup := setupTestKV(t)
		defer cleanup()

		ctx := context.Background()
		_, err := mappingsKV.Put(ctx, "vote_result.poll-789", []byte("1"))
		require.NoError(t, err)

		entry := &kvEntry{
			key:       "itx-poll-results.poll-789",
			operation: jetstream.KeyValueDelete,
		}

		mockPublisher := &mockEventPublisher{}
		idMapper := idmapper.NewNoOpMapper()
		logger := slog.Default()

		shouldRetry := kvHandler(ctx, entry, mockPublisher, idMapper, mappingsKV, nil, logger)

		assert.False(t, shouldRetry)
		assert.Len(t, mockPublisher.publishedVoteResults, 1)
	})
}
