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
		"project_id", payload.ProjectID,
		"committee_id", payload.CommitteeID,
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
		"poll_id", result.PollID,
		"status", result.Status,
	)

	return result, nil
}
