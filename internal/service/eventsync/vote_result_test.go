// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resultPayload(pollID string) map[string]any {
	return map[string]any{"poll_id": pollID, "project_id": "proj-1", "status": "Closed"}
}

func resultState(h *harness, uid string) domain.MappingState {
	return h.store.State(context.Background(), domain.MappingKindVoteResult, uid)
}

func TestSync_VoteResultUpdate(t *testing.T) {
	key := "itx-poll-results.poll-123"

	// Policy divergence: vote results require the parent vote to be Synced.
	t.Run("retries when the parent vote mapping is missing", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Retry, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	t.Run("policy: retries when the parent vote mapping is tombstoned", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingTombstoned)
		assert.Equal(t, Retry, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	t.Run("publishes a created result when there is no prior mapping, and records it", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "poll-123", h.pub.publishedVoteResults[0].VoteUID)
		assert.Equal(t, "created", h.pub.publishedVoteResultActions[0])
		assert.Equal(t, domain.MappingSynced, resultState(h, "poll-123"))
	})

	t.Run("publishes an updated result when an active mapping exists", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.store.Seed(domain.MappingKindVoteResult, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "updated", h.pub.publishedVoteResultActions[0])
	})

	// Policy divergence: unlike votes and vote responses, a tombstoned own mapping
	// (deleted, then re-sent) is a new create, not an update.
	t.Run("policy: a tombstoned own mapping counts as a create", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.store.Seed(domain.MappingKindVoteResult, "poll-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "created", h.pub.publishedVoteResultActions[0])
		assert.Equal(t, domain.MappingSynced, resultState(h, "poll-123"))
	})

	t.Run("retries a transient publish error and records no mapping", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.pub.publishVoteResultErr = errors.New("connection timeout")
		assert.Equal(t, Retry, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Equal(t, domain.MappingAbsent, resultState(h, "poll-123"))
	})

	t.Run("acks a permanent publish error", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.pub.publishVoteResultErr = errors.New("invalid data format")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Equal(t, domain.MappingAbsent, resultState(h, "poll-123"))
	})

	t.Run("acks without publishing on a permanent conversion error", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: errors.New("unknown project")}))
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	// Policy divergence: vote results (like votes, unlike vote responses) retry a
	// conversion error when it is transient.
	t.Run("policy: a transient conversion error is retried", func(t *testing.T) {
		h := newHarness(t, withIDMapper(&mockIDMapper{projectErr: domain.NewUnavailableError("id mapper unavailable")}))
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		assert.Equal(t, Retry, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	t.Run("acks without publishing when poll_id is missing", func(t *testing.T) {
		h := newHarness(t)
		data := resultPayload("poll-123")
		delete(data, "poll_id")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, data)))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	t.Run("still acks when recording the mapping fails", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(putJSON(t, key, resultPayload("poll-123"))))
		assert.Len(t, h.pub.publishedVoteResults, 1)
	})

	t.Run("publishes only a vote result event", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-123", domain.MappingSynced)
		h.sync(putJSON(t, key, resultPayload("poll-123")))
		assert.Empty(t, h.pub.publishedVotes)
		assert.Empty(t, h.pub.publishedVoteResponses)
	})
}

func TestSync_VoteResultDelete(t *testing.T) {
	key := "itx-poll-results.poll-123"

	t.Run("publishes a delete and tombstones the mapping", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVoteResult, "poll-123", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "poll-123", h.pub.publishedVoteResults[0].VoteUID)
		assert.Equal(t, "deleted", h.pub.publishedVoteResultActions[0])
		assert.Equal(t, domain.MappingTombstoned, resultState(h, "poll-123"))
	})

	t.Run("skips the publish when already tombstoned", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVoteResult, "poll-123", domain.MappingTombstoned)
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Empty(t, h.pub.publishedVoteResults)
	})

	t.Run("retries a transient publish error and does not tombstone", func(t *testing.T) {
		h := newHarness(t)
		h.pub.publishVoteResultErr = errors.New("connection timeout")
		assert.Equal(t, Retry, h.sync(hardDelete(key)))
		assert.Equal(t, domain.MappingAbsent, resultState(h, "poll-123"))
	})

	t.Run("still acks when tombstoning fails", func(t *testing.T) {
		h := newHarness(t)
		h.store.PutErr = errors.New("kv put failed")
		assert.Equal(t, Ack, h.sync(hardDelete(key)))
		assert.Len(t, h.pub.publishedVoteResults, 1)
	})
}
