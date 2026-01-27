// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	apiservice "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
)

// CreateVote creates a new vote (proxies to ITX POST /voting/poll)
func (s *VotingAPI) CreateVote(ctx context.Context, payload *votesvc.CreateVotePayload) (*votesvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "CreateVote")

	logger.InfoContext(ctx, "Create vote request received",
		"name", payload.Name,
		"project_uid", payload.ProjectUID,
		"committee_uid", payload.CommitteeUID,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertCreateVotePayloadToDomain(payload)

	// Call service layer
	pollResp, err := s.voteService.CreateVote(ctx, req)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create vote", "error", err)
		return nil, handleError(err)
	}

	// Convert domain response to Goa result
	result := apiservice.ConvertPollResponseToVoteResult(pollResp)

	logger.InfoContext(ctx, "Vote created successfully",
		"vote_uid", result.VoteUID,
		"status", result.Status,
	)

	return result, nil
}

// GetVote retrieves vote details (proxies to ITX GET /voting/poll/{poll_id})
func (s *VotingAPI) GetVote(ctx context.Context, payload *votesvc.GetVotePayload) (*votesvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "GetVote")

	logger.InfoContext(ctx, "Get vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	pollResp, err := s.voteService.GetVote(ctx, payload.VoteUID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get vote", "error", err)
		return nil, handleError(err)
	}

	// Convert domain response to Goa result
	result := apiservice.ConvertPollResponseToVoteResult(pollResp)

	logger.InfoContext(ctx, "Vote retrieved successfully", "vote_uid", result.VoteUID)

	return result, nil
}

// UpdateVote updates a vote (proxies to ITX PUT /voting/poll/{poll_id})
func (s *VotingAPI) UpdateVote(ctx context.Context, payload *votesvc.UpdateVotePayload) (*votesvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "UpdateVote")

	logger.InfoContext(ctx, "Update vote request received",
		"vote_uid", payload.VoteUID,
		"name", payload.Name,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertUpdateVotePayloadToDomain(payload)

	// Call service layer
	pollResp, err := s.voteService.UpdateVote(ctx, payload.VoteUID, req)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to update vote", "error", err)
		return nil, handleError(err)
	}

	// Convert domain response to Goa result
	result := apiservice.ConvertPollResponseToVoteResult(pollResp)

	logger.InfoContext(ctx, "Vote updated successfully", "vote_uid", result.VoteUID)

	return result, nil
}

// DeleteVote deletes a vote (proxies to ITX DELETE /voting/poll/{poll_id})
func (s *VotingAPI) DeleteVote(ctx context.Context, payload *votesvc.DeleteVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "DeleteVote")

	logger.InfoContext(ctx, "Delete vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	err := s.voteService.DeleteVote(ctx, payload.VoteUID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to delete vote", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote deleted successfully", "vote_uid", payload.VoteUID)

	return nil
}

// ExtendVote extends a vote's end time (proxies to ITX POST /voting/poll/{poll_id}/extend)
func (s *VotingAPI) ExtendVote(ctx context.Context, payload *votesvc.ExtendVotePayload) (*votesvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "ExtendVote")

	logger.InfoContext(ctx, "Extend vote request received",
		"vote_uid", payload.VoteUID,
		"end_time", payload.EndTime,
	)

	// Call service layer
	pollResp, err := s.voteService.ExtendVote(ctx, payload.VoteUID, payload.EndTime)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to extend vote", "error", err)
		return nil, handleError(err)
	}

	// Convert domain response to Goa result
	result := apiservice.ConvertPollResponseToVoteResult(pollResp)

	logger.InfoContext(ctx, "Vote extended successfully",
		"vote_uid", result.VoteUID,
		"end_time", result.EndTime,
	)

	return result, nil
}

// EnableVote enables a vote for voting (proxies to ITX PUT /voting/poll/{poll_id}/enable)
func (s *VotingAPI) EnableVote(ctx context.Context, payload *votesvc.EnableVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "EnableVote")

	logger.InfoContext(ctx, "Enable vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	err := s.voteService.EnableVote(ctx, payload.VoteUID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to enable vote", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote enabled successfully", "vote_uid", payload.VoteUID)

	return nil
}

// BulkResendVote bulk resends vote emails to select recipients (proxies to ITX POST /voting/poll/{poll_id}/bulk_resend)
func (s *VotingAPI) BulkResendVote(ctx context.Context, payload *votesvc.BulkResendVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "BulkResendVote")

	logger.InfoContext(ctx, "Bulk resend vote request received",
		"vote_uid", payload.VoteUID,
		"recipient_count", len(payload.RecipientIds),
	)

	// Call service layer
	err := s.voteService.BulkResendVote(ctx, payload.VoteUID, payload.RecipientIds)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to bulk resend vote emails", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote emails bulk resent successfully",
		"vote_uid", payload.VoteUID,
		"recipient_count", len(payload.RecipientIds),
	)

	return nil
}
