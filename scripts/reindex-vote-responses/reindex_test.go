// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	conn       *nats.Conn
	js         jetstream.JetStream
	objectsKV  jetstream.KeyValue
	mappingsKV jetstream.KeyValue
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	// Keep retried reads fast; the production delays are seconds.
	orig := readRetryDelays
	readRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { readRetryDelays = orig })

	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir()})
	require.NoError(t, err)
	go ns.Start()
	require.True(t, ns.ReadyForConnections(4*time.Second), "NATS server not ready")
	t.Cleanup(ns.Shutdown)

	conn, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	js, err := jetstream.New(conn)
	require.NoError(t, err)

	ctx := context.Background()
	objectsKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "v1-objects"})
	require.NoError(t, err)
	mappingsKV, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "v1-mappings"})
	require.NoError(t, err)

	return &fixture{conn: conn, js: js, objectsKV: objectsKV, mappingsKV: mappingsKV}
}

func (f *fixture) putResponse(t *testing.T, id string, rec map[string]any) {
	t.Helper()
	data, err := json.Marshal(rec)
	require.NoError(t, err)
	_, err = f.objectsKV.Put(context.Background(), "itx-poll-vote."+id, data)
	require.NoError(t, err)
}

func (f *fixture) mapVote(t *testing.T, pollID, value string) {
	t.Helper()
	_, err := f.mappingsKV.Put(context.Background(), "vote."+pollID, []byte(value))
	require.NoError(t, err)
}

func (f *fixture) mapResponse(t *testing.T, id, value string) {
	t.Helper()
	_, err := f.mappingsKV.Put(context.Background(), "vote_response."+id, []byte(value))
	require.NoError(t, err)
}

func (f *fixture) revision(t *testing.T, key string) uint64 {
	t.Helper()
	entry, err := f.objectsKV.Get(context.Background(), key)
	require.NoError(t, err)
	return entry.Revision()
}

// seedRecords writes one record per shape the reindex must handle and returns the keys that
// should be replayed.
func (f *fixture) seedRecords(t *testing.T) []string {
	f.mapVote(t, "vote-1", "1")
	f.mapVote(t, "vote-gone", "!del")

	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	f.putResponse(t, "r2", map[string]any{"poll_id": "vote-1", "username": ""}) // email-only voters are replayed too: their index must be fixed
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
	return map[string]any{
		"_id": "vote_response:" + objectID,
		"_source": map[string]any{
			"object_id": objectID,
			"data":      map[string]any{"vote_uid": voteUID, "poll_id": voteUID},
		},
	}
}

// osHitChecked is osHit with the document's indexed access check.
func osHitChecked(objectID, voteUID, check string) map[string]any {
	h := osHit(objectID, voteUID)
	h["_source"].(map[string]any)["access_check_query"] = check
	return h
}

type osStub struct {
	URL string

	mu           sync.Mutex
	deleteScroll []string // scroll_id of each DELETE /_search/scroll
	keepAlives   []string // the scroll keep-alive sent with the search and every scroll request
}

func (s *osStub) scrollKeepAlives() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keepAlives...)
}

func (s *osStub) deletedScrollIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleteScroll...)
}

