// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reindexFixture struct {
	conn       *nats.Conn
	js         jetstream.JetStream
	objectsKV  jetstream.KeyValue
	mappingsKV jetstream.KeyValue
}

func newReindexFixture(t *testing.T) *reindexFixture {
	t.Helper()
	_, url := startTestNATSServer(t)

	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	js, err := jetstream.New(conn)
	require.NoError(t, err)

	ctx := context.Background()
	objectsKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: V1ObjectsBucket})
	require.NoError(t, err)
	mappingsKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: V1MappingsBucket})
	require.NoError(t, err)

	return &reindexFixture{conn: conn, js: js, objectsKV: objectsKV, mappingsKV: mappingsKV}
}

func (f *reindexFixture) putResponse(t *testing.T, id string, record map[string]any) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	_, err = f.objectsKV.Put(context.Background(), "itx-poll-vote."+id, data)
	require.NoError(t, err)
}

func (f *reindexFixture) mapVote(t *testing.T, pollID string, value string) {
	t.Helper()
	_, err := f.mappingsKV.Put(context.Background(), "vote."+pollID, []byte(value))
	require.NoError(t, err)
}

func (f *reindexFixture) revision(t *testing.T, key string) uint64 {
	t.Helper()
	entry, err := f.objectsKV.Get(context.Background(), key)
	require.NoError(t, err)
	return entry.Revision()
}

func (f *reindexFixture) mapResponse(t *testing.T, id, value string) {
	t.Helper()
	_, err := f.mappingsKV.Put(context.Background(), "vote_response."+id, []byte(value))
	require.NoError(t, err)
}

// seedReplayRecords writes one record per shape the replay must handle and returns the keys
// that should be replayed.
func (f *reindexFixture) seedReplayRecords(t *testing.T) []string {
	f.mapVote(t, "vote-1", "1")
	f.mapVote(t, "vote-gone", "!del")

	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	f.putResponse(t, "r2", map[string]any{"poll_id": "vote-1", "username": ""}) // email-only voters are replayed too: the index must be fixed for them
	f.mapResponse(t, "r2", "1")
	f.putResponse(t, "r3", map[string]any{"poll_id": "vote-1", "username": "bob"}) // never processed: no response mapping
	f.putResponse(t, "r4", map[string]any{"poll_id": "vote-1", "username": "carol", "_sdc_deleted_at": "2026"})
	f.mapResponse(t, "r4", "1")
	f.putResponse(t, "r5", map[string]any{"poll_id": "vote-1", "username": "dave"})
	f.mapResponse(t, "r5", "!del") // response already deleted
	f.putResponse(t, "r6", map[string]any{"poll_id": "vote-gone", "username": "erin"})
	f.mapResponse(t, "r6", "1") // parent tombstoned
	f.putResponse(t, "r7", map[string]any{"poll_id": "vote-unmapped", "username": "frank"})
	f.mapResponse(t, "r7", "1") // parent never mapped

	return []string{"itx-poll-vote.r1", "itx-poll-vote.r2"}
}

// osHit is an indexed vote_response document as the OpenSearch scroll returns it.
func osHit(objectID, voteUID string) map[string]any {
	return map[string]any{"_source": map[string]any{
		"object_id": objectID,
		"data":      map[string]any{"vote_uid": voteUID, "poll_id": voteUID},
	}}
}

// newOpenSearchStub serves the scroll API: the first search returns pages[0], each scroll call
// the next page, and an empty page ends it. It returns the base URL and the count of searches.
func newOpenSearchStub(t *testing.T, pages ...[]map[string]any) (string, func() int) {
	t.Helper()
	var mu sync.Mutex
	next, searches := 0, 0

	respond := func(w http.ResponseWriter) {
		hits := []map[string]any{}
		if next < len(pages) {
			hits = pages[next]
		}
		next++
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"_scroll_id": "scroll-1",
			"hits":       map[string]any{"hits": hits},
		}))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/resources/_search":
			searches++
			respond(w)
		case r.Method == http.MethodPost && r.URL.Path == "/_search/scroll":
			respond(w)
		case r.Method == http.MethodDelete && r.URL.Path == "/_search/scroll":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() int { mu.Lock(); defer mu.Unlock(); return searches }
}

// indexedPages lists every seeded response as indexed, split over two scroll pages, plus the
// shapes that only exist in the index: a duplicate, a document with no object_id, and a response
// that is indexed but has no record in v1-objects.
func indexedPages() [][]map[string]any {
	return [][]map[string]any{
		{osHit("r1", "vote-1"), osHit("r2", "vote-1"), osHit("r3", "vote-1"), osHit("r4", "vote-1")},
		{osHit("r5", "vote-1"), osHit("r6", "vote-gone"), osHit("r7", "vote-unmapped"),
			osHit("r1", "vote-1"), osHit("", "vote-1"), osHit("ghost", "vote-1")},
	}
}

func TestReplayVoteResponses_DryRunWritesNothing(t *testing.T) {
	f := newReindexFixture(t)
	want := f.seedReplayRecords(t)
	before := f.revision(t, want[0])
	osURL, _ := newOpenSearchStub(t, indexedPages()...)

	res, err := ReplayVoteResponses(context.Background(), f.js, ReplayVoteResponsesOptions{OpenSearchURL: osURL}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, before, f.revision(t, want[0]), "dry run must not re-put")
	assert.ElementsMatch(t, want, res.Keys)
	assert.Equal(t, 10, res.Scanned, "every indexed document across both scroll pages")
	assert.Equal(t, 1, res.Duplicates)
	assert.Equal(t, 1, res.SkippedNoID)
	assert.Equal(t, 1, res.NotFound)
	assert.Equal(t, 2, res.Eligible)
	assert.Equal(t, 1, res.SkippedNoMap)
	assert.Equal(t, 1, res.SkippedDeleted)
	assert.Equal(t, 1, res.SkippedTomb)
	assert.Equal(t, 2, res.SkippedNoParent)
	assert.Zero(t, res.Replayed)
}

