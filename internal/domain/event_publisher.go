// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// EventPublisher defines the interface for publishing events to NATS
type EventPublisher interface {
	// PublishVoteEvent publishes a vote (poll) event to indexer and FGA-sync
	PublishVoteEvent(ctx context.Context, action string, vote *VoteData) error

	// PublishVoteResponseEvent publishes a vote response event to indexer and FGA-sync
	PublishVoteResponseEvent(ctx context.Context, action string, voteResponse *VoteResponseData) error

	// PublishVoteResultEvent publishes a poll result snapshot event to the indexer.
	// No FGA message is emitted: access is derived from the parent vote's results_viewer relation.
	PublishVoteResultEvent(ctx context.Context, action string, pollResult *PollResultData) error

	// Close closes the publisher connection
	Close() error
}