// newOpenSearchStub serves the scroll API: the first search returns pages[0], each scroll call the
// next page, and an empty page ends it. failPageAt makes that (0-based) scroll page return a 500.
// extra lets a test add fields (timed_out, _shards) to every response.
func newOpenSearchStub(t *testing.T, failPageAt int, extra map[string]any, pages ...[]map[string]any) *osStub {
	t.Helper()
	stub := &osStub{}
	next := 0

	respond := func(w http.ResponseWriter) {
		if next == failPageAt {
			next++
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		hits := []map[string]any{}
		if next < len(pages) {
			hits = pages[next]
		}
		next++
		body := map[string]any{"_scroll_id": "scroll-1", "hits": map[string]any{"hits": hits}}
		for k, v := range extra {
			body[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/resources/_search":
			stub.keepAlives = append(stub.keepAlives, r.URL.Query().Get("scroll"))
			respond(w)
		case r.Method == http.MethodPost && r.URL.Path == "/_search/scroll":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(raw, &body)
			stub.keepAlives = append(stub.keepAlives, body["scroll"])
			respond(w)
		case r.Method == http.MethodDelete && r.URL.Path == "/_search/scroll":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(raw, &body)
			stub.deleteScroll = append(stub.deleteScroll, body["scroll_id"])
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	stub.URL = srv.URL
	return stub
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

func collect(keys *[]string) func(string) {
	return func(k string) { *keys = append(*keys, k) }
}

func TestReindex_DryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	want := f.seedRecords(t)
	before := f.revision(t, want[0])
	stub := newOpenSearchStub(t, -1, nil, indexedPages()...)

	var keys []string
	res, err := reindex(context.Background(), f.js, options{OpenSearchURL: stub.URL, OnEligible: collect(&keys)}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, before, f.revision(t, want[0]), "dry run must not re-put")
	assert.ElementsMatch(t, want, keys)
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

// v1-objects holds every v1 object type; the reindex must only touch the responses OpenSearch lists.
func TestReindex_OnlyTouchesIndexedResponses(t *testing.T) {
	f := newFixture(t)
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

	stub := newOpenSearchStub(t, -1, nil, []map[string]any{osHit("r1", "vote-1")})
	var keys []string
	_, err = reindex(context.Background(), f.js,
		options{OpenSearchURL: stub.URL, Apply: true, RatePerSecond: 1000, OnEligible: collect(&keys)}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, []string{"itx-poll-vote.r1"}, keys)
	assert.Equal(t, r2Before, f.revision(t, "itx-poll-vote.r2"))
	assert.Equal(t, meetingBefore, f.revision(t, "itx-zoom-meetings-v2.m1"))
}

func TestReindex_ApplyRePutsEligibleRecordsWithTheirOwnBytes(t *testing.T) {
	f := newFixture(t)
	want := f.seedRecords(t)
	skipped := f.revision(t, "itx-poll-vote.r3")
	beforeValue, err := f.objectsKV.Get(context.Background(), want[0])
	require.NoError(t, err)
	stub := newOpenSearchStub(t, -1, nil, indexedPages()...)

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

	res, err := reindex(context.Background(), f.js,
		options{OpenSearchURL: stub.URL, Apply: true, RatePerSecond: 1000}, slog.Default())
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

func TestReindex_VoteUIDFilterUsesTheIndexedVote(t *testing.T) {
	f := newFixture(t)
	f.mapVote(t, "vote-1", "1")
	f.mapVote(t, "vote-2", "1")
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	f.putResponse(t, "r2", map[string]any{"poll_id": "vote-2", "username": "bob"})
	f.mapResponse(t, "r2", "1")
	stub := newOpenSearchStub(t, -1, nil, []map[string]any{osHit("r1", "vote-1"), osHit("r2", "vote-2")})

	var keys []string
	res, err := reindex(context.Background(), f.js,
		options{OpenSearchURL: stub.URL, VoteUID: "vote-2", OnEligible: collect(&keys)}, slog.Default())
	require.NoError(t, err)

	assert.Equal(t, []string{"itx-poll-vote.r2"}, keys)
	assert.Equal(t, 1, res.SkippedOther)
}

func TestReindex_FailsWhenOpenSearchIsUnavailable(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := reindex(context.Background(), f.js, options{OpenSearchURL: srv.URL}, slog.Default())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OpenSearch")
}

// A partial OpenSearch result is a subset of the documents; treating it as complete would leave
// some responses unprocessed while the run looks successful.
func TestReindex_FailsOnAPartialOpenSearchResult(t *testing.T) {
	tests := map[string]map[string]any{
		"timed out":     {"timed_out": true},
		"failed shards": {"_shards": map[string]any{"failed": 1}},
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.mapVote(t, "vote-1", "1")
			f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
			f.mapResponse(t, "r1", "1")
			stub := newOpenSearchStub(t, -1, extra, []map[string]any{osHit("r1", "vote-1")})

			_, err := reindex(context.Background(), f.js, options{OpenSearchURL: stub.URL}, slog.Default())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "partial result")
		})
	}
}

// After a failed page fetch the scroll must still be released with the last valid ID, not "".
func TestReindex_ReleasesTheScrollWithTheLastGoodID(t *testing.T) {
	f := newFixture(t)
	f.mapVote(t, "vote-1", "1")
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")
	// page 0 (the search) succeeds, page 1 (the first scroll call) fails
	stub := newOpenSearchStub(t, 1, nil, []map[string]any{osHit("r1", "vote-1")})

	_, err := reindex(context.Background(), f.js, options{OpenSearchURL: stub.URL}, slog.Default())

	require.Error(t, err)
	assert.Equal(t, []string{"scroll-1"}, stub.deletedScrollIDs())
}

// failingKV fails Get for one key with a non-"not found" error, like a NATS timeout would.
type failingKV struct {
	jetstream.KeyValue
	failKey string
}

func (k failingKV) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	if key == k.failKey {
		return nil, errors.New("nats: timeout")
	}
	return k.KeyValue.Get(ctx, key)
}

// A read that keeps failing (after retries) must abort the run, not be counted as "never
// processed" (and, for the parent vote, cached as unmapped so every later response of that vote is
// silently skipped).
func TestReindex_APersistentReadErrorAbortsTheRun(t *testing.T) {
	tests := map[string]string{
		"response mapping": "vote_response.r1",
		"parent mapping":   "vote.vote-1",
		"record":           "itx-poll-vote.r1",
	}
	for name, failKey := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.mapVote(t, "vote-1", "1")
			f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
			f.mapResponse(t, "r1", "1")

			res := &result{}
			r := &runner{
				objectsKV:  failingKV{KeyValue: f.objectsKV, failKey: failKey},
				mappingsKV: failingKV{KeyValue: f.mappingsKV, failKey: failKey},
				res:        res,
				logger:     slog.Default(),
				seen:       map[string]bool{},
				parentOK:   map[string]bool{},
			}

			err := r.process(context.Background(), openSearchHit{Source: openSearchSource{ObjectID: "r1"}})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "nats: timeout")
			assert.Contains(t, err.Error(), "after 4 attempts")
			assert.Zero(t, res.SkippedNoMap)
			assert.Zero(t, res.SkippedNoParent)
			assert.Zero(t, res.NotFound)
			assert.Empty(t, r.parentOK, "a failed read must not be cached as an answer")
		})
	}
}

