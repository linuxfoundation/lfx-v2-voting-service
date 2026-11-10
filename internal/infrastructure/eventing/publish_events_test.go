// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	fgaconstants "github.com/linuxfoundation/lfx-v2-fga-sync/pkg/constants"
	indexerTypes "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/types"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
)

// published is one message seen on the embedded server.
type published struct {
	Subject string
	Raw     []byte
}

// indexerMsg decodes an indexer envelope. Data stays raw so tests can check both the
// object payload (create/update) and the bare UID string (delete).
type indexerMsg struct {
	Action         string                       `json:"action"`
	Headers        map[string]string            `json:"headers"`
	Data           json.RawMessage              `json:"data"`
	IndexingConfig *indexerTypes.IndexingConfig `json:"indexing_config"`
}

// accessMsg decodes a generic fga-sync message.
type accessMsg struct {
	ObjectType string `json:"object_type"`
	Operation  string `json:"operation"`
	Data       struct {
		UID              string              `json:"uid"`
		Public           bool                `json:"public"`
		Relations        map[string][]string `json:"relations"`
		References       map[string][]string `json:"references"`
		ExcludeRelations []string            `json:"exclude_relations"`
	} `json:"data"`
}

// barrierSubject carries the sentinel that seen() uses to know the recorder has caught up.
const barrierSubject = "test.recorder.barrier"

// newRecordingPublisher starts an embedded NATS server, records everything published on
// it, and returns a publisher plus a function that returns the messages seen so far.
//
// The returned function is deterministic: it publishes a sentinel from the same connection
// and waits for the recorder to receive it. NATS delivers a connection's messages to a
// subscription in order, so once the sentinel arrives every earlier message has been
// recorded. No sleeping is involved.
func newRecordingPublisher(t *testing.T, opts ...PublisherOption) (*NATSPublisher, *nats.Conn, func() []published) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Port: -1})
	require.NoError(t, err)
	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	var mu sync.Mutex
	var seen []published
	barrier := make(chan struct{}, 1)
	_, err = conn.Subscribe(">", func(m *nats.Msg) {
		if m.Subject == barrierSubject {
			barrier <- struct{}{}
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, published{Subject: m.Subject, Raw: append([]byte(nil), m.Data...)})
	})
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	return NewNATSPublisher(conn, slog.Default(), opts...), conn, func() []published {
		require.NoError(t, conn.Publish(barrierSubject, nil))
		select {
		case <-barrier:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the message recorder to catch up")
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]published(nil), seen...)
	}
}

// bySubject returns the messages published on subject.
func bySubject(msgs []published, subject string) []published {
	var out []published
	for _, m := range msgs {
		if m.Subject == subject {
			out = append(out, m)
		}
	}
	return out
}

func decodeIndexer(t *testing.T, m published) indexerMsg {
	t.Helper()
	var msg indexerMsg
	require.NoError(t, json.Unmarshal(m.Raw, &msg))
	return msg
}

func decodeAccess(t *testing.T, m published) accessMsg {
	t.Helper()
	var msg accessMsg
	require.NoError(t, json.Unmarshal(m.Raw, &msg))
	return msg
}

func testVote() *domain.VoteData {
	return &domain.VoteData{
		VoteUID: "vote-1", Name: "Board Vote", Description: "Annual vote",
		ProjectUID: "proj-1", CommitteeUID: "comm-1",
	}
}

