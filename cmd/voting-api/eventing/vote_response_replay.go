// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	voteResponseKeyPrefix        = "itx-poll-vote."
	voteMappingKeyPrefix         = "vote."
	voteResponseMappingKeyPrefix = "vote_response."

	// DefaultReindexRatePerSecond keeps a full reindex from outrunning the consumer; each record
	// fans out to the indexer and to fga-sync, which serializes mutations per object.
	DefaultReindexRatePerSecond = 50
)

// ReplayVoteResponsesOptions configures ReplayVoteResponses.
type ReplayVoteResponsesOptions struct {
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
	Scanned         int // itx-poll-vote records read
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

// ReplayVoteResponses reindexes vote responses by re-putting each `itx-poll-vote.*` record in
// the v1-objects bucket with its existing bytes. The put lands as a new revision on the
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
	keys, err := objectsKV.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s keys: %w", V1ObjectsBucket, err)
	}

	parentOK := map[string]bool{}
	var entries []jetstream.KeyValueEntry

	for key := range keys.Keys() {
		if !strings.HasPrefix(key, voteResponseKeyPrefix) {
			continue
		}
		entry, err := objectsKV.Get(ctx, key)
		if err != nil {
			logger.WarnContext(ctx, "skipping unreadable vote response record", "key", key, "err", err)
			continue
		}
		result.Scanned++

		rec, err := decodeVoteResponseRecord(entry.Value())
		if err != nil {
			logger.WarnContext(ctx, "skipping undecodable vote response record", "key", key, "err", err)
			continue
		}

		switch {
		case rec.softDeleted:
			result.SkippedDeleted++
			continue
		case opts.VoteUID != "" && rec.pollID != opts.VoteUID:
			result.SkippedOther++
			continue
		}

		respMapping, err := mappingsKV.Get(ctx, voteResponseMappingKeyPrefix+strings.TrimPrefix(key, voteResponseKeyPrefix))
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
	return entries, nil
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