// The bucket is written by the upstream sync; a stale replay must never overwrite newer data.
func TestReplayRecord_DoesNotOverwriteARecordChangedSinceItWasRead(t *testing.T) {
	f := newFixture(t)
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

// The next page is requested only after the current page's records are re-put, which takes
// pageSize / rate seconds. The scroll must outlive that, or a throttled run dies before page two.
func TestScrollKeepAlive_OutlivesAPageAtTheConfiguredRate(t *testing.T) {
	tests := []struct {
		rate int
		want string
	}{
		{rate: 1, want: "520s"},    // 200s per page, doubled, plus the 2m floor
		{rate: 50, want: "128s"},   // the default: 4s per page
		{rate: 0, want: "128s"},    // unset falls back to the default rate
		{rate: 1000, want: "122s"}, // a page takes under a second
	}
	for _, tt := range tests {
		got := scrollKeepAlive(tt.rate)
		assert.Equal(t, tt.want, got, "rate %d", tt.rate)

		d, err := time.ParseDuration(got)
		require.NoError(t, err)
		rate := tt.rate
		if rate <= 0 {
			rate = defaultRatePerSecond
		}
		pageTime := time.Duration((openSearchPageSize+rate-1)/rate) * time.Second
		assert.Greater(t, d, pageTime, "rate %d: the scroll must outlive a full page", tt.rate)
	}
}

func TestReindex_SendsTheRateDerivedKeepAliveOnEveryScrollRequest(t *testing.T) {
	f := newFixture(t)
	f.seedRecords(t)
	stub := newOpenSearchStub(t, -1, nil, indexedPages()...)

	_, err := reindex(context.Background(), f.js, options{OpenSearchURL: stub.URL, RatePerSecond: 1}, slog.Default())
	require.NoError(t, err)

	got := stub.scrollKeepAlives()
	require.GreaterOrEqual(t, len(got), 3, "the search plus one scroll request per further page")
	for _, k := range got {
		assert.Equal(t, "520s", k)
	}
}

// flakyKV fails the first n Gets of one key with a transient error, then reads normally.
type flakyKV struct {
	jetstream.KeyValue
	key   string
	n     int
	calls int
}

func (k *flakyKV) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	if key == k.key {
		k.calls++
		if k.calls <= k.n {
			return nil, errors.New("nats: timeout")
		}
	}
	return k.KeyValue.Get(ctx, key)
}

// One timeout in a long run must not abort it: the read is retried and the run carries on.
func TestReindex_ATransientReadErrorIsRetried(t *testing.T) {
	f := newFixture(t)
	f.mapVote(t, "vote-1", "1")
	f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
	f.mapResponse(t, "r1", "1")

	flaky := &flakyKV{KeyValue: f.mappingsKV, key: "vote_response.r1", n: 2}
	res := &result{}
	r := &runner{
		objectsKV: f.objectsKV, mappingsKV: flaky, res: res, logger: slog.Default(),
		seen: map[string]bool{}, parentOK: map[string]bool{},
	}

	err := r.process(context.Background(), openSearchHit{Source: openSearchSource{ObjectID: "r1"}})

	require.NoError(t, err)
	assert.Equal(t, 3, flaky.calls, "two failures, then the successful read")
	assert.Equal(t, 1, res.Eligible)
	assert.Zero(t, res.SkippedNoMap, "a retried read that then succeeds is a normal record, not a skip")
}