// v1-objects holds every v1 object type; the replay must only touch the responses OpenSearch lists.
func TestReplayVoteResponses_OnlyTouchesIndexedResponses(t *testing.T) {
	f := newReindexFixture(t)
	f.mapVote(t, "vote-1", "1")
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	// processed and eligible, but not in the index: not this tool's job
	f.putResponse(t, "r2", map[string]any{"poll_id": "vote-1", "username": "bob"})
	f.mapResponse(t, "r2", "1")
	_, err := f.objectsKV.Put(context.Background(), "itx-zoom-meetings-v2.m1", []byte(`{"id":"m1"}`))
	require.NoError(t, err)
	r2Before := f.revision(t, "itx-poll-vote.r2")
	meetingBefore := f.revision(t, "itx-zoom-meetings-v2.m1")

	osURL, _ := newOpenSearchStub(t, []map[string]any{osHit("r1", "vote-1")})
	res, err := ReplayVoteResponses(context.Background(), f.js,
		ReplayVoteResponsesOptions{OpenSearchURL: osURL, Apply: true, RatePerSecond: 1000}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, []string{"itx-poll-vote.r1"}, res.Keys)
	assert.Equal(t, r2Before, f.revision(t, "itx-poll-vote.r2"))
	assert.Equal(t, meetingBefore, f.revision(t, "itx-zoom-meetings-v2.m1"))
}

func TestReplayVoteResponses_ApplyRePutsEligibleRecordsWithTheirOwnBytes(t *testing.T) {
	f := newReindexFixture(t)
	want := f.seedReplayRecords(t)
	skipped := f.revision(t, "itx-poll-vote.r3")
	beforeValue, err := f.objectsKV.Get(context.Background(), want[0])
	require.NoError(t, err)
	osURL, _ := newOpenSearchStub(t, indexedPages()...)

	// What the voting consumer would see: messages on the subject it filters on.
	var mu sync.Mutex
	var seen []string
	sub, err := f.conn.Subscribe("$KV.v1-objects.itx-poll-vote.>", func(m *nats.Msg) {
		mu.Lock()
		seen = append(seen, m.Subject)
		mu.Unlock()
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, f.conn.Flush())

	res, err := ReplayVoteResponses(context.Background(), f.js,
		ReplayVoteResponsesOptions{OpenSearchURL: osURL, Apply: true, RatePerSecond: 1000}, slog.Default())
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, 2, res.Replayed)
	assert.Zero(t, res.Conflicts)
	assert.Zero(t, res.Failed)

	afterValue, err := f.objectsKV.Get(context.Background(), want[0])
	require.NoError(t, err)
	assert.Greater(t, afterValue.Revision(), beforeValue.Revision(), "a new revision triggers the consumer")
	assert.Equal(t, beforeValue.Value(), afterValue.Value(), "bytes must be unchanged")
	assert.Equal(t, skipped, f.revision(t, "itx-poll-vote.r3"), "records that are not eligible are untouched")

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"$KV.v1-objects." + want[0], "$KV.v1-objects." + want[1]}, seen)
}

func TestReplayVoteResponses_VoteUIDFilterUsesTheIndexedVote(t *testing.T) {
	f := newReindexFixture(t)
	f.mapVote(t, "vote-1", "1")
	f.mapVote(t, "vote-2", "1")
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	f.putResponse(t, "r2", map[string]any{"poll_id": "vote-2", "username": "bob"})
	f.mapResponse(t, "r2", "1")
	osURL, _ := newOpenSearchStub(t, []map[string]any{osHit("r1", "vote-1"), osHit("r2", "vote-2")})

	res, err := ReplayVoteResponses(context.Background(), f.js,
		ReplayVoteResponsesOptions{OpenSearchURL: osURL, VoteUID: "vote-2"}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, []string{"itx-poll-vote.r2"}, res.Keys)
	assert.Equal(t, 1, res.SkippedOther)
}

func TestReplayVoteResponses_FailsWhenOpenSearchIsUnavailable(t *testing.T) {
	f := newReindexFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := ReplayVoteResponses(context.Background(), f.js, ReplayVoteResponsesOptions{OpenSearchURL: srv.URL}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OpenSearch")
}

// The bucket is written by the upstream sync; a stale replay must never overwrite newer data.
func TestReplayRecord_DoesNotOverwriteARecordChangedSinceItWasRead(t *testing.T) {
	f := newReindexFixture(t)
	ctx := context.Background()
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})

	stale, err := f.objectsKV.Get(ctx, "itx-poll-vote.r1")
	require.NoError(t, err)

	newer := []byte(`{"poll_id":"vote-1","username":"alice","vote_status":"ended"}`)
	_, err = f.objectsKV.Put(ctx, "itx-poll-vote.r1", newer)
	require.NoError(t, err)

	err = replayRecord(ctx, f.objectsKV, stale)
	require.ErrorIs(t, err, jetstream.ErrKeyExists)

	current, err := f.objectsKV.Get(ctx, "itx-poll-vote.r1")
	require.NoError(t, err)
	assert.Equal(t, newer, current.Value(), "the newer upstream write must survive")
}
