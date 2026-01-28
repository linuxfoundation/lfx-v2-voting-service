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

// VoteService implements the vote service business logic
type VoteService struct {
	jwtAuth     domain.Authenticator
	proxyClient domain.PollClient
	idMapper    domain.IDMapper
	logger      *slog.Logger
}

// NewVoteService creates a new vote service
func NewVoteService(
	jwtAuth domain.Authenticator,
	proxyClient domain.PollClient,
	idMapper domain.IDMapper,
	logger *slog.Logger,
) *VoteService {
	return &VoteService{
		jwtAuth:     jwtAuth,
		proxyClient: proxyClient,
		idMapper:    idMapper,
		logger:      logger,
	}
}

// JWTAuth implements the authorization logic for JWT tokens
func (s *VoteService) JWTAuth(ctx context.Context, token string, scheme *security.JWTScheme) (context.Context, error) {
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
func (s *VoteService) CreateVote(ctx context.Context, req *CreateVoteRequest) (*itx.PollResponse, error) {
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

	// Map v2 project UID to v1 project SFID
	projectSFID, err := s.idMapper.MapProjectV2ToV1(ctx, req.ProjectUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map project UID to SFID", "error", err, "project_uid", req.ProjectUID)
		return nil, err
	}
	s.logger.InfoContext(ctx, "Mapped project ID", "v2_uid", req.ProjectUID, "v1_sfid", projectSFID)

	// Map v2 committee UID to v1 committee identifier
	var committeeSFID string
	if req.CommitteeUID != "" {
		committeeMapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, req.CommitteeUID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", req.CommitteeUID)
			return nil, err
		}
		// committeeMapping format is "project_sfid:committee_sfid", extract committee_sfid
		committeeSFID = committeeMapping
		s.logger.InfoContext(ctx, "Mapped committee ID", "v2_uid", req.CommitteeUID, "v1_mapping", committeeSFID)
	}

	// Map committee UIDs array
	var committeeIDs []string
	if len(req.CommitteeUIDs) > 0 {
		committeeIDs = make([]string, len(req.CommitteeUIDs))
		for i, uid := range req.CommitteeUIDs {
			mapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, uid)
			if err != nil {
				s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", uid)
				return nil, err
			}
			committeeIDs[i] = mapping
		}
		s.logger.InfoContext(ctx, "Mapped committee IDs array", "count", len(committeeIDs))
	}

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.CreatePollRequest{
		Name:                        req.Name,
		Description:                 req.Description,
		EndTime:                     req.EndTime,
		ProjectID:                   projectSFID,
		CommitteeID:                 committeeSFID,
		CommitteeIDs:                committeeIDs,
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

	// Map response fields from v1 to v2
	if err := s.mapPollResponseV1ToV2(ctx, proxyResp); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote created successfully",
		"poll_id", proxyResp.PollID,
		"status", proxyResp.Status,
	)

	return proxyResp, nil
}

