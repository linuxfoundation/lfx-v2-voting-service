// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"goa.design/goa/v3/security"
)

// VoteResponseService implements vote response business logic
type VoteResponseService struct {
	jwtAuth     domain.Authenticator
	proxyClient domain.VoteResponseClient
	logger      *slog.Logger
}

// NewVoteResponseService creates a new vote response service
func NewVoteResponseService(
	jwtAuth domain.Authenticator,
	proxyClient domain.VoteResponseClient,
	logger *slog.Logger,
) *VoteResponseService {
	return &VoteResponseService{
		jwtAuth:     jwtAuth,
		proxyClient: proxyClient,
		logger:      logger,
	}
}

// JWTAuth implements the authorization logic for JWT tokens
func (s *VoteResponseService) JWTAuth(ctx context.Context, token string, scheme *security.JWTScheme) (context.Context, error) {
	principal, err := s.jwtAuth.ParsePrincipal(ctx, token, s.logger)
	if err != nil {
		s.logger.WarnContext(ctx, "JWT validation failed", "error", err)
		return ctx, domain.NewValidationError("invalid or expired token", err)
	}

	// Store principal in context
	ctx = context.WithValue(ctx, constants.PrincipalContextID, principal)
	s.logger.InfoContext(ctx, "JWT validated", "principal", principal)

	return ctx, nil
}

// CreateVoteResponse submits a vote response (proxies to ITX POST /voting/vote)
func (s *VoteResponseService) CreateVoteResponse(ctx context.Context, req *CreateVoteResponseRequest) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Creating vote response",
		"principal", principal,
		"vote_id", req.VoteID,
		"abstain", req.Abstain,
	)

	// Build proxy request
	proxyReq := &itx.CreateVoteRequest{
		VoteID:          req.VoteID,
		UserVoteContent: make([]itx.PollAnswerInput, len(req.UserVoteContent)),
		Abstain:         req.Abstain,
	}

	// Convert vote answers
	for i, answer := range req.UserVoteContent {
		rankedChoices := make([]itx.RankedChoiceInput, len(answer.RankedChoices))
		for j, rc := range answer.RankedChoices {
			rankedChoices[j] = itx.RankedChoiceInput{
				ChoiceID:   rc.ChoiceID,
				ChoiceRank: rc.ChoiceRank,
			}
		}
		proxyReq.UserVoteContent[i] = itx.PollAnswerInput{
			QuestionID:    answer.QuestionID,
			ChoiceIDs:     answer.ChoiceIDs,
			RankedChoices: rankedChoices,
		}
	}

	// Call ITX proxy
	err := s.proxyClient.CreateVote(ctx, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create vote response in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote response created successfully", "vote_id", req.VoteID)

	return nil
}

// GetVoteResponse retrieves vote response details (proxies to ITX GET /voting/vote/{vote_id})
func (s *VoteResponseService) GetVoteResponse(ctx context.Context, voteID string) (*itx.VoteResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Getting vote response", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	voteResp, err := s.proxyClient.GetVote(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get vote response from ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote response retrieved successfully", "vote_id", voteResp.VoteID)

	return voteResp, nil
}

// UpdateVoteResponse updates a vote response (proxies to ITX PUT /voting/vote/{vote_id})
func (s *VoteResponseService) UpdateVoteResponse(ctx context.Context, voteID string, req *UpdateVoteResponseRequest) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Updating vote response",
		"principal", principal,
		"vote_id", voteID,
		"abstain", req.Abstain,
	)

	// Build proxy request
	proxyReq := &itx.UpdateVoteRequest{
		UserVoteContent: make([]itx.PollAnswerInput, len(req.UserVoteContent)),
		Abstain:         req.Abstain,
	}

	// Convert vote answers
	for i, answer := range req.UserVoteContent {
		rankedChoices := make([]itx.RankedChoiceInput, len(answer.RankedChoices))
		for j, rc := range answer.RankedChoices {
			rankedChoices[j] = itx.RankedChoiceInput{
				ChoiceID:   rc.ChoiceID,
				ChoiceRank: rc.ChoiceRank,
			}
		}
		proxyReq.UserVoteContent[i] = itx.PollAnswerInput{
			QuestionID:    answer.QuestionID,
			ChoiceIDs:     answer.ChoiceIDs,
			RankedChoices: rankedChoices,
		}
	}

	// Call ITX proxy
	err := s.proxyClient.UpdateVote(ctx, voteID, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update vote response in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote response updated successfully", "vote_id", voteID)

	return nil
}

// ResendVoteResponse resends the vote email (proxies to ITX POST /voting/vote/{vote_id}/resend)
func (s *VoteResponseService) ResendVoteResponse(ctx context.Context, voteID string) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Resending vote email", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err := s.proxyClient.ResendVote(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to resend vote email in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote email resent successfully", "vote_id", voteID)

	return nil
}

// CreateVoteResponseRequest is the internal request type for creating a vote response
type CreateVoteResponseRequest struct {
	VoteID          string
	UserVoteContent []VoteAnswerRequest
	Abstain         bool
}

// UpdateVoteResponseRequest is the internal request type for updating a vote response
type UpdateVoteResponseRequest struct {
	UserVoteContent []VoteAnswerRequest
	Abstain         bool
}

// VoteAnswerRequest represents a vote answer
type VoteAnswerRequest struct {
	QuestionID    string
	ChoiceIDs     []string
	RankedChoices []RankedChoiceRequest
}

// RankedChoiceRequest represents a ranked choice
type RankedChoiceRequest struct {
	ChoiceID   string
	ChoiceRank int
}
