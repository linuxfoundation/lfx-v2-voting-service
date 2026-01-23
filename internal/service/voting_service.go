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

// VotingService implements the voting service business logic
type VotingService struct {
	jwtAuth     domain.Authenticator
	proxyClient domain.ITXProxyClient
	logger      *slog.Logger
}

// NewVotingService creates a new voting service
func NewVotingService(
	jwtAuth domain.Authenticator,
	proxyClient domain.ITXProxyClient,
	logger *slog.Logger,
) *VotingService {
	return &VotingService{
		jwtAuth:     jwtAuth,
		proxyClient: proxyClient,
		logger:      logger,
	}
}

// JWTAuth implements the authorization logic for JWT tokens
func (s *VotingService) JWTAuth(ctx context.Context, token string, scheme *security.JWTScheme) (context.Context, error) {
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

// CreateVote creates a new vote (proxies to ITX POST /voting/poll)
func (s *VotingService) CreateVote(ctx context.Context, req *CreateVoteRequest) (*itx.PollResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Creating vote",
		"principal", principal,
		"name", req.Name,
		"project_uid", req.ProjectUID,
		"committee_uid", req.CommitteeUID,
	)

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.CreatePollRequest{
		Name:                        req.Name,
		Description:                 req.Description,
		EndTime:                     req.EndTime,
		ProjectID:                   req.ProjectUID,   // Map ProjectUID → ProjectID for ITX
		CommitteeID:                 req.CommitteeUID, // Map CommitteeUID → CommitteeID for ITX
		CommitteeIDs:                req.CommitteeUIDs, // Map CommitteeUIDs → CommitteeIDs for ITX
		CommitteeFilters:            req.CommitteeFilters,
		PseudoAnonymity:             req.PseudoAnonymity,
		PollType:                    req.PollType,
		NumWinners:                  req.NumWinners,
		AllowAbstain:                req.AllowAbstain,
		QuorumPercentage:            req.QuorumPercentage,
		WinningThresholdPercentage:  req.WinningThresholdPercentage,
	}

	// Convert poll questions
	proxyReq.PollQuestions = make([]itx.PollQuestionInput, len(req.PollQuestions))
	for i, q := range req.PollQuestions {
		choices := make([]string, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = c.ChoiceText
		}
		proxyReq.PollQuestions[i] = itx.PollQuestionInput{
			Prompt:  q.Prompt,
			Type:    q.Type,
			Choices: choices,
		}
	}

	// Convert poll comment prompts
	proxyReq.PollCommentPrompts = make([]itx.PollCommentPrompt, len(req.PollCommentPrompts))
	for i, p := range req.PollCommentPrompts {
		proxyReq.PollCommentPrompts[i] = itx.PollCommentPrompt{
			Prompt: p.Prompt,
		}
	}

	// Call ITX proxy
	proxyResp, err := s.proxyClient.CreatePoll(ctx, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create poll in ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote created successfully",
		"poll_id", proxyResp.PollID,
		"status", proxyResp.Status,
	)

	return proxyResp, nil
}

// GetVote retrieves vote details (proxies to ITX GET /voting/poll/{poll_id})
func (s *VotingService) GetVote(ctx context.Context, voteID string) (*itx.PollResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Getting vote", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	pollResp, err := s.proxyClient.GetPoll(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get poll from ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote retrieved successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// UpdateVote updates a vote (proxies to ITX PUT /voting/poll/{poll_id})
func (s *VotingService) UpdateVote(ctx context.Context, voteID string, req *UpdateVoteRequest) (*itx.PollResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Updating vote",
		"principal", principal,
		"vote_id", voteID,
		"name", req.Name,
	)

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.UpdatePollRequest{
		Name:                        req.Name,
		Description:                 req.Description,
		EndTime:                     req.EndTime,
		ProjectID:                   req.ProjectUID,   // Map ProjectUID → ProjectID for ITX
		CommitteeID:                 req.CommitteeUID, // Map CommitteeUID → CommitteeID for ITX
		CommitteeIDs:                req.CommitteeUIDs, // Map CommitteeUIDs → CommitteeIDs for ITX
		CommitteeFilters:            req.CommitteeFilters,
		PseudoAnonymity:             req.PseudoAnonymity,
		PollType:                    req.PollType,
		NumWinners:                  req.NumWinners,
		AllowAbstain:                req.AllowAbstain,
		QuorumPercentage:            req.QuorumPercentage,
		WinningThresholdPercentage:  req.WinningThresholdPercentage,
	}

	// Convert poll questions
	proxyReq.PollQuestions = make([]itx.PollQuestionInput, len(req.PollQuestions))
	for i, q := range req.PollQuestions {
		choices := make([]string, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = c.ChoiceText
		}
		proxyReq.PollQuestions[i] = itx.PollQuestionInput{
			Prompt:  q.Prompt,
			Type:    q.Type,
			Choices: choices,
		}
	}

	// Convert poll comment prompts
	proxyReq.PollCommentPrompts = make([]itx.PollCommentPrompt, len(req.PollCommentPrompts))
	for i, p := range req.PollCommentPrompts {
		proxyReq.PollCommentPrompts[i] = itx.PollCommentPrompt{
			Prompt: p.Prompt,
		}
	}

	// Call ITX proxy
	pollResp, err := s.proxyClient.UpdatePoll(ctx, voteID, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update poll in ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote updated successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// DeleteVote deletes a vote (proxies to ITX DELETE /voting/poll/{poll_id})
func (s *VotingService) DeleteVote(ctx context.Context, voteID string) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Deleting vote", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err := s.proxyClient.DeletePoll(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete poll from ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote deleted successfully", "poll_id", voteID)

	return nil
}

// CreateVoteRequest is the internal request type for creating a vote
type CreateVoteRequest struct {
	Name                        string
	Description                 string
	EndTime                     string
	ProjectUID                  string
	CommitteeUID                string
	CommitteeUIDs               []string
	CommitteeFilters            []string
	PollQuestions               []PollQuestionRequest
	PollCommentPrompts          []PollCommentPromptRequest
	PseudoAnonymity             bool
	PollType                    string
	NumWinners                  *int
	AllowAbstain                bool
	QuorumPercentage            *int
	WinningThresholdPercentage  *int
}

// PollQuestionRequest represents a question in the request
type PollQuestionRequest struct {
	Prompt  string
	Type    string
	Choices []PollChoiceRequest
}

// PollChoiceRequest represents a choice in the request
type PollChoiceRequest struct {
	ChoiceText string
}

// PollCommentPromptRequest represents a comment prompt in the request
type PollCommentPromptRequest struct {
	Prompt string
}

// UpdateVoteRequest is the internal request type for updating a vote
type UpdateVoteRequest struct {
	Name                        string
	Description                 string
	EndTime                     string
	ProjectUID                  string
	CommitteeUID                string
	CommitteeUIDs               []string
	CommitteeFilters            []string
	PollQuestions               []PollQuestionRequest
	PollCommentPrompts          []PollCommentPromptRequest
	PseudoAnonymity             bool
	PollType                    string
	NumWinners                  *int
	AllowAbstain                bool
	QuorumPercentage            *int
	WinningThresholdPercentage  *int
}
