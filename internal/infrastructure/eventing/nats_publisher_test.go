// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	fgaconstants "github.com/linuxfoundation/lfx-v2-fga-sync/pkg/constants"
	indexerTypes "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/types"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVoteIndexingConfig_Contract(t *testing.T) {
	voteUID := "vote-abc-123"
	data := &domain.VoteData{
		VoteUID:      voteUID,
		Name:         "Test Vote",
		Description:  "A description",
		ProjectUID:   "proj-111",
		CommitteeUID: "comm-222",
	}

	cfg := buildVoteIndexingConfig(data)

	// docs/indexer-contract.md § Vote (Poll) — Access Control
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "viewer")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, voteUID)

	// Search behaviour
	assertField(t, "SortName", cfg.SortName, data.Name)
	assertField(t, "Fulltext", cfg.Fulltext, fmt.Sprintf("%s %s", data.Name, data.Description))

	// Tags — exactly these two entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("committee_uid:%s", data.CommitteeUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these two entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("committee:%s", data.CommitteeUID),
	})
}

func TestBuildVoteIndexingConfig_EmptyOptionalFields(t *testing.T) {
	data := &domain.VoteData{VoteUID: "v1"}

	cfg := buildVoteIndexingConfig(data)

	if len(cfg.Tags) != 0 {
		t.Errorf("expected no tags when ProjectUID and CommitteeUID are empty, got %v", cfg.Tags)
	}
	if len(cfg.ParentRefs) != 0 {
		t.Errorf("expected no parent refs when UIDs are empty, got %v", cfg.ParentRefs)
	}
	if len(cfg.NameAndAliases) != 0 {
		t.Errorf("expected no name aliases when Name is empty, got %v", cfg.NameAndAliases)
	}
}

func TestBuildVoteResponseIndexingConfig_Contract(t *testing.T) {
	uid := "vr-abc-123"
	voteUID := "vote-parent-456"
	data := &domain.VoteResponseData{
		UID:        uid,
		VoteUID:    voteUID,
		Username:   "alice",
		ProjectUID: "proj-111",
	}

	cfg := buildVoteResponseIndexingConfig(data)

	// docs/indexer-contract.md § Vote Response — Access Control
	// Note: access is scoped to the response itself (owner or vote auditor), not vote#viewer,
	// so vote participants cannot read each other's ballots.
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote_response:%s", uid))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "auditor")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote_response:%s", uid))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, uid)

	// Search behaviour
	assertField(t, "SortName", cfg.SortName, data.Username)
	assertField(t, "Fulltext", cfg.Fulltext, data.Username)

	// Tags — exactly these two entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("vote_uid:%s", voteUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these two entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("vote:%s", voteUID),
	})
}

func TestBuildVoteResultIndexingConfig_Contract(t *testing.T) {
	voteUID := "vote-result-123"
	data := &domain.PollResultData{
		VoteUID:      voteUID,
		ProjectUID:   "proj-111",
		CommitteeUID: "comm-222",
	}

	cfg := buildVoteResultIndexingConfig(data)

	// docs/indexer-contract.md § Vote Result — Access Control
	assertField(t, "AccessCheckObject", cfg.AccessCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "AccessCheckRelation", cfg.AccessCheckRelation, "results_viewer")
	assertField(t, "HistoryCheckObject", cfg.HistoryCheckObject, fmt.Sprintf("vote:%s", voteUID))
	assertField(t, "HistoryCheckRelation", cfg.HistoryCheckRelation, "auditor")
	assertField(t, "ObjectID", cfg.ObjectID, voteUID)

	// Tags — exactly these three entries, no more
	assertExactSlice(t, "Tags", cfg.Tags, []string{
		fmt.Sprintf("vote_uid:%s", voteUID),
		fmt.Sprintf("committee_uid:%s", data.CommitteeUID),
		fmt.Sprintf("project_uid:%s", data.ProjectUID),
	})

	// Parent refs — exactly these three entries, no more
	assertExactSlice(t, "ParentRefs", cfg.ParentRefs, []string{
		fmt.Sprintf("vote:%s", voteUID),
		fmt.Sprintf("project:%s", data.ProjectUID),
		fmt.Sprintf("committee:%s", data.CommitteeUID),
	})
}

