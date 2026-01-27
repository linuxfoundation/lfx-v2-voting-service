// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	apiservice "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
)

// CreateVoteResponse submits a vote response (proxies to ITX POST /voting/vote)
func (s *VotingAPI) CreateVoteResponse(ctx context.Context, payload *votesvc.CreateVoteResponsePayload) error {
	logger := slog.With("component", "voting_api", "method", "CreateVoteResponse")

	logger.InfoContext(ctx, "Create vote response request received",
		"vote_response_id", payload.VoteResponseID,
		"vote_uid", payload.VoteUID,
		"abstain", payload.Abstain,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertCreateVoteResponsePayloadToDomain(payload)

	// Call service layer
	err := s.voteResponseService.CreateVoteResponse(ctx, req)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to create vote response", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote response created successfully", "vote_response_id", payload.VoteResponseID)

	return nil
}

// GetVoteResponse retrieves vote response details (proxies to ITX GET /voting/vote/{vote_id})
func (s *VotingAPI) GetVoteResponse(ctx context.Context, payload *votesvc.GetVoteResponsePayload) (*votesvc.VoteResponseResult, error) {
	logger := slog.With("component", "voting_api", "method", "GetVoteResponse")

	logger.InfoContext(ctx, "Get vote response request received", "vote_response_id", payload.VoteResponseID)

	// Call service layer
	voteResp, err := s.voteResponseService.GetVoteResponse(ctx, payload.VoteResponseID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get vote response", "error", err)
		return nil, handleError(err)
	}

	// Convert domain response to Goa result
	result := apiservice.ConvertVoteResponseToResult(voteResp)

	logger.InfoContext(ctx, "Vote response retrieved successfully", "vote_response_id", result.VoteID)

	return result, nil
}

// UpdateVoteResponse updates a vote response (proxies to ITX PUT /voting/vote/{vote_id})
func (s *VotingAPI) UpdateVoteResponse(ctx context.Context, payload *votesvc.UpdateVoteResponsePayload) error {
	logger := slog.With("component", "voting_api", "method", "UpdateVoteResponse")

	logger.InfoContext(ctx, "Update vote response request received",
		"vote_response_id", payload.VoteResponseID,
		"abstain", payload.Abstain,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertUpdateVoteResponsePayloadToDomain(payload)

	// Call service layer
	err := s.voteResponseService.UpdateVoteResponse(ctx, payload.VoteResponseID, req)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to update vote response", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote response updated successfully", "vote_response_id", payload.VoteResponseID)

	return nil
}

// ResendVoteResponse resends the vote email (proxies to ITX POST /voting/vote/{vote_id}/resend)
func (s *VotingAPI) ResendVoteResponse(ctx context.Context, payload *votesvc.ResendVoteResponsePayload) error {
	logger := slog.With("component", "voting_api", "method", "ResendVoteResponse")

	logger.InfoContext(ctx, "Resend vote email request received", "vote_response_id", payload.VoteResponseID)

	// Call service layer
	err := s.voteResponseService.ResendVoteResponse(ctx, payload.VoteResponseID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to resend vote email", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote email resent successfully", "vote_response_id", payload.VoteResponseID)

	return nil
}
