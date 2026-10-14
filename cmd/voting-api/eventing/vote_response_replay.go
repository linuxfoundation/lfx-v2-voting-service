// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

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

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	voteResponseKeyPrefix        = "itx-poll-vote."
	voteMappingKeyPrefix         = "vote."
	voteResponseMappingKeyPrefix = "vote_response."

	openSearchIndex    = "resources"
	openSearchPageSize = 200
	openSearchScroll   = "2m"

	// DefaultReindexRatePerSecond keeps a full reindex from outrunning the consumer; each record
	// fans out to the indexer and to fga-sync, which serializes mutations per object.
	DefaultReindexRatePerSecond = 50
)

// ReplayVoteResponsesOptions configures ReplayVoteResponses.
type ReplayVoteResponsesOptions struct {
	// OpenSearchURL is the OpenSearch base URL used to find which vote responses are indexed.
	// Driving the replay from the index (as lfx-v2-meeting-service's reindex_meetings does) avoids
	// listing every key of the v1-objects bucket, which holds all v1 object types and is far too
	// large to scan.
	OpenSearchURL string
	// HTTPClient is used for OpenSearch requests; defaults to a client with a 30s timeout.
	HTTPClient *http.Client
	// Apply re-puts the records. When false (the default) nothing is written and the result
	// reports what would be replayed.
	Apply bool
	// VoteUID restricts the replay to the responses of a single vote (v2 UID, same as poll_id).
	VoteUID string
	// RatePerSecond caps how many records are re-put per second, so the replay does not outrun
	// the consumer (each record fans out to the indexer and fga-sync).
	RatePerSecond int
}

// ReplayVoteResponsesResult summarizes a replay run.
type ReplayVoteResponsesResult struct {
	Scanned         int // indexed vote_response documents read from OpenSearch
	SkippedNoID     int // indexed document without an object_id
	Duplicates      int // object_id seen more than once in the index
	NotFound        int // indexed, but no itx-poll-vote record in v1-objects
	SkippedDeleted  int // soft-deleted records (a replay would re-run the delete path)
	SkippedOther    int // outside the VoteUID filter
	SkippedNoMap    int // never processed by the consumer: a replay would be a first create, not a reindex
	SkippedNoParent int // parent vote missing or tombstoned in v1-mappings
	SkippedTomb     int // response mapping tombstoned (already deleted)
	Eligible        int // records that will be / were re-put
	Replayed        int // re-put succeeded
	Conflicts       int // record changed since it was read; the newer write already triggered the consumer
	Failed          int // re-put failed
	Keys            []string
}

// ReplayVoteResponses reindexes vote responses. It scrolls OpenSearch for the indexed
// `vote_response` documents, then re-puts each one's `itx-poll-vote.{object_id}` record in the
// v1-objects bucket with its existing bytes. The put lands as a new revision on the
// `$KV.v1-objects.itx-poll-vote.>` subject the voting consumer already watches, so each record
// goes through the live handler (handleVoteResponseUpdate): the indexer message with the current
// IndexingConfig, the vote_response owner tuple, and the vote participant tuple. Use it after
// changing what the handler emits for existing responses, e.g. the access check or participants.
//
// The write is a compare-and-swap on the revision that was read. The bucket is written by the
// upstream sync, so a record that changed in between is never overwritten with stale bytes; it is
// counted as a conflict, and the newer write already went through the consumer.
//
// Only records the consumer has already processed are replayed (they have a vote_response
// mapping), so the handler treats them as updates and the LFID invite path, which fires only on
// a first create, does not run.
func ReplayVoteResponses(
	ctx context.Context,
	js jetstream.JetStream,
	opts ReplayVoteResponsesOptions,
	logger *slog.Logger,
) (*ReplayVoteResponsesResult, error) {
	if opts.RatePerSecond <= 0 {
		opts.RatePerSecond = DefaultReindexRatePerSecond
	}

	objectsKV, err := js.KeyValue(ctx, V1ObjectsBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", V1ObjectsBucket, err)
	}
	mappingsKV, err := js.KeyValue(ctx, V1MappingsBucket)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", V1MappingsBucket, err)
	}

	result := &ReplayVoteResponsesResult{}
	entries, err := collectReplayEntries(ctx, objectsKV, mappingsKV, opts, result, logger)
	if err != nil {
		return result, err
	}
	result.Eligible = len(entries)
	logger.InfoContext(ctx, "vote response replay collected records", "eligible", result.Eligible, "apply", opts.Apply)
	if !opts.Apply {
		return result, nil
	}

	ticker := time.NewTicker(time.Second / time.Duration(opts.RatePerSecond))
	defer ticker.Stop()

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}

		switch err := replayRecord(ctx, objectsKV, entry); {
		case err == nil:
			result.Replayed++
		case errors.Is(err, jetstream.ErrKeyExists):
			result.Conflicts++
			logger.InfoContext(ctx, "record changed since it was read, not overwriting", "key", entry.Key())
		default:
			result.Failed++
			logger.ErrorContext(ctx, "failed to re-put record", "key", entry.Key(), "err", err)
		}
	}

	if result.Failed > 0 {
		return result, fmt.Errorf("%d of %d records failed to re-put", result.Failed, result.Eligible)
	}
	return result, nil
}

// replayRecord re-puts an entry's own bytes, guarded by the revision it was read at.
func replayRecord(ctx context.Context, kv jetstream.KeyValue, entry jetstream.KeyValueEntry) error {
	_, err := kv.Update(ctx, entry.Key(), entry.Value(), entry.Revision())
	return err
}

