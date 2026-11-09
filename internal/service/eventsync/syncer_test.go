// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// mockEventPublisher is a recording domain.EventPublisher for testing.
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

func (m *mockEventPublisher) PublishVoteEvent(_ context.Context, action string, vote *domain.VoteData) error {
	m.publishedVotes = append(m.publishedVotes, vote)
	m.publishedVoteActions = append(m.publishedVoteActions, action)
	return m.publishVoteErr
}

func (m *mockEventPublisher) PublishVoteResponseEvent(_ context.Context, action string, voteResponse *domain.VoteResponseData) error {
	m.publishedVoteResponses = append(m.publishedVoteResponses, voteResponse)
	m.publishedVoteResponseActions = append(m.publishedVoteResponseActions, action)
	return m.publishVoteResponseErr
}

func (m *mockEventPublisher) PublishVoteResultEvent(_ context.Context, action string, pollResult *domain.PollResultData) error {
	m.publishedVoteResults = append(m.publishedVoteResults, pollResult)
	m.publishedVoteResultActions = append(m.publishedVoteResultActions, action)
	return m.publishVoteResultErr
}

func (m *mockEventPublisher) Close() error { return nil }

// stubPolls is a PollPrivacyReader with a fixed answer.
type stubPolls struct {
	pseudoAnonymous bool
	err             error
	calls           []string
}

func (s *stubPolls) IsPseudoAnonymous(_ context.Context, pollID string) (bool, error) {
	s.calls = append(s.calls, pollID)
	return s.pseudoAnonymous, s.err
}

// inviteCall is one recorded MaybeInvite call.
type inviteCall struct {
	voteResponseUID, email, displayName, pollID string
}

// stubInviter records invite requests.
type stubInviter struct {
	calls []inviteCall
}

func (s *stubInviter) MaybeInvite(_ context.Context, _ *slog.Logger, voteResponseUID, email, displayName, pollID string) {
	s.calls = append(s.calls, inviteCall{voteResponseUID, email, displayName, pollID})
}

// harness wires a Syncer to recording test adapters.
type harness struct {
	pub     *mockEventPublisher
	store   *memoryStore
	polls   *stubPolls
	inviter *stubInviter
	syncer  *Syncer
}

// newHarness builds a Syncer over the in-memory mapping store, a recording publisher,
// the no-op ID mapper, a named (non-pseudo-anonymous) poll reader and a recording inviter.
// opts may override any dependency.
func newHarness(t *testing.T, opts ...func(*Deps)) *harness {
	t.Helper()
	logging.InitStructureLogConfig()

	h := &harness{
		pub:     &mockEventPublisher{},
		store:   newMemoryStore(),
		polls:   &stubPolls{},
		inviter: &stubInviter{},
	}
	deps := Deps{
		Publisher: h.pub,
		IDMapper:  &mockIDMapper{},
		Mappings:  h.store,
		Polls:     h.polls,
		Inviter:   h.inviter,
		Logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(&deps)
	}
	h.syncer = New(deps)
	return h
}

func withIDMapper(m domain.IDMapper) func(*Deps) { return func(d *Deps) { d.IDMapper = m } }
func withNoPolls() func(*Deps)                   { return func(d *Deps) { d.Polls = nil } }
func withNoInviter() func(*Deps)                 { return func(d *Deps) { d.Inviter = nil } }

// sync runs one entry through the Syncer.
func (h *harness) sync(e Entry) Outcome {
	return h.syncer.Sync(context.Background(), e)
}

// putJSON builds a put entry whose value is the JSON encoding of data.
func putJSON(t *testing.T, key string, data map[string]any) Entry {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return Entry{Key: key, Op: OpPut, Value: raw}
}

// hardDelete builds a delete entry; a KV delete/purge marker carries no value.
func hardDelete(key string) Entry {
	return Entry{Key: key, Op: OpDelete}
}

