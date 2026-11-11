// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"errors"
	"testing"

	"bytes"
	"encoding/json"
	"log/slog"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func responsePayload(voteID, pollID string) map[string]any {
	return map[string]any{
		"vote_id":      voteID,
		"poll_id":      pollID,
		"project_id":   "project-sfid",
		"user_id":      "user-123",
		"username":     "testuser",
		"vote_status":  "submitted",
		"poll_answers": []any{},
	}
}

func responseState(h *harness, uid string) domain.MappingState {
	return h.store.State(context.Background(), domain.MappingKindVoteResponse, uid)
}

// withParentVote seeds the parent vote as synced.
func withParentVote(h *harness, pollID string) {
	h.store.Seed(domain.MappingKindVote, pollID, domain.MappingSynced)
}

func TestSync_VoteResponseUpdate(t *testing.T) {
	key := "itx-poll-vote.vote-123"

	t.Run("publishes a created vote response and records the mapping", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, "vote-123", h.pub.publishedVoteResponses[0].UID)
		assert.Equal(t, "testuser", h.pub.publishedVoteResponses[0].Username)
		assert.Equal(t, "created", h.pub.publishedVoteResponseActions[0])
		assert.Equal(t, domain.MappingSynced, responseState(h, "vote-123"))
	})

	t.Run("publishes a vote response without a username", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		data := responsePayload("vote-123", "poll-456")
		delete(data, "username")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Empty(t, h.pub.publishedVoteResponses[0].Username)
	})

	t.Run("a vote response with a synced mapping is an update", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Equal(t, "updated", h.pub.publishedVoteResponseActions[0])
	})

	// Policy divergence: unlike vote results, a vote response whose own mapping is
	// tombstoned (deleted, then re-sent) is still reported as an update.
	t.Run("policy: a tombstoned own mapping counts as an update", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, "updated", h.pub.publishedVoteResponseActions[0])
		assert.Equal(t, domain.MappingSynced, responseState(h, "vote-123"))
	})

	t.Run("retries when the parent vote is not in the mappings yet", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Retry, h.sync(putJSON(t, "itx-poll-vote.vote-999", responsePayload("vote-999", "poll-999"))))
		assert.Empty(t, h.pub.publishedVoteResponses)
		assert.Equal(t, domain.MappingAbsent, responseState(h, "vote-999"))
	})

	// Policy divergence: unlike vote results, a vote response only needs its parent
	// vote to have a mapping; a tombstoned (deleted) parent is accepted.
	t.Run("policy: a tombstoned parent vote is accepted", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-456", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		require.Len(t, h.pub.publishedVoteResponses, 1)
	})

	t.Run("acks without publishing when poll_id is missing", func(t *testing.T) {
		h := newHarness(t)
		data := responsePayload("vote-123", "")
		delete(data, "poll_id")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.pub.publishedVoteResponses)
	})

	t.Run("acks without publishing when vote_id is missing", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		data := responsePayload("", "poll-456")
		delete(data, "vote_id")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.pub.publishedVoteResponses)
	})

	t.Run("acks without publishing on a permanent conversion error", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: errors.New("unknown project")}))
		withParentVote(h, "poll-456")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Empty(t, h.pub.publishedVoteResponses)
	})

	// Policy divergence: unlike votes and vote results, every conversion error is
	// permanent for a vote response, even one that would be transient elsewhere.
	t.Run("policy: a transient conversion error is still acked, never retried", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: domain.NewUnavailableError("id mapper unavailable")}))
		withParentVote(h, "poll-456")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Empty(t, h.pub.publishedVoteResponses)
	})

	t.Run("retries a transient publish error and records no mapping", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.pub.publishVoteResponseErr = errors.New("connection timeout")
		assert.Equal(t, Retry, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Equal(t, domain.MappingAbsent, responseState(h, "vote-123"))
		assert.Empty(t, h.inviter.calls, "no invite when the publish failed")
	})

	t.Run("acks a permanent publish error", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.pub.publishVoteResponseErr = errors.New("invalid data format")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Equal(t, domain.MappingAbsent, responseState(h, "vote-123"))
	})

	t.Run("still acks when recording the mapping fails", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, responsePayload("vote-123", "poll-456"))))
		assert.Len(t, h.pub.publishedVoteResponses, 1)
	})
}