func collectReplayEntries(
	ctx context.Context,
	objectsKV, mappingsKV jetstream.KeyValue,
	opts ReplayVoteResponsesOptions,
	result *ReplayVoteResponsesResult,
	logger *slog.Logger,
) ([]jetstream.KeyValueEntry, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	scrollID, page, err := openScroll(ctx, client, opts.OpenSearchURL)
	if err != nil {
		return nil, fmt.Errorf("open OpenSearch scroll: %w", err)
	}
	defer func() {
		if err := deleteScroll(ctx, client, opts.OpenSearchURL, scrollID); err != nil {
			logger.WarnContext(ctx, "failed to delete OpenSearch scroll context", "err", err)
		}
	}()

	seen := map[string]bool{}
	parentOK := map[string]bool{}
	var entries []jetstream.KeyValueEntry

	for len(page) > 0 {
		for _, hit := range page {
			result.Scanned++
			id := hit.Source.ObjectID
			if id == "" {
				result.SkippedNoID++
				continue
			}
			if seen[id] {
				result.Duplicates++
				continue
			}
			seen[id] = true

			// Filter on the indexed vote so a single-vote run does not read every record.
			if opts.VoteUID != "" && hit.Source.voteUID() != opts.VoteUID {
				result.SkippedOther++
				continue
			}

			key := voteResponseKeyPrefix + id
			entry, err := objectsKV.Get(ctx, key)
			if err != nil {
				if errors.Is(err, jetstream.ErrKeyNotFound) {
					result.NotFound++
				} else {
					logger.WarnContext(ctx, "skipping unreadable vote response record", "key", key, "err", err)
				}
				continue
			}

			rec, err := decodeVoteResponseRecord(entry.Value())
			if err != nil {
				logger.WarnContext(ctx, "skipping undecodable vote response record", "key", key, "err", err)
				continue
			}
			if rec.softDeleted {
				result.SkippedDeleted++
				continue
			}

			respMapping, err := mappingsKV.Get(ctx, voteResponseMappingKeyPrefix+id)
			if err != nil {
				result.SkippedNoMap++
				continue
			}
			if isTombstonedMapping(respMapping.Value()) {
				result.SkippedTomb++
				continue
			}

			ok, cached := parentOK[rec.pollID]
			if !cached {
				mapping, err := mappingsKV.Get(ctx, voteMappingKeyPrefix+rec.pollID)
				ok = rec.pollID != "" && err == nil && !isTombstonedMapping(mapping.Value())
				parentOK[rec.pollID] = ok
			}
			if !ok {
				result.SkippedNoParent++
				continue
			}

			entries = append(entries, entry)
			result.Keys = append(result.Keys, key)
		}

		logger.InfoContext(ctx, "scanned indexed vote responses", "scanned", result.Scanned, "eligible", len(entries))

		page, scrollID, err = nextScrollPage(ctx, client, opts.OpenSearchURL, scrollID)
		if err != nil {
			return nil, fmt.Errorf("OpenSearch scroll page: %w", err)
		}
	}
	return entries, nil
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
	Hits     struct {
		Hits []openSearchHit `json:"hits"`
	} `json:"hits"`
}

// openScroll opens a scroll over every indexed vote_response document.
func openScroll(ctx context.Context, client *http.Client, osURL string) (string, []openSearchHit, error) {
	body, err := json.Marshal(map[string]any{
		"query":   map[string]any{"term": map[string]any{"object_type": "vote_response"}},
		"_source": []string{"object_id", "data.vote_uid", "data.poll_id"},
		"size":    openSearchPageSize,
	})
	if err != nil {
		return "", nil, err
	}
	return doScroll(ctx, client, http.MethodPost,
		fmt.Sprintf("%s/%s/_search?scroll=%s", strings.TrimRight(osURL, "/"), openSearchIndex, openSearchScroll), body)
}

// nextScrollPage fetches the next page; an empty page means the scroll is exhausted.
func nextScrollPage(ctx context.Context, client *http.Client, osURL, scrollID string) ([]openSearchHit, string, error) {
	body, err := json.Marshal(map[string]string{"scroll": openSearchScroll, "scroll_id": scrollID})
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
	return resp.Body.Close()
}

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
	return out.ScrollID, out.Hits.Hits, nil
}

// voteResponseRecord is the subset of an itx-poll-vote record the reindex and the soft-delete path read.
type voteResponseRecord struct {
	pollID      string
	username    string
	softDeleted bool
}

// decodeVoteResponseRecord decodes a KV value (JSON or msgpack) with the same string/bool
// coercion as the live handler, since Meltano sends some fields as strings.
func decodeVoteResponseRecord(value []byte) (voteResponseRecord, error) {
	v1Data, err := decodeKVEntryData(value)
	if err != nil {
		return voteResponseRecord{}, err
	}
	deletedAt, hasDeleted := v1Data["_sdc_deleted_at"]

	raw, err := json.Marshal(v1Data)
	if err != nil {
		return voteResponseRecord{}, err
	}
	var rec domain.VoteDBRaw
	if err := json.Unmarshal(raw, &rec); err != nil {
		return voteResponseRecord{}, err
	}
	return voteResponseRecord{
		pollID:      rec.PollID,
		username:    rec.Username,
		softDeleted: hasDeleted && deletedAt != nil && deletedAt != "",
	}, nil
}