// publishedCount is the total number of events published across all resource kinds.
func (h *harness) publishedCount() int {
	return len(h.pub.publishedVotes) + len(h.pub.publishedVoteResponses) + len(h.pub.publishedVoteResults)
}

func TestSync_Routing(t *testing.T) {
	votePayload := map[string]any{"poll_id": "poll-123", "name": "Test", "project_id": "proj-1", "poll_questions": []any{}}
	responsePayload := map[string]any{"vote_id": "vote-123", "poll_id": "poll-456", "project_id": "proj-1", "poll_answers": []any{}}
	resultPayload := map[string]any{"poll_id": "poll-789", "project_id": "proj-1", "status": "Closed"}

	t.Run("PUT itx-poll routes to the vote handler", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll.poll-123", votePayload)))
		assert.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("PUT itx-poll-vote routes to the vote response handler", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-456", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll-vote.vote-123", responsePayload)))
		assert.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("PUT itx-poll-results routes to the vote result handler", func(t *testing.T) {
		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-789", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(putJSON(t, "itx-poll-results.poll-789", resultPayload)))
		assert.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("DELETE itx-poll routes to the vote delete handler", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(hardDelete("itx-poll.poll-123")))
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteActions[0])
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("DELETE itx-poll-vote routes to the vote response delete handler", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(hardDelete("itx-poll-vote.vote-123")))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteResponseActions[0])
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("DELETE itx-poll-results routes to the vote result delete handler", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(hardDelete("itx-poll-results.poll-789")))
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteResultActions[0])
		assert.Equal(t, 1, h.publishedCount())
	})

	t.Run("decodes a msgpack itx-poll record", func(t *testing.T) {
		encoded, err := msgpack.Marshal(votePayload)
		require.NoError(t, err)
		// The msgpack bytes must not be valid JSON, so the fallback decoder is exercised.
		var jsonCheck map[string]any
		require.Error(t, json.Unmarshal(encoded, &jsonCheck))

		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll.poll-123", Op: OpPut, Value: encoded}))
		assert.Len(t, h.pub.publishedVotes, 1)
	})

	t.Run("decodes a msgpack itx-poll-vote record", func(t *testing.T) {
		encoded, err := msgpack.Marshal(responsePayload)
		require.NoError(t, err)

		h := newHarness(t)
		h.store.Seed(domain.MappingKindVote, "poll-456", domain.MappingSynced)
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll-vote.vote-123", Op: OpPut, Value: encoded}))
		assert.Len(t, h.pub.publishedVoteResponses, 1)
	})

	t.Run("acks without publishing when the payload is neither JSON nor msgpack", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll.poll-123", Op: OpPut, Value: []byte("invalid json")}))
		assert.Zero(t, h.publishedCount())
	})

	t.Run("acks an unsupported key prefix on PUT", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(putJSON(t, "unsupported-prefix.some-id", map[string]any{})))
		assert.Zero(t, h.publishedCount())
	})

	t.Run("acks an unsupported key prefix on DELETE", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(hardDelete("unsupported.id-123")))
		assert.Zero(t, h.publishedCount())
	})

	t.Run("acks a delete whose key has no UID", func(t *testing.T) {
		for _, key := range []string{"no-period-key", "itx-poll.", ""} {
			h := newHarness(t)
			assert.Equal(t, Ack, h.sync(hardDelete(key)), "key %q", key)
			assert.Zero(t, h.publishedCount(), "key %q", key)
		}
	})

	t.Run("acks an unknown operation", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll.poll-123", Op: OpUnknown, Value: []byte(`{}`)}))
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll.poll-123", Op: Operation(99), Value: []byte(`{}`)}))
		assert.Zero(t, h.publishedCount())
	})
}

