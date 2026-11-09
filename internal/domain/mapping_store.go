// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// MappingKind identifies the kind of v1 resource a mapping record tracks.
type MappingKind string

const (
	// MappingKindVote tracks synced votes (v1 itx-poll records).
	MappingKindVote MappingKind = "vote"
	// MappingKindVoteResponse tracks synced vote responses (v1 itx-poll-vote records).
	MappingKindVoteResponse MappingKind = "vote_response"
	// MappingKindVoteResult tracks synced vote results (v1 itx-poll-results records).
	MappingKindVoteResult MappingKind = "vote_result"
)

// MappingState is what the sync state store knows about a single resource.
type MappingState int

const (
	// MappingAbsent means the resource has not been synced, or its state could not be read.
	MappingAbsent MappingState = iota
	// MappingSynced means the resource was synced and has not been deleted since.
	MappingSynced
	// MappingTombstoned means the resource was synced and then deleted.
	MappingTombstoned
)

// MappingStore records which v1 resources the sync has already processed, so that
// create-vs-update can be decided, child resources can wait for their parent, and
// redelivered deletes are skipped.
type MappingStore interface {
	// State reports what is known about the resource. A read failure is reported as
	// MappingAbsent, matching the previous behaviour of the sync handlers.
	State(ctx context.Context, kind MappingKind, uid string) MappingState

	// MarkSynced records that the resource was synced.
	MarkSynced(ctx context.Context, kind MappingKind, uid string) error

	// MarkTombstoned records that the resource was deleted, so a redelivered delete is skipped.
	MarkTombstoned(ctx context.Context, kind MappingKind, uid string) error
}