func TestPublishVoteEvent(t *testing.T) {
	for _, action := range []string{"created", "updated"} {
		t.Run(action+" sends the full object to the indexer and syncs access", func(t *testing.T) {
			publisher, _, seen := newRecordingPublisher(t)

			require.NoError(t, publisher.PublishVoteEvent(context.Background(), action, testVote()))

			msgs := seen()
			require.Len(t, msgs, 2)

			idx := bySubject(msgs, IndexVoteSubject)
			require.Len(t, idx, 1)
			env := decodeIndexer(t, idx[0])
			assert.Equal(t, action, env.Action)
			var data domain.VoteData
			require.NoError(t, json.Unmarshal(env.Data, &data))
			assert.Equal(t, "vote-1", data.VoteUID)
			assert.Equal(t, "Board Vote", data.Name)
			require.NotNil(t, env.IndexingConfig)
			assert.Equal(t, "vote-1", env.IndexingConfig.ObjectID)
			require.NotNil(t, env.IndexingConfig.Public)
			assert.False(t, *env.IndexingConfig.Public, "votes are never public")

			acc := bySubject(msgs, fgaconstants.GenericUpdateAccessSubject)
			require.Len(t, acc, 1)
			a := decodeAccess(t, acc[0])
			assert.Equal(t, "vote", a.ObjectType)
			assert.Equal(t, "update_access", a.Operation)
			assert.Equal(t, "vote-1", a.Data.UID)
			assert.False(t, a.Data.Public)
			assert.Equal(t, map[string][]string{"project": {"proj-1"}, "committee": {"comm-1"}}, a.Data.References)
			assert.Equal(t, []string{"participant"}, a.Data.ExcludeRelations,
				"update_access must not delete per-response participant tuples")
		})
	}

	t.Run("only the references that exist are sent", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)
		vote := testVote()
		vote.CommitteeUID = ""

		require.NoError(t, publisher.PublishVoteEvent(context.Background(), "updated", vote))

		acc := bySubject(seen(), fgaconstants.GenericUpdateAccessSubject)
		require.Len(t, acc, 1)
		assert.Equal(t, map[string][]string{"project": {"proj-1"}}, decodeAccess(t, acc[0]).Data.References)
	})

	t.Run("a vote with no project or committee skips the access message", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteEvent(context.Background(), "created", &domain.VoteData{VoteUID: "vote-1"}))

		msgs := seen()
		assert.Len(t, bySubject(msgs, IndexVoteSubject), 1)
		assert.Empty(t, bySubject(msgs, fgaconstants.GenericUpdateAccessSubject))
	})

	t.Run("deleted sends the bare UID and removes all access", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteEvent(context.Background(), "deleted", testVote()))

		msgs := seen()
		require.Len(t, msgs, 2)
		env := decodeIndexer(t, bySubject(msgs, IndexVoteSubject)[0])
		assert.Equal(t, "deleted", env.Action)
		assert.JSONEq(t, `"vote-1"`, string(env.Data))
		require.NotNil(t, env.IndexingConfig)
		assert.Equal(t, "vote:vote-1", env.IndexingConfig.AccessCheckObject)

		del := bySubject(msgs, fgaconstants.GenericDeleteAccessSubject)
		require.Len(t, del, 1)
		a := decodeAccess(t, del[0])
		assert.Equal(t, "vote", a.ObjectType)
		assert.Equal(t, "delete_access", a.Operation)
		assert.Equal(t, "vote-1", a.Data.UID)
		assert.Empty(t, bySubject(msgs, fgaconstants.GenericUpdateAccessSubject))
	})
}

func TestPublishVoteResultEvent(t *testing.T) {
	result := &domain.PollResultData{VoteUID: "vote-1", ProjectUID: "proj-1", CommitteeUID: "comm-1", NumVotesCast: 7}

	t.Run("created sends the snapshot to the indexer only", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResultEvent(context.Background(), "created", result))

		msgs := seen()
		require.Len(t, msgs, 1, "vote_result access is inherited from the vote, so no FGA message is sent")
		assert.Equal(t, IndexVoteResultSubject, msgs[0].Subject)
		env := decodeIndexer(t, msgs[0])
		assert.Equal(t, "created", env.Action)
		var data domain.PollResultData
		require.NoError(t, json.Unmarshal(env.Data, &data))
		assert.Equal(t, 7, data.NumVotesCast)
		require.NotNil(t, env.IndexingConfig)
		assert.Equal(t, "results_viewer", env.IndexingConfig.AccessCheckRelation)
		require.NotNil(t, env.IndexingConfig.Public)
		assert.False(t, *env.IndexingConfig.Public)
	})

	t.Run("deleted sends the bare vote UID", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResultEvent(context.Background(), "deleted", result))

		msgs := seen()
		require.Len(t, msgs, 1)
		env := decodeIndexer(t, msgs[0])
		assert.Equal(t, "deleted", env.Action)
		assert.JSONEq(t, `"vote-1"`, string(env.Data))
	})
}