func TestBuildVoteResultIndexingConfig_AlwaysEmitsVoteUID(t *testing.T) {
	voteUID := "v1"
	data := &domain.PollResultData{VoteUID: voteUID}

	cfg := buildVoteResultIndexingConfig(data)

	// vote_uid tag and vote parent ref must always be emitted regardless of
	// other optional fields — the indexer requires them to relate results back
	// to the parent vote.
	assertContains(t, "Tags", cfg.Tags, fmt.Sprintf("vote_uid:%s", voteUID))
	assertContains(t, "ParentRefs", cfg.ParentRefs, fmt.Sprintf("vote:%s", voteUID))
}

func assertField(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("IndexingConfig.%s: got %q, want %q (check docs/indexer-contract.md)", field, got, want)
	}
}

func assertContains(t *testing.T, field string, slice []string, want string) {
	t.Helper()
	for _, v := range slice {
		if v == want {
			return
		}
	}
	t.Errorf("IndexingConfig.%s does not contain %q (got %v); check docs/indexer-contract.md", field, want, slice)
}

// assertExactSlice verifies that got and want contain exactly the same elements
// (order-independent). An extra or missing element in got fails the test, which
// is stronger than assertContains (subset check).
func assertExactSlice(t *testing.T, field string, got, want []string) {
	t.Helper()
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if !reflect.DeepEqual(gotCopy, wantCopy) {
		t.Errorf("IndexingConfig.%s mismatch (check docs/indexer-contract.md):\n  got:  %v\n  want: %v", field, got, want)
	}
}

// fgaMessage is the subset of an fga-sync message these tests assert on.
type fgaMessage struct {
	Subject    string
	ObjectType string                 `json:"object_type"`
	Operation  string                 `json:"operation"`
	Data       map[string]interface{} `json:"data"`
}

// newCapturingPublisher starts an embedded NATS server and returns a publisher plus a
// function that drains the fga-sync messages published so far.
func newCapturingPublisher(t *testing.T, opts ...PublisherOption) (*NATSPublisher, func() []fgaMessage) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Port: -1})
	require.NoError(t, err)
	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	// One wildcard subscription so everything is handled by a single callback, in publish
	// order. drain publishes a sentinel and waits for it, which guarantees every message
	// published before it has already been recorded (no sleeping).
	fgaSubjects := map[string]bool{
		fgaconstants.GenericUpdateAccessSubject: true,
		fgaconstants.GenericDeleteAccessSubject: true,
		fgaconstants.GenericMemberPutSubject:    true,
		fgaconstants.GenericMemberRemoveSubject: true,
	}
	const barrierSubject = "test.fga.barrier"
	var mu sync.Mutex
	var seen []fgaMessage
	barrier := make(chan struct{}, 1)
	_, err = conn.Subscribe(">", func(m *nats.Msg) {
		if m.Subject == barrierSubject {
			barrier <- struct{}{}
			return
		}
		if !fgaSubjects[m.Subject] {
			return
		}
		var msg fgaMessage
		require.NoError(t, json.Unmarshal(m.Data, &msg))
		msg.Subject = m.Subject
		mu.Lock()
		seen = append(seen, msg)
		mu.Unlock()
	})
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	drain := func() []fgaMessage {
		require.NoError(t, conn.Publish(barrierSubject, nil))
		select {
		case <-barrier:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the fga message recorder to catch up")
		}
		mu.Lock()
		defer mu.Unlock()
		out := seen
		seen = nil
		return out
	}
	return NewNATSPublisher(conn, slog.Default(), opts...), drain
}

func TestSendVoteParticipantMessage(t *testing.T) {
	tests := []struct {
		name          string
		data          domain.VoteResponseData
		wantOperation string
		wantSubject   string
		wantNone      bool
	}{
		{
			name:          "invitee with username becomes participant of the vote",
			data:          domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai"},
			wantOperation: "member_put",
			wantSubject:   fgaconstants.GenericMemberPutSubject,
		},
		{
			name:          "removed voter loses participant",
			data:          domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai", VoterRemoved: true},
			wantOperation: "member_remove",
			wantSubject:   fgaconstants.GenericMemberRemoveSubject,
		},
		{
			name:     "no username is skipped (email-only invitee, same as the owner tuple)",
			data:     domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1"},
			wantNone: true,
		},
		{
			name:     "no vote uid is skipped",
			data:     domain.VoteResponseData{UID: "resp-1", Username: "nickcoai"},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher, drain := newCapturingPublisher(t)

			require.NoError(t, publisher.sendVoteParticipantMessage(context.Background(), &tt.data, tt.data.VoterRemoved))

			got := drain()
			if tt.wantNone {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantSubject, got[0].Subject)
			assert.Equal(t, "vote", got[0].ObjectType)
			assert.Equal(t, tt.wantOperation, got[0].Operation)
			assert.Equal(t, "vote-1", got[0].Data["uid"])
			assert.Equal(t, "nickcoai", got[0].Data["username"])
			assert.Equal(t, []interface{}{"participant"}, got[0].Data["relations"])
		})
	}
}