func TestSync_VoteResponseUpdate_PseudoAnonymous(t *testing.T) {
	ballot := func() map[string]any {
		return map[string]any{
			"vote_id":            "vote-123",
			"poll_id":            "poll-456",
			"project_id":         "project-sfid",
			"user_id":            "user-123",
			"user_email":         "voter@example.com",
			"username":           "testuser",
			"vote_status":        "responded",
			"vote_creation_time": "2024-01-01T00:00:00Z",
			"abstained":          true,
			"poll_answers": []any{
				map[string]any{
					"question_id": "q1",
					"prompt":      "Pick one",
					"type":        "single_choice",
					"user_choice": []any{map[string]any{"choice_id": "c1", "choice_text": "Alice"}},
				},
			},
			"comment_responses": []any{
				map[string]any{"prompt_id": "prompt-1", "comment_text": "secret"},
			},
		}
	}

	tests := []struct {
		name            string
		polls           *stubPolls // nil means no PollPrivacyReader is wired
		existingMapping bool
		wantRetry       bool
		wantPublished   bool
		wantRedacted    bool
	}{
		{name: "pseudo-anonymous poll redacts the ballot", polls: &stubPolls{pseudoAnonymous: true}, wantPublished: true, wantRedacted: true},
		{name: "named poll keeps the ballot", polls: &stubPolls{}, wantPublished: true},
		{
			name:          "transient poll read error retries without publishing",
			polls:         &stubPolls{err: errors.New("nats timeout")},
			wantRetry:     true,
			wantPublished: false,
		},
		{
			name:            "update of an existing response on a pseudo-anonymous poll stays redacted",
			polls:           &stubPolls{pseudoAnonymous: true},
			existingMapping: true,
			wantPublished:   true,
			wantRedacted:    true,
		},
		// Fails closed: without a reader every poll is treated as pseudo-anonymous.
		{name: "no poll reader wired redacts the ballot", polls: nil, wantPublished: true, wantRedacted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []func(*Deps)
			if tt.polls == nil {
				opts = append(opts, withNoPolls())
			}
			h := newHarness(t, opts...)
			if tt.polls != nil {
				h.polls.pseudoAnonymous = tt.polls.pseudoAnonymous
				h.polls.err = tt.polls.err
			}
			withParentVote(h, "poll-456")
			if tt.existingMapping {
				h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingSynced)
			}

			out := h.sync(putJSON(t, "itx-poll-vote.vote-123", ballot()))

			if tt.wantRetry {
				assert.Equal(t, Retry, out)
			} else {
				assert.Equal(t, Ack, out)
			}
			if tt.polls != nil {
				assert.Equal(t, []string{"poll-456"}, h.polls.calls, "the parent poll is looked up by its poll_id")
			}
			if !tt.wantPublished {
				assert.Empty(t, h.pub.publishedVoteResponses)
				assert.Equal(t, domain.MappingAbsent, responseState(h, "vote-123"))
				return
			}
			require.Len(t, h.pub.publishedVoteResponses, 1)
			published := h.pub.publishedVoteResponses[0]

			// Participation data is always kept.
			assert.Equal(t, "testuser", published.Username)
			assert.Equal(t, "voter@example.com", published.UserEmail)
			assert.Equal(t, "responded", published.VoteStatus)
			assert.Equal(t, "2024-01-01T00:00:00Z", published.VoteCreationTime)

			assert.Equal(t, tt.wantRedacted, published.BallotRedacted)
			if tt.wantRedacted {
				assert.Nil(t, published.PollAnswers)
				assert.Nil(t, published.CommentResponses)
				assert.False(t, published.Abstained)
				return
			}
			assert.Len(t, published.PollAnswers, 1)
			assert.Len(t, published.CommentResponses, 1)
			assert.True(t, published.Abstained)
		})
	}
}

