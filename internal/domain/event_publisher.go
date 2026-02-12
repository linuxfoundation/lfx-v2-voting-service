// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import "context"

// EventPublisher defines the interface for publishing events to NATS
type EventPublisher interface {
	// PublishVoteEvent publishes a vote (poll) event to indexer and FGA-sync
	PublishVoteEvent(ctx context.Context, action string, vote interface{}) error

	// PublishVoteResponseEvent publishes a vote response event to indexer and FGA-sync
	PublishVoteResponseEvent(ctx context.Context, action string, voteResponse interface{}) error

	// Close closes the publisher connection
	Close() error
}
