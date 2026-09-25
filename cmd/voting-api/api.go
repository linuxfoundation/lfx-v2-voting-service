// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net/http"
	"strconv"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"goa.design/goa/v3/security"
)

// VotingAPI implements the vote service interface
type VotingAPI struct {
	voteService         *service.VoteService
	voteResponseService *service.VoteResponseService
}

// NewVotingAPI creates a new VotingAPI
func NewVotingAPI(
	voteService *service.VoteService,
	voteResponseService *service.VoteResponseService,
) *VotingAPI {
	return &VotingAPI{
		voteService:         voteService,
		voteResponseService: voteResponseService,
	}
}

// createVoteResponseError creates an error response for both vote and vote_responses services
// It tries both service error types and returns the first one that matches
func createVoteResponseError(code int, err error) error {
	codeStr := strconv.Itoa(code)
	msg := err.Error()

	switch code {
	case http.StatusBadRequest:
		// Both services have BadRequestError, try vote first
		return &votesvc.BadRequestError{Code: codeStr, Message: msg}
	case http.StatusUnauthorized:
		return &votesvc.UnauthorizedError{Code: codeStr, Message: msg}
	case http.StatusForbidden:
		return &votesvc.ForbiddenError{Code: codeStr, Message: msg}
	case http.StatusNotFound:
		return &votesvc.NotFoundError{Code: codeStr, Message: msg}
	case http.StatusConflict:
		// Only vote service has ConflictError
		return &votesvc.ConflictError{Code: codeStr, Message: msg}
	case http.StatusInternalServerError:
		return &votesvc.InternalServerError{Code: codeStr, Message: msg}
	case http.StatusServiceUnavailable:
		return &votesvc.ServiceUnavailableError{Code: codeStr, Message: msg}
	default:
		// Fallback to internal server error
		return &votesvc.InternalServerError{Code: strconv.Itoa(http.StatusInternalServerError), Message: msg}
	}
}

// handleError converts domain errors to HTTP errors
// This function maps domain error types to appropriate HTTP responses
func handleError(err error) error {
	errorType := domain.GetErrorType(err)

	var code int
	switch errorType {
	case domain.ErrorTypeValidation:
		code = http.StatusBadRequest
	case domain.ErrorTypeNotFound:
		code = http.StatusNotFound
	case domain.ErrorTypeConflict:
		code = http.StatusConflict
	case domain.ErrorTypeUnavailable:
		code = http.StatusServiceUnavailable
	case domain.ErrorTypeInternal:
		code = http.StatusInternalServerError
	default:
		code = http.StatusInternalServerError
	}

	return createVoteResponseError(code, err)
}

// JWTAuth implements Auther interface for the JWT security scheme for vote service
func (s *VotingAPI) JWTAuth(ctx context.Context, bearerToken string, scheme *security.JWTScheme) (context.Context, error) {
	// Both services use the same JWT auth, so we can delegate to either
	return s.voteService.JWTAuth(ctx, bearerToken, scheme)
}