func TestSync_VoteResponseUpdate_Invite(t *testing.T) {
	key := "itx-poll-vote.vote-123"
	guest := func() map[string]any {
		data := responsePayload("vote-123", "poll-456")
		delete(data, "username")
		data["user_email"] = "Guest@Example.com"
		data["user_name"] = "Guest User"
		return data
	}

	t.Run("invites a newly created response with no username and an email", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, guest())))
		require.Len(t, h.inviter.calls, 1)
		// The email is normalised to lower case before it reaches the invite hook.
		assert.Equal(t, inviteCall{
			voteResponseUID: "vote-123",
			email:           "guest@example.com",
			displayName:     "Guest User",
			pollID:          "poll-456",
		}, h.inviter.calls[0])
	})

	t.Run("does not invite an update", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, guest())))
		assert.Empty(t, h.inviter.calls)
	})

	t.Run("does not invite a response whose tombstoned mapping counts as an update", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, guest())))
		assert.Empty(t, h.inviter.calls)
	})

	t.Run("does not invite a voter who already has a username", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		data := guest()
		data["username"] = "existing"
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.inviter.calls)
	})

	t.Run("does not invite a voter with no email", func(t *testing.T) {
		h := newHarness(t)
		withParentVote(h, "poll-456")
		data := guest()
		delete(data, "user_email")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.inviter.calls)
	})

	t.Run("works without an inviter wired", func(t *testing.T) {
		h := newHarness(t, withNoInviter())
		withParentVote(h, "poll-456")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, guest())))
		assert.Len(t, h.pub.publishedVoteResponses, 1)
	})
}

func TestSync_VoteResponseDelete(t *testing.T) {
	key := "itx-poll-vote.vote-123"

	t.Run("publishes a delete and tombstones the mapping", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, "vote-123", h.pub.publishedVoteResponses[0].UID)
		assert.Equal(t, "deleted", h.pub.publishedVoteResponseActions[0])
		assert.Equal(t, domain.MappingTombstoned, responseState(h, "vote-123"))
	})

	t.Run("skips the publish when the mapping is already tombstoned", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVoteResponse, "vote-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Empty(t, h.pub.publishedVoteResponses) // no duplicate event
	})

	t.Run("retries a transient publish error and does not tombstone", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteResponseErr = errors.New("deadline exceeded")
		assert.Equal(t, Retry, h.sync(hardDelete(key)))
		assert.Equal(t, domain.MappingAbsent, responseState(h, "vote-123"))
	})

	t.Run("an undecodable delete record still publishes a delete without a voter", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.syncer.Sync(context.Background(), Entry{Key: key, Op: OpDelete, Value: []byte("not-json-or-msgpack")}))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Empty(t, h.pub.publishedVoteResponses[0].Username)
	})

	t.Run("still acks when tombstoning fails", func(t *testing.T) {
		h := newHarness(t)
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Len(t, h.pub.publishedVoteResponses, 1)
	})
}

func TestShouldSendVoteResponseInvite(t *testing.T) {
	assert.True(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionUpdated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "existing", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", ""))
}

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

		idMapper := &mockIDMapper{}
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

		idMapper := &mockIDMapper{}
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

		idMapper := &mockIDMapper{}
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

		idMapper := &mockIDMapper{}
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

		idMapper := &mockIDMapper{}
		ctx := context.Background()

		logger := slog.Default()
		result, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, logger)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to marshal v1Data to JSON")
	})
}

// logRecords decodes the JSON log lines written to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal(line, &rec))
		records = append(records, rec)
	}
	return records
}

// TestSync_VoteResponseUpdate_LogsCarryParentVote pins that every log line of a vote
// response update after the UID check carries the parent vote's poll_id, so operators
// can search logs by parent vote.
func TestSync_VoteResponseUpdate_LogsCarryParentVote(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := newHarness(t, func(d *Deps) { d.Logger = logger })
	withParentVote(h, "poll-456")
	h.pub.publishVoteResponseErr = errors.New("indexer rejected the message")

	assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll-vote.vote-123", responsePayload("vote-123", "poll-456"))))

	var sawPublishFailure bool
	for _, rec := range logRecords(t, &buf) {
		if rec["vote_response_id"] == nil {
			continue // logged before the UID was known
		}
		assert.Equal(t, "poll-456", rec["poll_id"], "log line %q lost poll_id", rec["msg"])
		if rec["msg"] == "failed to publish vote response event" {
			sawPublishFailure = true
		}
	}
	assert.True(t, sawPublishFailure, "expected the publish failure to be logged")
}