// GetVote retrieves vote details (proxies to ITX GET /voting/poll/{poll_id})
func (s *VoteService) GetVote(ctx context.Context, voteID string) (*itx.PollResponse, error) {
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

	// Map response fields from v1 to v2
	if err := s.mapPollResponseV1ToV2(ctx, pollResp); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote retrieved successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// UpdateVote updates a vote (proxies to ITX PUT /voting/poll/{poll_id})
func (s *VoteService) UpdateVote(ctx context.Context, voteID string, req *UpdateVoteRequest) (*itx.PollResponse, error) {
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

	// Map v2 project UID to v1 project SFID
	projectSFID, err := s.idMapper.MapProjectV2ToV1(ctx, req.ProjectUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map project UID to SFID", "error", err, "project_uid", req.ProjectUID)
		return nil, err
	}

	// Map v2 committee UID to v1 committee identifier
	var committeeSFID string
	if req.CommitteeUID != "" {
		committeeMapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, req.CommitteeUID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", req.CommitteeUID)
			return nil, err
		}
		committeeSFID = committeeMapping
	}

	// Map committee UIDs array
	var committeeIDs []string
	if len(req.CommitteeUIDs) > 0 {
		committeeIDs = make([]string, len(req.CommitteeUIDs))
		for i, uid := range req.CommitteeUIDs {
			mapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, uid)
			if err != nil {
				s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", uid)
				return nil, err
			}
			committeeIDs[i] = mapping
		}
	}

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.UpdatePollRequest{
		Name:                        req.Name,
		Description:                 req.Description,
		EndTime:                     req.EndTime,
		ProjectID:                   projectSFID,
		CommitteeID:                 committeeSFID,
		CommitteeIDs:                committeeIDs,
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

	// Map response fields from v1 to v2
	if err := s.mapPollResponseV1ToV2(ctx, pollResp); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote updated successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// DeleteVote deletes a vote (proxies to ITX DELETE /voting/poll/{poll_id})
func (s *VoteService) DeleteVote(ctx context.Context, voteID string) error {
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

// ExtendVote extends a vote's end time (proxies to ITX POST /voting/poll/{poll_id}/extend)
func (s *VoteService) ExtendVote(ctx context.Context, voteID string, endTime string) (*itx.PollResponse, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Extending vote", "principal", principal, "vote_id", voteID, "end_time", endTime)

	// Build proxy request
	proxyReq := &itx.ExtendPollRequest{
		EndTime: endTime,
	}

	// Call ITX proxy
	pollResp, err := s.proxyClient.ExtendPoll(ctx, voteID, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to extend poll in ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	// Map response fields from v1 to v2
	if err := s.mapPollResponseV1ToV2(ctx, pollResp); err != nil {
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote extended successfully", "poll_id", pollResp.PollID, "end_time", pollResp.EndTime)

	return pollResp, nil
}

// EnableVote enables a vote for voting (proxies to ITX PUT /voting/poll/{poll_id}/enable)
func (s *VoteService) EnableVote(ctx context.Context, voteID string) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Enabling vote", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err := s.proxyClient.EnablePoll(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to enable poll in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote enabled successfully", "poll_id", voteID)

	return nil
}

// BulkResendVote bulk resends vote emails to select recipients (proxies to ITX POST /voting/poll/{poll_id}/bulk_resend)
func (s *VoteService) BulkResendVote(ctx context.Context, voteID string, recipientIDs []string) error {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Bulk resending vote emails",
		"principal", principal,
		"vote_id", voteID,
		"recipient_count", len(recipientIDs),
	)

	// Build proxy request
	proxyReq := &itx.BulkResendRequest{
		RecipientIDs: recipientIDs,
	}

	// Call ITX proxy
	err := s.proxyClient.BulkResendPoll(ctx, voteID, proxyReq)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to bulk resend poll emails in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote emails bulk resent successfully",
		"poll_id", voteID,
		"recipient_count", len(recipientIDs),
	)

	return nil
}

// GetVoteResults retrieves aggregated poll results (proxies to ITX GET /voting/poll/{poll_id}/results)
func (s *VoteService) GetVoteResults(ctx context.Context, voteID string) (*itx.VoteResults, error) {
	// Extract principal from context
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, domain.NewValidationError("authentication required")
	}

	s.logger.InfoContext(ctx, "Getting vote results", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	results, err := s.proxyClient.GetPollResults(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get poll results from ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote results retrieved successfully", "poll_id", voteID)

	return results, nil
}

// mapPollResponseV1ToV2 maps a PollResponse from ITX (v1 IDs) to v2 UIDs
func (s *VoteService) mapPollResponseV1ToV2(ctx context.Context, resp *itx.PollResponse) error {
	// Map v1 project SFID to v2 project UID
	if resp.ProjectID != "" {
		projectUID, err := s.idMapper.MapProjectV1ToV2(ctx, resp.ProjectID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map project SFID to UID - returning empty project ID", "error", err, "v1_project_id", resp.ProjectID)
			resp.ProjectID = ""
		} else {
			resp.ProjectID = projectUID
			s.logger.DebugContext(ctx, "Mapped project ID in response", "v1_sfid", resp.ProjectID, "v2_uid", projectUID)
		}
	}

	// Map v1 committee SFID to v2 committee UID
	if resp.CommitteeID != "" {
		committeeUID, err := s.idMapper.MapCommitteeV1ToV2(ctx, resp.CommitteeID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map committee SFID to UID - returning empty committee ID", "error", err, "v1_committee_id", resp.CommitteeID)
			resp.CommitteeID = ""
		} else {
			resp.CommitteeID = committeeUID
			s.logger.DebugContext(ctx, "Mapped committee ID in response", "v1_sfid", resp.CommitteeID, "v2_uid", committeeUID)
		}
	}

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
