// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	voteResponseKeyPrefix        = "itx-poll-vote."
	voteMappingKeyPrefix         = "vote."
	voteResponseMappingKeyPrefix = "vote_response."

	openSearchIndex    = "resources"
	openSearchPageSize = 200
	// minScrollKeepAlive is the floor for how long OpenSearch holds the scroll between page requests.
	minScrollKeepAlive = 2 * time.Minute

	// defaultRatePerSecond keeps a full reindex from outrunning the consumer; each record fans
	// out to the indexer and to fga-sync, which applies mutations one at a time.
	defaultRatePerSecond = 50
)

// scrollKeepAlive is how long OpenSearch must hold the scroll between page requests. The next page
// is requested only after every eligible record of the current one has been re-put, which takes
// pageSize / rate seconds at the configured rate. It doubles that to leave room for the KV reads
// per record and adds the floor, so a slow rate cannot outlive its own scroll.
func scrollKeepAlive(ratePerSecond int) string {
	if ratePerSecond <= 0 {
		ratePerSecond = defaultRatePerSecond
	}
	pageSeconds := (openSearchPageSize + ratePerSecond - 1) / ratePerSecond
	return fmt.Sprintf("%ds", 2*pageSeconds+int(minScrollKeepAlive.Seconds()))
}

// options configures reindex.
type options struct {
	// OpenSearchURL is the OpenSearch base URL used to find which vote responses are indexed.
	// Driving the run from the index (as lfx-v2-meeting-service's reindex_meetings does) avoids
	// listing every key of the v1-objects bucket, which holds all v1 object types and is far too
	// large to scan.
	OpenSearchURL string
	// HTTPClient is used for OpenSearch requests; defaults to a client with a 30s timeout.
	HTTPClient *http.Client
	// Apply re-puts the records. When false (the default) nothing is written and the result
	// reports what would be replayed.
	Apply bool
	// VoteUID restricts the run to the responses of a single vote (v2 UID, same as poll_id).
	VoteUID string
	// RatePerSecond caps how many records are re-put per second.
	RatePerSecond int
	// OnEligible is called with the key of every record that is (or, in a dry run, would be)
	// re-put, as it is found.
	OnEligible func(key string)
}

// result summarizes a run.
type result struct {
	Scanned         int // indexed vote_response documents read from OpenSearch
	SkippedNoID     int // indexed document without an object_id
	Duplicates      int // object_id seen more than once in the index
	SkippedOther    int // outside the VoteUID filter
	NotFound        int // indexed, but no itx-poll-vote record in v1-objects
	Undecodable     int // record present but not decodable
	SkippedDeleted  int // soft-deleted records (a replay would re-run the delete path)
	SkippedNoMap    int // never processed by the consumer: a replay would be a first create, not a reindex
	SkippedTomb     int // response mapping tombstoned (already deleted)
	SkippedNoParent int // parent vote missing or tombstoned in v1-mappings
	Eligible        int // records that will be / were re-put
	Replayed        int // re-put succeeded
	Conflicts       int // record changed since it was read; the newer write already triggered the consumer
	Failed          int // re-put failed
}