func TestPublishVoteResponseEvent_IndexerAndAccess(t *testing.T) {
	resp := &domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", ProjectUID: "proj-1", Username: "alice"}

	t.Run("created indexes the response and grants the owner access", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "created", resp))

		msgs := seen()
		idx := bySubject(msgs, IndexVoteResponseSubject)
		require.Len(t, idx, 1)
		env := decodeIndexer(t, idx[0])
		assert.Equal(t, "created", env.Action)
		require.NotNil(t, env.IndexingConfig)
		assert.Equal(t, "vote_response:resp-1", env.IndexingConfig.AccessCheckObject)
		assert.Equal(t, "auditor", env.IndexingConfig.AccessCheckRelation,
			"a response must not be gated on vote#viewer or every participant could read every ballot")

		acc := bySubject(msgs, fgaconstants.GenericUpdateAccessSubject)
		require.Len(t, acc, 1)
		a := decodeAccess(t, acc[0])
		assert.Equal(t, "vote_response", a.ObjectType)
		assert.Equal(t, "resp-1", a.Data.UID)
		assert.Equal(t, map[string][]string{"owner": {"alice"}}, a.Data.Relations)
		assert.Equal(t, map[string][]string{"vote": {"vote-1"}}, a.Data.References)
	})

	t.Run("a response with no username or vote skips the access message", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "updated", &domain.VoteResponseData{UID: "resp-2"}))

		msgs := seen()
		assert.Len(t, bySubject(msgs, IndexVoteResponseSubject), 1)
		assert.Empty(t, bySubject(msgs, fgaconstants.GenericUpdateAccessSubject))
	})

	t.Run("owner relation is omitted when there is no username", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "updated", &domain.VoteResponseData{UID: "resp-3", VoteUID: "vote-1"}))

		acc := bySubject(seen(), fgaconstants.GenericUpdateAccessSubject)
		require.Len(t, acc, 1)
		a := decodeAccess(t, acc[0])
		assert.Empty(t, a.Data.Relations)
		assert.Equal(t, map[string][]string{"vote": {"vote-1"}}, a.Data.References)
	})

	t.Run("deleted sends the bare UID and removes the response's access", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", resp))

		msgs := seen()
		env := decodeIndexer(t, bySubject(msgs, IndexVoteResponseSubject)[0])
		assert.Equal(t, "deleted", env.Action)
		assert.JSONEq(t, `"resp-1"`, string(env.Data))

		del := bySubject(msgs, fgaconstants.GenericDeleteAccessSubject)
		require.Len(t, del, 1)
		assert.Equal(t, "vote_response", decodeAccess(t, del[0]).ObjectType)
		assert.Equal(t, "resp-1", decodeAccess(t, del[0]).Data.UID)
	})
}

func TestPublish_Headers(t *testing.T) {
	t.Run("system events use the service authorization header", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)

		require.NoError(t, publisher.PublishVoteEvent(context.Background(), "updated", testVote()))

		env := decodeIndexer(t, bySubject(seen(), IndexVoteSubject)[0])
		assert.Equal(t, "Bearer voting-service", env.Headers["authorization"])
		assert.NotContains(t, env.Headers, "x-on-behalf-of")
	})

	t.Run("request authorization and principal are forwarded", func(t *testing.T) {
		publisher, _, seen := newRecordingPublisher(t)
		ctx := context.WithValue(context.Background(), constants.AuthorizationContextID, "Bearer user-token")
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "alice")

		require.NoError(t, publisher.PublishVoteEvent(ctx, "updated", testVote()))

		env := decodeIndexer(t, bySubject(seen(), IndexVoteSubject)[0])
		assert.Equal(t, "Bearer user-token", env.Headers["authorization"])
		assert.Equal(t, "alice", env.Headers["x-on-behalf-of"])
	})
}

func TestPublish_ConnectionClosed(t *testing.T) {
	calls := map[string]func(*NATSPublisher) error{
		"PublishVoteEvent": func(p *NATSPublisher) error {
			return p.PublishVoteEvent(context.Background(), "created", testVote())
		},
		"PublishVoteResponseEvent": func(p *NATSPublisher) error {
			return p.PublishVoteResponseEvent(context.Background(), "created", &domain.VoteResponseData{UID: "r", VoteUID: "v", Username: "alice"})
		},
		"PublishVoteResultEvent": func(p *NATSPublisher) error {
			return p.PublishVoteResultEvent(context.Background(), "created", &domain.PollResultData{VoteUID: "v"})
		},
	}
	for name, call := range calls {
		t.Run(name+" returns an internal error", func(t *testing.T) {
			publisher, conn, _ := newRecordingPublisher(t)
			conn.Close()

			err := call(publisher)

			require.Error(t, err)
			assert.Equal(t, domain.ErrorTypeInternal, domain.GetErrorType(err))
		})
	}
}

func TestNATSPublisher_Close(t *testing.T) {
	publisher, conn, _ := newRecordingPublisher(t)

	require.NoError(t, publisher.Close())

	assert.False(t, conn.IsClosed(), "the connection is owned by the event processor, not the publisher")
}
