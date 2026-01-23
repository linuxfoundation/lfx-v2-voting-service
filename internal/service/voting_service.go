// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
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
	ctx = context.WithValue(ctx, "principal", principal)
	s.logger.InfoContext(ctx, "JWT validated", "principal", principal)

	return ctx, nil
}

// CreateVote creates a new vote (proxies to ITX POST /voting/poll)
func (s *VotingService) CreateVote(ctx context.Context, req *CreateVoteRequest) (*domain.PollResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value("principal").(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Creating vote",
		"principal", principal,
		"name", req.Name,
		"project_id", req.ProjectID,
		"committee_id", req.CommitteeID,
	)

	// Build proxy request
	proxyReq := &domain.CreatePollRequest{
		Name:             req.Name,
		Description:      req.Description,
		EndTime:          req.EndTime,
		ProjectID:        req.ProjectID,
		CommitteeID:      req.CommitteeID,
		CommitteeFilters: req.CommitteeFilters,
		PseudoAnonymity:  req.PseudoAnonymity,
		PollType:         req.PollType,
		NumWinners:       req.NumWinners,
		AllowAbstain:     req.AllowAbstain,
	}

	// Convert poll questions
	proxyReq.PollQuestions = make([]domain.PollQuestionInput, len(req.PollQuestions))
	for i, q := range req.PollQuestions {
		choices := make([]string, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = c.ChoiceText
		}
		proxyReq.PollQuestions[i] = domain.PollQuestionInput{
			Prompt:  q.Prompt,
			Type:    q.Type,
			Choices: choices,
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

// CreateVoteRequest is the internal request type for creating a vote
type CreateVoteRequest struct {
	Name             string
	Description      string
	EndTime          string
	ProjectID        string
	CommitteeID      string
	CommitteeFilters []string
	PollQuestions    []PollQuestionRequest
	PseudoAnonymity  bool
	PollType         string
	NumWinners       *int
	AllowAbstain     bool
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