// reindex reindexes vote responses. It scrolls OpenSearch for the indexed `vote_response`
// documents, then re-puts each one's `itx-poll-vote.{object_id}` record in the v1-objects bucket
// with its existing bytes. The put lands as a new revision on the `$KV.v1-objects.itx-poll-vote.>`
// subject the voting consumer already watches, so each record goes through the live handler: the
// indexer message with the current IndexingConfig, the vote_response owner tuple, and (when
// participant grants are enabled on the service) the vote participant tuple.
//
// Each record is processed as it is read, so record payloads are never accumulated. The run does
// keep the set of object IDs seen so far (to drop duplicates) and one entry per parent vote, so
// memory grows with the number of distinct responses, but only by those IDs.
//
// The write is a compare-and-swap on the revision that was read. The bucket is written by the
// upstream sync, so a record that changed in between is never overwritten with stale bytes; it is
// counted as a conflict, and the newer write already went through the consumer.
//
// Only records the consumer has already processed are replayed (they have a vote_response
// mapping), so the handler treats them as updates and the LFID invite path, which fires only on a
// first create, does not run.
//
// A read that fails for any reason other than "not found" aborts the run with an error, so an
// operator never mistakes a partial run for a complete one.
func reindex(ctx context.Context, js jetstream.JetStream, opts options, logger *slog.Logger) (*result, error) {
	if opts.RatePerSecond <= 0 {
		opts.RatePerSecond = defaultRatePerSecond
	}

	objectsKV, err := js.KeyValue(ctx, eventing.V1ObjectsBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", eventing.V1ObjectsBucket, err)
	}
	mappingsKV, err := js.KeyValue(ctx, eventing.V1MappingsBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", eventing.V1MappingsBucket, err)
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	res := &result{}
	r := &runner{
		objectsKV:  objectsKV,
		mappingsKV: mappingsKV,
		opts:       opts,
		res:        res,
		logger:     logger,
		seen:       map[string]bool{},
		parentOK:   map[string]bool{},
	}
	if opts.Apply {
		ticker := time.NewTicker(time.Second / time.Duration(opts.RatePerSecond))
		defer ticker.Stop()
		r.tick = ticker.C
	}

	keepAlive := scrollKeepAlive(opts.RatePerSecond)
	scrollID, page, err := openScroll(ctx, client, opts.OpenSearchURL, keepAlive)
	if err != nil {
		return res, fmt.Errorf("open OpenSearch scroll: %w", err)
	}
	// lastScrollID always holds the most recent valid ID, so cleanup never sends an empty one
	// after a failed page fetch.
	lastScrollID := scrollID
	defer func() {
		if err := deleteScroll(context.WithoutCancel(ctx), client, opts.OpenSearchURL, lastScrollID); err != nil {
			logger.WarnContext(ctx, "failed to release OpenSearch scroll context", "err", err)
		}
	}()

	for len(page) > 0 {
		for _, hit := range page {
			if err := r.process(ctx, hit); err != nil {
				return res, err
			}
		}
		logger.InfoContext(ctx, "scanned indexed vote responses", "scanned", res.Scanned, "eligible", res.Eligible)

		nextHits, nextID, err := nextScrollPage(ctx, client, opts.OpenSearchURL, lastScrollID, keepAlive)
		if err != nil {
			return res, fmt.Errorf("OpenSearch scroll page: %w", err)
		}
		lastScrollID, page = nextID, nextHits
	}

	if res.Failed > 0 {
		return res, fmt.Errorf("%d of %d records failed to re-put", res.Failed, res.Eligible)
	}
	return res, nil
}

type runner struct {
	objectsKV, mappingsKV jetstream.KeyValue
	opts                  options
	res                   *result
	logger                *slog.Logger
	tick                  <-chan time.Time
	seen                  map[string]bool
	parentOK              map[string]bool
}

// process handles one indexed document: it applies every skip rule and, for an eligible record,
// re-puts it (when applying). It returns an error only for a failure that makes the run incomplete.
func (r *runner) process(ctx context.Context, hit openSearchHit) error {
	r.res.Scanned++
	id := hit.Source.ObjectID
	if id == "" {
		r.res.SkippedNoID++
		return nil
	}
	if r.seen[id] {
		r.res.Duplicates++
		return nil
	}
	r.seen[id] = true

	// Filter on the indexed vote so a single-vote run does not read every record.
	if r.opts.VoteUID != "" && hit.Source.voteUID() != r.opts.VoteUID {
		r.res.SkippedOther++
		return nil
	}

	key := voteResponseKeyPrefix + id
	entry, found, err := getEntry(ctx, r.objectsKV, key)
	if err != nil {
		return fmt.Errorf("read %s: %w", key, err)
	}
	if !found {
		r.res.NotFound++
		return nil
	}

	rec, err := decodeRecord(entry.Value())
	if err != nil {
		r.logger.WarnContext(ctx, "skipping undecodable vote response record", "key", key, "err", err)
		r.res.Undecodable++
		return nil
	}
	if rec.softDeleted {
		r.res.SkippedDeleted++
		return nil
	}

	respMapping, found, err := getEntry(ctx, r.mappingsKV, voteResponseMappingKeyPrefix+id)
	if err != nil {
		return fmt.Errorf("read mapping %s%s: %w", voteResponseMappingKeyPrefix, id, err)
	}
	if !found {
		r.res.SkippedNoMap++
		return nil
	}
	if isTombstoned(respMapping) {
		r.res.SkippedTomb++
		return nil
	}

	ok, err := r.parentMapped(ctx, rec.pollID)
	if err != nil {
		return err
	}
	if !ok {
		r.res.SkippedNoParent++
		return nil
	}

	r.res.Eligible++
	if r.opts.OnEligible != nil {
		r.opts.OnEligible(key)
	}
	if !r.opts.Apply {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.tick:
	}

	switch err := replayRecord(ctx, r.objectsKV, entry); {
	case err == nil:
		r.res.Replayed++
	case errors.Is(err, jetstream.ErrKeyExists):
		r.res.Conflicts++
		r.logger.InfoContext(ctx, "record changed since it was read, not overwriting", "key", key)
	default:
		r.res.Failed++
		r.logger.ErrorContext(ctx, "failed to re-put record", "key", key, "err", err)
	}
	return nil
}

// parentMapped reports whether the parent vote is mapped and not tombstoned. Only a definitive
// answer is cached; a read error aborts the run instead of being remembered as "not mapped".
func (r *runner) parentMapped(ctx context.Context, pollID string) (bool, error) {
	if pollID == "" {
		return false, nil
	}
	if ok, cached := r.parentOK[pollID]; cached {
		return ok, nil
	}
	mapping, found, err := getEntry(ctx, r.mappingsKV, voteMappingKeyPrefix+pollID)
	if err != nil {
		return false, fmt.Errorf("read mapping %s%s: %w", voteMappingKeyPrefix, pollID, err)
	}
	ok := found && !isTombstoned(mapping)
	r.parentOK[pollID] = ok
	return ok, nil
}

// replayRecord re-puts an entry's own bytes, guarded by the revision it was read at.
func replayRecord(ctx context.Context, kv jetstream.KeyValue, entry jetstream.KeyValueEntry) error {
	_, err := kv.Update(ctx, entry.Key(), entry.Value(), entry.Revision())
	return err
}

// getEntry reads a key, reporting a missing key as found=false and any other failure as an error.
func getEntry(ctx context.Context, kv jetstream.KeyValue, key string) (jetstream.KeyValueEntry, bool, error) {
	entry, err := kv.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return entry, true, nil
}

func isTombstoned(entry jetstream.KeyValueEntry) bool {
	return string(entry.Value()) == eventing.TombstoneMarker
}

// record is the part of an itx-poll-vote record the reindex reads.
type record struct {
	pollID      string
	softDeleted bool
}

// decodeRecord decodes a KV value (JSON, then msgpack) with the same string/bool coercion as the
// live handler, since Meltano sends some fields as strings.
func decodeRecord(value []byte) (record, error) {
	var v1Data map[string]any
	jsonErr := json.Unmarshal(value, &v1Data)
	if jsonErr != nil {
		if msgpackErr := msgpack.Unmarshal(value, &v1Data); msgpackErr != nil {
			return record{}, fmt.Errorf("not JSON (%v) or msgpack (%v)", jsonErr, msgpackErr)
		}
	}
	deletedAt, hasDeleted := v1Data["_sdc_deleted_at"]

	raw, err := json.Marshal(v1Data)
	if err != nil {
		return record{}, err
	}
	var rec domain.VoteDBRaw
	if err := json.Unmarshal(raw, &rec); err != nil {
		return record{}, err
	}
	return record{
		pollID:      rec.PollID,
		softDeleted: hasDeleted && deletedAt != nil && deletedAt != "",
	}, nil
}

// openSearchSource is the part of an indexed vote_response document the reindex reads.
type openSearchSource struct {
	ObjectID string `json:"object_id"`
	Data     struct {
		VoteUID string `json:"vote_uid"`
		PollID  string `json:"poll_id"`
	} `json:"data"`
}

// voteUID is the indexed vote's UID; legacy documents may only carry the v1 poll_id.
func (s openSearchSource) voteUID() string {
	if s.Data.VoteUID != "" {
		return s.Data.VoteUID
	}
	return s.Data.PollID
}

type openSearchHit struct {
	Source openSearchSource `json:"_source"`
}

type openSearchScrollResponse struct {
	ScrollID string `json:"_scroll_id"`
	TimedOut bool   `json:"timed_out"`
	Shards   struct {
		Failed int `json:"failed"`
	} `json:"_shards"`
	Hits struct {
		Hits []openSearchHit `json:"hits"`
	} `json:"hits"`
}

// openScroll opens a scroll over every indexed vote_response document.
func openScroll(ctx context.Context, client *http.Client, osURL, keepAlive string) (string, []openSearchHit, error) {
	body, err := json.Marshal(map[string]any{
		"query":   map[string]any{"term": map[string]any{"object_type": "vote_response"}},
		"_source": []string{"object_id", "data.vote_uid", "data.poll_id"},
		"size":    openSearchPageSize,
	})
	if err != nil {
		return "", nil, err
	}
	return doScroll(ctx, client, http.MethodPost,
		fmt.Sprintf("%s/%s/_search?scroll=%s", strings.TrimRight(osURL, "/"), openSearchIndex, keepAlive), body)
}

// nextScrollPage fetches the next page; an empty page means the scroll is exhausted.
func nextScrollPage(ctx context.Context, client *http.Client, osURL, scrollID, keepAlive string) ([]openSearchHit, string, error) {
	body, err := json.Marshal(map[string]string{"scroll": keepAlive, "scroll_id": scrollID})
	if err != nil {
		return nil, "", err
	}
	id, hits, err := doScroll(ctx, client, http.MethodPost, strings.TrimRight(osURL, "/")+"/_search/scroll", body)
	if err != nil {
		return nil, "", err
	}
	return hits, id, nil
}

// deleteScroll releases the scroll context held open on the OpenSearch cluster.
func deleteScroll(ctx context.Context, client *http.Client, osURL, scrollID string) error {
	body, err := json.Marshal(map[string]string{"scroll_id": scrollID})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(osURL, "/")+"/_search/scroll", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenSearch returned status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// doScroll runs one scroll request. A page that OpenSearch reports as partial (timed out, or any
// shard failed) is an error: its hits are a subset, so treating it as complete would leave some
// documents unprocessed while the run looks successful.
func doScroll(ctx context.Context, client *http.Client, method, url string, body []byte) (string, []openSearchHit, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read OpenSearch response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("OpenSearch returned status %d: %s", resp.StatusCode, string(raw))
	}
	var out openSearchScrollResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, fmt.Errorf("unmarshal OpenSearch response: %w", err)
	}
	if out.TimedOut || out.Shards.Failed > 0 {
		return "", nil, fmt.Errorf("OpenSearch returned a partial result (timed_out=%t, failed_shards=%d)", out.TimedOut, out.Shards.Failed)
	}
	return out.ScrollID, out.Hits.Hits, nil
}