func TestSync_SoftDelete(t *testing.T) {
	t.Run("a non-empty _sdc_deleted_at on itx-poll publishes a vote delete, not an upsert", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll.poll-123", map[string]any{
			"poll_id": "poll-123", "name": "Test", "project_id": "proj-1", "_sdc_deleted_at": "2024-01-01T00:00:00Z",
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteActions[0])
		assert.Empty(t, h.pub.publishedVoteResponses)
		assert.Equal(t, domain.MappingTombstoned, h.store.State(context.Background(), domain.MappingKindVote, "poll-123"))
	})

	t.Run("a non-empty _sdc_deleted_at on itx-poll-vote publishes a vote response delete", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll-vote.vote-123", map[string]any{
			"vote_id": "vote-123", "poll_id": "poll-456", "project_id": "proj-1", "_sdc_deleted_at": "2024-01-01T00:00:00Z",
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVoteResponses, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteResponseActions[0])
		assert.Empty(t, h.pub.publishedVotes)
	})

	t.Run("a non-empty _sdc_deleted_at on itx-poll-results publishes a vote result delete", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll-results.poll-789", map[string]any{
			"poll_id": "poll-789", "_sdc_deleted_at": "2024-01-01T00:00:00Z",
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVoteResults, 1)
		assert.Equal(t, "deleted", h.pub.publishedVoteResultActions[0])
	})

	t.Run("a soft delete of a vote response carries the voter so the participant can be removed", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll-vote.vote-123", map[string]any{
			"vote_id": "vote-123", "poll_id": "poll-456", "username": "nickcoai", "_sdc_deleted_at": "2024-01-01T00:00:00Z",
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVoteResponses, 1)
		got := h.pub.publishedVoteResponses[0]
		assert.Equal(t, "deleted", h.pub.publishedVoteResponseActions[0])
		assert.Equal(t, "vote-123", got.UID)
		assert.Equal(t, "poll-456", got.VoteUID)
		assert.Equal(t, "poll-456", got.PollID)
		assert.Equal(t, "nickcoai", got.Username)
	})

	t.Run("a hard delete of a vote response has no record, so no voter is known", func(t *testing.T) {
		h := newHarness(t)
		assert.Equal(t, Ack, h.sync(Entry{Key: "itx-poll-vote.vote-123", Op: OpDelete, Value: nil}))
		require.Len(t, h.pub.publishedVoteResponses, 1)
		got := h.pub.publishedVoteResponses[0]
		assert.Equal(t, "vote-123", got.UID)
		assert.Empty(t, got.Username)
		assert.Empty(t, got.VoteUID)
		assert.Empty(t, got.PollID)
	})

	t.Run("a null _sdc_deleted_at is a normal upsert", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll.poll-123", map[string]any{
			"poll_id": "poll-123", "name": "Test", "project_id": "proj-1", "poll_questions": []any{}, "_sdc_deleted_at": nil,
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "created", h.pub.publishedVoteActions[0])
	})

	t.Run("an empty-string _sdc_deleted_at is a normal upsert", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll.poll-123", map[string]any{
			"poll_id": "poll-123", "name": "Test", "project_id": "proj-1", "poll_questions": []any{}, "_sdc_deleted_at": "",
		}))
		assert.Equal(t, Ack, out)
		require.Len(t, h.pub.publishedVotes, 1)
		assert.Equal(t, "created", h.pub.publishedVoteActions[0])
	})

	t.Run("a soft delete on an unsupported prefix is acked without publishing", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "other.id-1", map[string]any{"_sdc_deleted_at": "2024-01-01T00:00:00Z"}))
		assert.Equal(t, Ack, out)
		assert.Zero(t, h.publishedCount())
	})

	t.Run("a soft delete whose key has no UID is acked without publishing", func(t *testing.T) {
		h := newHarness(t)
		out := h.sync(putJSON(t, "itx-poll.", map[string]any{"_sdc_deleted_at": "2024-01-01T00:00:00Z"}))
		assert.Equal(t, Ack, out)
		assert.Zero(t, h.publishedCount())
	})
}