func TestPublishVoteResponseEvent_MaintainsParticipant(t *testing.T) {
	data := &domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai"}

	for _, action := range []string{"created", "updated"} {
		t.Run(action+" writes owner on the response and participant on the vote", func(t *testing.T) {
			publisher, drain := newCapturingPublisher(t, WithParticipantGrants(true))
			require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), action, data))

			var subjects []string
			for _, m := range drain() {
				subjects = append(subjects, m.Subject)
			}
			assert.ElementsMatch(t, []string{fgaconstants.GenericUpdateAccessSubject, fgaconstants.GenericMemberPutSubject}, subjects)
		})
	}

	t.Run("deleted removes the response and, when the voter is known, the participant", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t, WithParticipantGrants(true))
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", data))

		var subjects []string
		var remove fgaMessage
		for _, m := range drain() {
			subjects = append(subjects, m.Subject)
			if m.Subject == fgaconstants.GenericMemberRemoveSubject {
				remove = m
			}
		}
		assert.ElementsMatch(t, []string{fgaconstants.GenericDeleteAccessSubject, fgaconstants.GenericMemberRemoveSubject}, subjects)
		assert.Equal(t, "vote", remove.ObjectType)
		assert.Equal(t, "vote-1", remove.Data["uid"])
		assert.Equal(t, "nickcoai", remove.Data["username"])
	})

	t.Run("deleted without a known voter only deletes the response", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t)
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", &domain.VoteResponseData{UID: "resp-1"}))

		got := drain()
		require.Len(t, got, 1)
		assert.Equal(t, fgaconstants.GenericDeleteAccessSubject, got[0].Subject)
	})
}

func TestSendVoteAccessMessage_PreservesParticipantTuples(t *testing.T) {
	publisher, drain := newCapturingPublisher(t)

	require.NoError(t, publisher.sendVoteAccessMessage(context.Background(), &domain.VoteData{
		VoteUID: "vote-1", ProjectUID: "proj-1", CommitteeUID: "comm-1",
	}))

	got := drain()
	require.Len(t, got, 1)
	assert.Equal(t, fgaconstants.GenericUpdateAccessSubject, got[0].Subject)
	// update_access is a full sync: without this, every vote update would delete the participants.
	assert.Equal(t, []interface{}{"participant"}, got[0].Data["exclude_relations"])
}

// Until responses are reindexed to vote_response#auditor, granting vote#viewer would let
// participants read other voters' ballots, so grants are off by default; revocations never are.
func TestPublishVoteResponseEvent_ParticipantGrantsAreGated(t *testing.T) {
	data := &domain.VoteResponseData{UID: "resp-1", VoteUID: "vote-1", Username: "nickcoai"}

	subjectsOf := func(msgs []fgaMessage) []string {
		var out []string
		for _, m := range msgs {
			out = append(out, m.Subject)
		}
		return out
	}

	for _, action := range []string{"created", "updated"} {
		t.Run(action+" with grants off writes the owner tuple but no participant", func(t *testing.T) {
			publisher, drain := newCapturingPublisher(t) // default: off
			require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), action, data))

			assert.Equal(t, []string{fgaconstants.GenericUpdateAccessSubject}, subjectsOf(drain()))
		})
	}

	t.Run("a removed voter is revoked even with grants off", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t)
		removed := *data
		removed.VoterRemoved = true
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "updated", &removed))

		assert.ElementsMatch(t,
			[]string{fgaconstants.GenericUpdateAccessSubject, fgaconstants.GenericMemberRemoveSubject},
			subjectsOf(drain()))
	})

	t.Run("a deleted response is revoked even with grants off", func(t *testing.T) {
		publisher, drain := newCapturingPublisher(t)
		require.NoError(t, publisher.PublishVoteResponseEvent(context.Background(), "deleted", data))

		assert.ElementsMatch(t,
			[]string{fgaconstants.GenericDeleteAccessSubject, fgaconstants.GenericMemberRemoveSubject},
			subjectsOf(drain()))
	})
}

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