// "Not found" is an answer, not an error: it is never retried.
func TestGetEntry_DoesNotRetryAMissingKey(t *testing.T) {
	f := newFixture(t)
	counting := &flakyKV{KeyValue: f.mappingsKV, key: "vote.missing", n: 0}

	_, found, err := getEntry(context.Background(), counting, "vote.missing", slog.Default())

	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, 1, counting.calls, "a missing key is read once")
}

// An interrupted run is resumed by skipping what is already on the new check; documents still on
// the old check, and those with no access check at all, are processed.
func TestReindex_SkipReindexedSkipsOnlyDocumentsOnTheNewCheck(t *testing.T) {
	f := newFixture(t)
	f.mapVote(t, "vote-1", "1")
	for _, id := range []string{"r1", "r2", "r3"} {
		f.putResponse(t, id, map[string]any{"poll_id": "vote-1", "username": "u-" + id})
		f.mapResponse(t, id, "1")
	}
	pages := []map[string]any{
		osHitChecked("r1", "vote-1", "vote_response:r1#auditor"), // done
		osHitChecked("r2", "vote-1", "vote:vote-1#viewer"),       // old
		osHit("r3", "vote-1"), // no access check indexed
	}

	t.Run("on", func(t *testing.T) {
		stub := newOpenSearchStub(t, -1, nil, pages)
		var keys []string
		res, err := reindex(context.Background(), f.js,
			options{OpenSearchURL: stub.URL, SkipReindexed: true, OnEligible: collect(&keys)}, slog.Default())
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"itx-poll-vote.r2", "itx-poll-vote.r3"}, keys)
		assert.Equal(t, 1, res.SkippedReindexed)
	})

	// The pass after enabling participant grants must re-put everything, including what is already
	// on the new check, so the flag is off by default.
	t.Run("off by default", func(t *testing.T) {
		stub := newOpenSearchStub(t, -1, nil, pages)
		var keys []string
		res, err := reindex(context.Background(), f.js,
			options{OpenSearchURL: stub.URL, OnEligible: collect(&keys)}, slog.Default())
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"itx-poll-vote.r1", "itx-poll-vote.r2", "itx-poll-vote.r3"}, keys)
		assert.Zero(t, res.SkippedReindexed)
	})
}

// The remediation pass replays only pseudo-anonymous votes' responses so the live handler
// re-indexes them without ballot data. It fails closed like the handler: a parent poll record
// missing the flag, or missing entirely, counts as pseudo-anonymous and is replayed.
func TestReindex_PseudoAnonymousOnlySkipsNamedVotes(t *testing.T) {
	f := newFixture(t)
	putPoll := func(pollID string, rec map[string]any) {
		data, err := json.Marshal(rec)
		require.NoError(t, err)
		_, err = f.objectsKV.Put(context.Background(), "itx-poll."+pollID, data)
		require.NoError(t, err)
	}
	putPoll("vote-pseudo", map[string]any{"poll_id": "vote-pseudo", "pseudo_anonymity": true})
	putPoll("vote-named", map[string]any{"poll_id": "vote-named", "pseudo_anonymity": false})
	putPoll("vote-noflag", map[string]any{"poll_id": "vote-noflag"})
	// vote-norecord has no itx-poll record at all.

	responses := map[string]string{
		"r-pseudo":   "vote-pseudo",
		"r-named-1":  "vote-named",
		"r-named-2":  "vote-named", // second response on the same poll exercises the per-poll cache
		"r-noflag":   "vote-noflag",
		"r-norecord": "vote-norecord",
	}
	var pages []map[string]any
	for _, pollID := range []string{"vote-pseudo", "vote-named", "vote-noflag", "vote-norecord"} {
		f.mapVote(t, pollID, "1")
	}
	for id, pollID := range responses {
		f.putResponse(t, id, map[string]any{"poll_id": pollID, "username": "u-" + id})
		f.mapResponse(t, id, "1")
		pages = append(pages, osHit(id, pollID))
	}

	t.Run("on", func(t *testing.T) {
		stub := newOpenSearchStub(t, -1, nil, pages)
		var keys []string
		res, err := reindex(context.Background(), f.js,
			options{OpenSearchURL: stub.URL, PseudoAnonymousOnly: true, OnEligible: collect(&keys)}, slog.Default())
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"itx-poll-vote.r-pseudo", "itx-poll-vote.r-noflag", "itx-poll-vote.r-norecord"}, keys)
		assert.Equal(t, 2, res.SkippedNamed)
		assert.Equal(t, 3, res.Eligible)
	})

	t.Run("off by default", func(t *testing.T) {
		stub := newOpenSearchStub(t, -1, nil, pages)
		var keys []string
		res, err := reindex(context.Background(), f.js,
			options{OpenSearchURL: stub.URL, OnEligible: collect(&keys)}, slog.Default())
		require.NoError(t, err)

		assert.Len(t, keys, len(responses))
		assert.Zero(t, res.SkippedNamed)
	})

	// Skipping documents already on the new access check would leave their ballot data in place.
	t.Run("rejects skip-reindexed", func(t *testing.T) {
		stub := newOpenSearchStub(t, -1, nil, pages)
		res, err := reindex(context.Background(), f.js,
			options{OpenSearchURL: stub.URL, PseudoAnonymousOnly: true, SkipReindexed: true}, slog.Default())
		require.ErrorContains(t, err, "-skip-reindexed")
		assert.Nil(t, res)
	})
}

