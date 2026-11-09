// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// marshalVoteData marshals a published VoteData back to its wire map so tests can
// assert on the exact JSON keys downstream indexer consumers see. Struct-field
// assertions alone cannot guard the indexer contract: a tag rename or a dropped
// `,omitempty` would silently change the wire format while field checks still pass.
func marshalVoteData(t *testing.T, vote *domain.VoteData) map[string]interface{} {
	t.Helper()
	payload, err := json.Marshal(vote)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &wire))
	return wire
}

func votePayload(pollID string) map[string]any {
	return map[string]any{
		"poll_id":        pollID,
		"name":           "Test Vote",
		"status":         "active",
		"project_id":     "project-sfid",
		"poll_questions": []any{},
	}
}

func voteState(h *harness, uid string) domain.MappingState {
	return h.store.State(context.Background(), domain.MappingKindVote, uid)
}

func TestSync_VoteUpdate(t *testing.T) {
	key := "itx-poll.poll-123"

	t.Run("publishes a created vote and records the mapping", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "poll-123", h.pub.publishedVotes[0].VoteUID)
		assert.Equal(t, "created", h.pub.publishedVoteActions[0])
		assert.Equal(t, domain.MappingSynced, voteState(h, "poll-123"))
	})

	t.Run("forwards end_time_timezone from the raw record to the published VoteData", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("poll-tz")
		data["end_time"] = "2026-02-15T23:59:59Z"
		data["end_time_timezone"] = "America/New_York"
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll.poll-tz", data)))
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "America/New_York", h.pub.publishedVotes[0].EndTimeTimezone)
	})

	t.Run("wire format: end_time_timezone key present with value when set", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("poll-tz-wire")
		data["end_time"] = "2026-02-15T23:59:59Z"
		data["end_time_timezone"] = "America/New_York"
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll.poll-tz-wire", data)))
		require.Len(t, h.pub.publishedVotes, 1)
		wire := marshalVoteData(t, h.pub.publishedVotes[0])
		assert.Equal(t, "America/New_York", wire["end_time_timezone"],
			"indexer contract: end_time_timezone must marshal under exactly this key")
	})

	t.Run("wire format: end_time_timezone key absent when empty", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("poll-no-tz-wire")
		data["end_time"] = "2026-02-15T23:59:59Z"
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll.poll-no-tz-wire", data)))
		require.Len(t, h.pub.publishedVotes, 1)
		wire := marshalVoteData(t, h.pub.publishedVotes[0])
		_, ok := wire["end_time_timezone"]
		assert.False(t, ok,
			"indexer contract: empty end_time_timezone must be omitted from the wire (`json:\",omitempty\"`)")
	})

	t.Run("a vote with no prior mapping is a create", func(t *testing.T) {
		h := newHarness(t)
		h.sync(putJSON(t, key, votePayload("poll-123")))
		assert.Equal(t, "created", h.pub.publishedVoteActions[0])
	})

	t.Run("a vote with a synced mapping is an update", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Equal(t, "updated", h.pub.publishedVoteActions[0])
	})

	// Policy divergence: unlike vote results, a vote whose own mapping is tombstoned
	// (deleted, then re-sent) is still reported as an update, not a create.
	t.Run("policy: a tombstoned own mapping counts as an update", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "updated", h.pub.publishedVoteActions[0])
		assert.Equal(t, domain.MappingSynced, voteState(h, "poll-123"))
	})

	t.Run("acks without publishing when the record cannot be converted", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("poll-789")
		data["total_voting_request_invitations"] = "invalid"
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll.poll-789", data)))
		assert.Empty(t, h.pub.publishedVotes)
	})

	t.Run("acks without publishing on a permanent ID mapping error", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: errors.New("unknown project")}))
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVotes)
	})

	// Policy divergence: votes (like vote results, unlike vote responses) retry a
	// conversion error when it is transient.
	t.Run("policy: a transient conversion error is retried", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: domain.NewUnavailableError("id mapper unavailable")}))
		assert.Equal(t, Retry, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVotes)
	})

	t.Run("acks without publishing when the vote UID is missing", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("")
		delete(data, "poll_id")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.pub.publishedVotes)
	})

	t.Run("acks without publishing when the project has no v2 mapping", func(t *testing.T) {
		h := newHarness(t)
		data := votePayload("poll-123")
		delete(data, "project_id")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.pub.publishedVotes)
		assert.Equal(t, domain.MappingAbsent, voteState(h, "poll-123"))
	})

	t.Run("retries a transient publish error and records no mapping", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteErr = errors.New("timeout error")
		assert.Equal(t, Retry, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Equal(t, domain.MappingAbsent, voteState(h, "poll-123"))
	})

	t.Run("acks a permanent publish error and records no mapping", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteErr = errors.New("invalid data format")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Equal(t, domain.MappingAbsent, voteState(h, "poll-123"))
	})

	t.Run("still acks when recording the mapping fails", func(t *testing.T) {
		h := newHarness(t)
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, votePayload("poll-123"))))
		assert.Len(t, h.pub.publishedVotes, 1)
	})
}

func TestSync_VoteDelete(t *testing.T) {
	key := "itx-poll.poll-123"

	t.Run("publishes a delete and tombstones the mapping", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "poll-123", h.pub.publishedVotes[0].VoteUID)
		assert.Equal(t, "deleted", h.pub.publishedVoteActions[0])
		assert.Equal(t, domain.MappingTombstoned, voteState(h, "poll-123"))
	})

	t.Run("skips the publish when the mapping is already tombstoned", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Empty(t, h.pub.publishedVotes) // no duplicate event
	})

	t.Run("retries a transient publish error and does not tombstone", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteErr = errors.New("connection timeout")
		assert.Equal(t, Retry, h.sync(hardDelete(key)))
		assert.Equal(t, domain.MappingAbsent, voteState(h, "poll-123"))
	})

	t.Run("acks a permanent publish error and does not tombstone", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteErr = errors.New("invalid data format")
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Equal(t, domain.MappingAbsent, voteState(h, "poll-123"))
	})

	t.Run("still acks when tombstoning fails", func(t *testing.T) {
		h := newHarness(t)
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Len(t, h.pub.publishedVotes, 1)
	})
}
