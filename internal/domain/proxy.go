// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// PollClient defines the interface for poll management operations in ITX
type PollClient interface {
	// CreatePoll creates a new poll in ITX (maps to our "create vote")
	CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error)

	// GetPoll retrieves poll details from ITX
	GetPoll(ctx context.Context, pollID string) (*itx.PollResponse, error)

	// UpdatePoll updates a poll in ITX (only when status is "disabled")
	UpdatePoll(ctx context.Context, pollID string, req *itx.UpdatePollRequest) (*itx.PollResponse, error)

	// DeletePoll deletes a poll in ITX (only when status is "disabled")
	DeletePoll(ctx context.Context, pollID string) error

	// ExtendPoll extends a poll's end time in ITX
	ExtendPoll(ctx context.Context, pollID string, req *itx.ExtendPollRequest) (*itx.PollResponse, error)

	// EnablePoll enables a poll for voting in ITX
	EnablePoll(ctx context.Context, pollID string) error

	// BulkResendPoll bulk resends poll emails to select recipients in ITX
	BulkResendPoll(ctx context.Context, pollID string, req *itx.BulkResendRequest) error

	// GetPollResults retrieves aggregated poll results from ITX
	GetPollResults(ctx context.Context, pollID string) (*itx.VoteResults, error)
}

// VoteResponseClient defines the interface for vote response operations in ITX
type VoteResponseClient interface {
	// CreateVote submits a vote response in ITX
	CreateVote(ctx context.Context, req *itx.CreateVoteRequest) error

	// GetVote retrieves vote response details from ITX
	GetVote(ctx context.Context, voteID string) (*itx.VoteResponse, error)

	// UpdateVote updates a vote response in ITX
	UpdateVote(ctx context.Context, voteID string, req *itx.UpdateVoteRequest) error

	// ResendVote resends the vote email in ITX
	ResendVote(ctx context.Context, voteID string) error
}

// ITXProxyClient combines both poll and vote response operations
type ITXProxyClient interface {
	PollClient
	VoteResponseClient
}