// A privacy replay cannot replace documents stored under historical IDs. Every hit must be
// checked, even when its object_id was already replayed from an earlier hit or scroll page.
func TestReindex_PseudoAnonymousOnlyRejectsNoncanonicalCopies(t *testing.T) {
	for _, apply := range []bool{false, true} {
		for _, shape := range []string{"legacy only", "duplicate same page", "duplicate later page", "missing document ID"} {
			t.Run(fmt.Sprintf("apply=%t/%s", apply, shape), func(t *testing.T) {
				f := newFixture(t)
				f.mapVote(t, "vote-1", "1")
				f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
				f.mapResponse(t, "r1", "1")
				before := f.revision(t, "itx-poll-vote.r1")
				legacy := osHit("r1", "vote-1")
				legacy["_id"] = "historical-r1"
				pages := [][]map[string]any{{legacy}}
				switch shape {
				case "duplicate same page":
					pages = [][]map[string]any{{osHit("r1", "vote-1"), legacy}}
				case "duplicate later page":
					pages = [][]map[string]any{{osHit("r1", "vote-1")}, {legacy}}
				case "missing document ID":
					delete(legacy, "_id")
				}
				stub := newOpenSearchStub(t, -1, nil, pages...)

				res, err := reindex(context.Background(), f.js, options{
					OpenSearchURL: stub.URL, PseudoAnonymousOnly: true, Apply: apply, RatePerSecond: 1000,
				}, slog.Default())

				require.ErrorContains(t, err, "noncanonical OpenSearch document")
				assert.Contains(t, err.Error(), `object_id="r1"`)
				assert.Contains(t, err.Error(), "separately remove or redact")
				assert.Zero(t, res.Duplicates, "the unsafe copy must not be hidden by deduplication")
				if apply && (shape == "duplicate same page" || shape == "duplicate later page") {
					assert.Equal(t, 1, res.Replayed, "an abort may follow earlier canonical replays")
				} else {
					assert.Zero(t, res.Replayed)
					assert.Equal(t, before, f.revision(t, "itx-poll-vote.r1"))
				}
				assert.Equal(t, []string{"scroll-1"}, stub.deletedScrollIDs())
			})
		}
	}
}

func TestReindex_NoncanonicalCopiesRespectModeAndVoteScope(t *testing.T) {
	for _, privacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("privacy=%t", privacy), func(t *testing.T) {
			f := newFixture(t)
			f.mapVote(t, "vote-1", "1")
			f.putResponse(t, "r1", map[string]any{"poll_id": "vote-1", "username": "alice"})
			f.mapResponse(t, "r1", "1")
			legacy := osHit("r1", "vote-1")
			legacy["_id"] = "historical-r1"
			stub := newOpenSearchStub(t, -1, nil, []map[string]any{legacy})
			opts := options{OpenSearchURL: stub.URL, PseudoAnonymousOnly: privacy}
			if privacy {
				opts.VoteUID = "another-vote"
			}

			res, err := reindex(context.Background(), f.js, opts, slog.Default())

			require.NoError(t, err)
			if privacy {
				assert.Equal(t, 1, res.SkippedOther)
				assert.Zero(t, res.Eligible)
			} else {
				assert.Equal(t, 1, res.Eligible, "default replay retains its existing behavior")
			}
		})
	}
}
