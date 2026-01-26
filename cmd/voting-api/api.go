// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	votingsvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/voting"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	apiservice "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
	"goa.design/goa/v3/security"
)

// VotingAPI implements the votingsvc.Service interface
type VotingAPI struct {
	votingService *service.VotingService
}

// NewVotingAPI creates a new VotingAPI
func NewVotingAPI(votingService *service.VotingService) *VotingAPI {
	return &VotingAPI{
		votingService: votingService,
	}
}

// createResponse creates a response error based on the HTTP status code
func createResponse(code int, err error) error {
	switch code {
	case http.StatusBadRequest:
		return &votingsvc.BadRequestError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusUnauthorized:
		return &votingsvc.UnauthorizedError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusForbidden:
		return &votingsvc.ForbiddenError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusNotFound:
		return &votingsvc.NotFoundError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusConflict:
		return &votingsvc.ConflictError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusInternalServerError:
		return &votingsvc.InternalServerError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	case http.StatusServiceUnavailable:
		return &votingsvc.ServiceUnavailableError{
			Code:    strconv.Itoa(code),
			Message: err.Error(),
		}
	default:
		return nil
	}
}

// handleError converts domain errors to HTTP errors
// This function maps domain error types to appropriate HTTP responses
func handleError(err error) error {
	errorType := domain.GetErrorType(err)

	switch errorType {
	case domain.ErrorTypeValidation:
		return createResponse(http.StatusBadRequest, err)
	case domain.ErrorTypeNotFound:
		return createResponse(http.StatusNotFound, err)
	case domain.ErrorTypeConflict:
		return createResponse(http.StatusConflict, err)
	case domain.ErrorTypeUnavailable:
		return createResponse(http.StatusServiceUnavailable, err)
	case domain.ErrorTypeInternal:
		return createResponse(http.StatusInternalServerError, err)
	default:
		return createResponse(http.StatusInternalServerError, err)
	}
}

// JWTAuth implements Auther interface for the JWT security scheme
func (s *VotingAPI) JWTAuth(ctx context.Context, bearerToken string, scheme *security.JWTScheme) (context.Context, error) {
	// Delegate to voting service
	return s.votingService.JWTAuth(ctx, bearerToken, scheme)
}

// CreateVote creates a new vote (proxies to ITX POST /voting/poll)
func (s *VotingAPI) CreateVote(ctx context.Context, payload *votingsvc.CreateVotePayload) (*votingsvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "CreateVote")

	logger.InfoContext(ctx, "Create vote request received",
		"name", payload.Name,
		"project_uid", payload.ProjectUID,
		"committee_uid", payload.CommitteeUID,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertCreateVotePayloadToDomain(payload)

	// Call service layer
	pollResp, err := s.votingService.CreateVote(ctx, req)
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
func (s *VotingAPI) GetVote(ctx context.Context, payload *votingsvc.GetVotePayload) (*votingsvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "GetVote")

	logger.InfoContext(ctx, "Get vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	pollResp, err := s.votingService.GetVote(ctx, payload.VoteUID)
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
func (s *VotingAPI) UpdateVote(ctx context.Context, payload *votingsvc.UpdateVotePayload) (*votingsvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "UpdateVote")

	logger.InfoContext(ctx, "Update vote request received",
		"vote_uid", payload.VoteUID,
		"name", payload.Name,
	)

	// Convert Goa payload to service request
	req := apiservice.ConvertUpdateVotePayloadToDomain(payload)

	// Call service layer
	pollResp, err := s.votingService.UpdateVote(ctx, payload.VoteUID, req)
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
func (s *VotingAPI) DeleteVote(ctx context.Context, payload *votingsvc.DeleteVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "DeleteVote")

	logger.InfoContext(ctx, "Delete vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	err := s.votingService.DeleteVote(ctx, payload.VoteUID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to delete vote", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote deleted successfully", "vote_uid", payload.VoteUID)

	return nil
}

// ExtendVote extends a vote's end time (proxies to ITX POST /voting/poll/{poll_id}/extend)
func (s *VotingAPI) ExtendVote(ctx context.Context, payload *votingsvc.ExtendVotePayload) (*votingsvc.VoteResult, error) {
	logger := slog.With("component", "voting_api", "method", "ExtendVote")

	logger.InfoContext(ctx, "Extend vote request received",
		"vote_uid", payload.VoteUID,
		"end_time", payload.EndTime,
	)

	// Call service layer
	pollResp, err := s.votingService.ExtendVote(ctx, payload.VoteUID, payload.EndTime)
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
func (s *VotingAPI) EnableVote(ctx context.Context, payload *votingsvc.EnableVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "EnableVote")

	logger.InfoContext(ctx, "Enable vote request received", "vote_uid", payload.VoteUID)

	// Call service layer
	err := s.votingService.EnableVote(ctx, payload.VoteUID)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to enable vote", "error", err)
		return handleError(err)
	}

	logger.InfoContext(ctx, "Vote enabled successfully", "vote_uid", payload.VoteUID)

	return nil
}

// BulkResendVote bulk resends vote emails to select recipients (proxies to ITX POST /voting/poll/{poll_id}/bulk_resend)
func (s *VotingAPI) BulkResendVote(ctx context.Context, payload *votingsvc.BulkResendVotePayload) error {
	logger := slog.With("component", "voting_api", "method", "BulkResendVote")

	logger.InfoContext(ctx, "Bulk resend vote request received",
		"vote_uid", payload.VoteUID,
		"recipient_count", len(payload.RecipientIds),
	)

	// Call service layer
	err := s.votingService.BulkResendVote(ctx, payload.VoteUID, payload.RecipientIds)
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
