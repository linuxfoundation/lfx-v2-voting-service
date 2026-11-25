// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package eventsync syncs v1 voting records (polls, ballots and poll results) into v2.
//
// It is a deep module: callers hand it one raw KV entry through Syncer.Sync and get back
// an Outcome. Payload decoding, soft-delete detection, key routing, ID mapping,
// parent-before-child ordering, create-vs-update detection, publishing and tombstoning
// all sit behind that interface. The external seams are the domain interfaces it is
// constructed with.
package eventsync

import (
	"context"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

const errKey = "error"

// Outcome tells the caller what to do with the message that carried a KV entry.
type Outcome int

const (
	// Ack means the entry is finished: processed, or permanently skipped. Do not redeliver.
	Ack Outcome = iota
	// Retry means the entry could not be processed yet, for example because its parent
	// has not been synced or a dependency is temporarily unavailable. Redeliver it later.
	Retry
)

// Operation is the kind of change a KV entry represents.
type Operation int

const (
	// OpUnknown is any operation the module does not handle; such entries are acknowledged.
	OpUnknown Operation = iota
	// OpPut is a create or update of a KV key. A put whose record is soft-deleted is
	// processed as a delete.
	OpPut
	// OpDelete is a hard delete or purge of a KV key.
	OpDelete
)

// Entry is a raw entry from the v1-objects KV bucket.
type Entry struct {
	// Key is the KV key without the bucket prefix, e.g. "itx-poll.<uid>".
	Key string
	// Op is the change the entry represents.
	Op Operation
	// Value is the raw JSON or msgpack record. It is empty for hard deletes and purges.
	Value []byte
}

// PollPrivacyReader reports whether a poll's vote responses must be indexed without
// ballot data.
type PollPrivacyReader interface {
	// IsPseudoAnonymous reports whether the parent poll is pseudo-anonymous. Implementations
	// fail closed (return true) when the poll cannot be read or decoded, and return an
	// error only for transient read failures, which the module turns into a retry.
	IsPseudoAnonymous(ctx context.Context, pollID string) (bool, error)
}

// VoteResponseInviter sends a best-effort LFID invite for a new vote-response participant.
// Implementations log and swallow their own errors.
type VoteResponseInviter interface {
	MaybeInvite(ctx context.Context, logger *slog.Logger, voteResponseUID, email, displayName, pollID string)
}

// Deps are the seams the Syncer depends on.
type Deps struct {
	// Publisher publishes indexer and FGA events.
	Publisher domain.EventPublisher
	// IDMapper maps v1 IDs to v2 UIDs.
	IDMapper domain.IDMapper
	// Mappings records which resources were already synced.
	Mappings domain.MappingStore
	// Polls reads parent poll privacy. If nil, every poll is treated as pseudo-anonymous.
	Polls PollPrivacyReader
	// Inviter sends LFID invites for new vote responses. Optional.
	Inviter VoteResponseInviter
	// Logger is the base logger.
	Logger *slog.Logger
}

// Syncer syncs v1 voting records into v2.
type Syncer struct {
	deps     Deps
	handlers map[string]resourceHandler
}

// New creates a Syncer.
func New(deps Deps) *Syncer {
	s := &Syncer{deps: deps}
	s.handlers = map[string]resourceHandler{
		prefixVote:         voteResource(),
		prefixVoteResponse: voteResponseResource(),
		prefixVoteResult:   voteResultResource(),
	}
	return s
}

// Sync processes one KV entry and reports whether the message should be acknowledged
// or redelivered.
func (s *Syncer) Sync(ctx context.Context, e Entry) Outcome {
	switch e.Op {
	case OpPut:
		return s.put(ctx, e)
	case OpDelete:
		s.deps.Logger.With("key", e.Key, "operation", "delete").InfoContext(ctx, "processing hard delete from KV bucket")
		return s.delete(ctx, e)
	default:
		s.deps.Logger.With("key", e.Key).DebugContext(ctx, "ignoring unknown KV operation")
		return Ack
	}
}

// put routes a put entry to its resource handler, or to the delete path when the
// record is soft-deleted.
func (s *Syncer) put(ctx context.Context, e Entry) Outcome {
	logger := s.deps.Logger

	v1Data, err := DecodeRecord(e.Value)
	if err != nil {
		logger.With(errKey, err, "key", e.Key).ErrorContext(ctx, "failed to unmarshal KV entry data as JSON or msgpack")
		return Ack
	}

	// A soft delete is a record that carries _sdc_deleted_at.
	if deletedAt, exists := v1Data["_sdc_deleted_at"]; exists && deletedAt != nil && deletedAt != "" {
		logger.With("key", e.Key, "_sdc_deleted_at", deletedAt).InfoContext(ctx, "processing soft delete from KV bucket")
		return s.delete(ctx, e)
	}

	prefix, _ := splitKey(e.Key)
	h, ok := s.handlers[prefix]
	if !ok {
		logger.With("key", e.Key, "prefix", prefix).DebugContext(ctx, "skipping update - unsupported type")
		return Ack
	}
	return h.update(ctx, s, e.Key, v1Data)
}

// delete routes a delete entry to its resource handler by key prefix.
func (s *Syncer) delete(ctx context.Context, e Entry) Outcome {
	logger := s.deps.Logger
	prefix, uid := splitKey(e.Key)
	if uid == "" {
		logger.With("key", e.Key).WarnContext(ctx, "cannot extract UID from key for deletion")
		return Ack
	}

	h, ok := s.handlers[prefix]
	if !ok {
		logger.With("key", e.Key, "prefix", prefix).DebugContext(ctx, "skipping delete - unsupported type")
		return Ack
	}
	return h.remove(ctx, s, uid, e.Value)
}

// splitKey splits "itx-poll.<uid>" into its prefix and UID. A key with no period is all prefix.
func splitKey(key string) (prefix, uid string) {
	prefix, uid, _ = strings.Cut(key, ".")
	return prefix, uid
}
