// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"goa.design/goa/v3/security"
)

// maxVoteNameLen is the maximum allowed Unicode character count for a vote name.
// ITX rejects names over 200 characters with an opaque 400; we validate early so
// callers receive a clear error message. Both CreateVote and UpdateVote enforce this.
const maxVoteNameLen = 200

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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
	}

	s.logger.InfoContext(ctx, "Creating vote",
		"principal", principal,
		"name", req.Name,
		"project_uid", req.ProjectUID,
		"committee_uid", req.CommitteeUID,
	)

	// Validate name length — ITX rejects names over 200 Unicode characters but returns an
	// opaque 400. We validate here so the client receives a clear, actionable error message.
	// utf8.RuneCountInString is used (not len) so multi-byte characters count as one character.
	if utf8.RuneCountInString(req.Name) > maxVoteNameLen {
		return nil, domain.NewValidationError("vote name must not exceed 200 characters")
	}

	// Map IDs from v2 (UIDs) to v1 (SFIDs)
	projectSFID, committeeSFID, committeeIDs, err := s.mapRequestIDsV2ToV1(ctx, req.ProjectUID, req.CommitteeUID, req.CommitteeUIDs)
	if err != nil {
		return nil, err
	}

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.CreatePollRequest{
		Name:                       req.Name,
		Description:                req.Description,
		EndTime:                    req.EndTime,
		ProjectID:                  projectSFID,
		CommitteeID:                committeeSFID,
		CommitteeIDs:               committeeIDs,
		CommitteeFilters:           req.CommitteeFilters,
		PseudoAnonymity:            req.PseudoAnonymity,
		PollType:                   req.PollType,
		NumWinners:                 req.NumWinners,
		AllowAbstain:               req.AllowAbstain,
		QuorumPercentage:           req.QuorumPercentage,
		WinningThresholdPercentage: req.WinningThresholdPercentage,
	}

	// Forward the authenticated user's LFID username so ITX can enrich the creator
	// record from LFX and notify them when the poll ends. ITX keys enrichment on
	// username/email, so the principal must be sent as the username, not a bare ID.
	proxyReq.CreatedBy = creatorFromPrincipal(principal)

	// Mark the origin so ITX links the poll-ended email to the self-serve app: the proxy
	// is the sole self-serve entry point, so every poll it creates is self-serve.
	proxyReq.Source = itx.PollSourceSelfServe

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
		s.logger.ErrorContext(ctx, "Failed to map poll response IDs", "error", err)
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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
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
		s.logger.ErrorContext(ctx, "Failed to map poll response IDs", "error", err)
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote retrieved successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// UpdateVote updates a vote (proxies to ITX PUT /voting/poll/{poll_id})
func (s *VoteService) UpdateVote(ctx context.Context, voteID string, req *UpdateVoteRequest) (*itx.PollResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
	}

	s.logger.InfoContext(ctx, "Updating vote",
		"principal", principal,
		"vote_id", voteID,
		"name", req.Name,
	)

	// Validate name length — same 200-character limit as CreateVote.
	if req.Name != "" && utf8.RuneCountInString(req.Name) > maxVoteNameLen {
		return nil, domain.NewValidationError("vote name must not exceed 200 characters")
	}

	// Map IDs from v2 (UIDs) to v1 (SFIDs)
	projectID, committeeID, committeeIDs, err := s.mapRequestIDsV2ToV1(ctx, req.ProjectUID, req.CommitteeUID, req.CommitteeUIDs)
	if err != nil {
		return nil, err
	}

	// Build proxy request - map from UID (LFXv2) to ID (ITX)
	proxyReq := &itx.UpdatePollRequest{
		Name:                       req.Name,
		Description:                req.Description,
		EndTime:                    req.EndTime,
		ProjectID:                  projectID,
		CommitteeID:                committeeID,
		CommitteeIDs:               committeeIDs,
		CommitteeFilters:           req.CommitteeFilters,
		PseudoAnonymity:            req.PseudoAnonymity,
		PollType:                   req.PollType,
		NumWinners:                 req.NumWinners,
		AllowAbstain:               req.AllowAbstain,
		QuorumPercentage:           req.QuorumPercentage,
		WinningThresholdPercentage: req.WinningThresholdPercentage,
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
		s.logger.ErrorContext(ctx, "Failed to map poll response IDs", "error", err)
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote updated successfully", "poll_id", pollResp.PollID)

	return pollResp, nil
}

// DeleteVote deletes a vote (proxies to ITX DELETE /voting/poll/{poll_id})
func (s *VoteService) DeleteVote(ctx context.Context, voteID string) error {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
	}

	s.logger.InfoContext(ctx, "Deleting vote", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err = s.proxyClient.DeletePoll(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete poll from ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote deleted successfully", "poll_id", voteID)

	return nil
}

// ExtendVote extends a vote's end time (proxies to ITX POST /voting/poll/{poll_id}/extend)
func (s *VoteService) ExtendVote(ctx context.Context, voteID string, endTime string) (*itx.PollResponse, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
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
		s.logger.ErrorContext(ctx, "Failed to map poll response IDs", "error", err)
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote extended successfully", "poll_id", pollResp.PollID, "end_time", pollResp.EndTime)

	return pollResp, nil
}

// EnableVote enables a vote for voting (proxies to ITX PUT /voting/poll/{poll_id}/enable)
func (s *VoteService) EnableVote(ctx context.Context, voteID string) error {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
	}

	s.logger.InfoContext(ctx, "Enabling vote", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err = s.proxyClient.EnablePoll(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to enable poll in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote enabled successfully", "poll_id", voteID)

	return nil
}

// BulkResendVote bulk resends vote emails to select recipients (proxies to ITX POST /voting/poll/{poll_id}/bulk_resend)
func (s *VoteService) BulkResendVote(ctx context.Context, voteID string, recipientIDs []string) error {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
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
	err = s.proxyClient.BulkResendPoll(ctx, voteID, proxyReq)
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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
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
		v1ProjectID := resp.ProjectID
		projectUID, err := s.idMapper.MapProjectV1ToV2(ctx, resp.ProjectID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map project SFID to UID", "error", err, "v1_sfid", v1ProjectID)
			return err
		}
		resp.ProjectID = projectUID
		s.logger.DebugContext(ctx, "Mapped project ID in response", "v1_sfid", v1ProjectID, "v2_uid", projectUID)
	}

	// Map v1 committee SFID to v2 committee UID
	if resp.CommitteeID != "" {
		v1CommitteeID := resp.CommitteeID
		committeeUID, err := s.idMapper.MapCommitteeV1ToV2(ctx, resp.CommitteeID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map committee SFID to UID", "error", err, "v1_committee_id", v1CommitteeID)
			return err
		}
		resp.CommitteeID = committeeUID
		s.logger.DebugContext(ctx, "Mapped committee ID in response", "v1_sfid", v1CommitteeID, "v2_uid", committeeUID)
	}

	return nil
}

// mapRequestIDsV2ToV1 maps project and committee IDs from v2 (UIDs) to v1 (SFIDs)
// Returns projectSFID, committeeSFID, committeeIDs, error
func (s *VoteService) mapRequestIDsV2ToV1(ctx context.Context, projectUID, committeeUID string, committeeUIDs []string) (string, string, []string, error) {
	// Map v2 project UID to v1 project SFID
	projectID, err := s.idMapper.MapProjectV2ToV1(ctx, projectUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map project UID to SFID", "error", err, "project_uid", projectUID)
		return "", "", nil, err
	}
	s.logger.InfoContext(ctx, "Mapped project ID", "v2_uid", projectUID, "v1_sfid", projectID)

	// Map v2 committee UID to v1 committee identifier
	var committeeID string
	if committeeUID != "" {
		committeeMapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, committeeUID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", committeeUID)
			return "", "", nil, err
		}
		committeeID = committeeMapping
		s.logger.InfoContext(ctx, "Mapped committee ID", "v2_uid", committeeUID, "v1_mapping", committeeID)
	}

	// Map committee UIDs array
	var committeeIDs []string
	if len(committeeUIDs) > 0 {
		committeeIDs = make([]string, len(committeeUIDs))
		for i, uid := range committeeUIDs {
			mapping, err := s.idMapper.MapCommitteeV2ToV1(ctx, uid)
			if err != nil {
				s.logger.ErrorContext(ctx, "Failed to map committee UID to SFID", "error", err, "committee_uid", uid)
				return "", "", nil, err
			}
			committeeIDs[i] = mapping
		}
		s.logger.InfoContext(ctx, "Mapped committee IDs array", "count", len(committeeIDs))
	}

	return projectID, committeeID, committeeIDs, nil
}

// requirePrincipal extracts the principal string from a context and returns a
// validation error if it is absent or empty. Centralises the auth guard that
// every service method must perform before calling ITX.
func requirePrincipal(ctx context.Context) (string, error) {
	principal, ok := ctx.Value(constants.PrincipalContextID).(string)
	if !ok || strings.TrimSpace(principal) == "" {
		return "", domain.NewValidationError("authentication required")
	}
	return principal, nil
}

// creatorFromPrincipal builds the ITX created_by payload from the JWT principal claim.
// The LFXv2 principal is the LFID username, so it is sent as the username field; ITX
// resolves the creator's email and display name from LFX for the poll-ended notification.
// Returns nil when no principal is present so no empty created_by is forwarded.
func creatorFromPrincipal(principal string) *itx.CreatedByInput {
	username := strings.TrimSpace(principal)
	if username == "" {
		return nil
	}
	return &itx.CreatedByInput{Username: username}
}

// CreateVoteRequest is the internal request type for creating a vote
type CreateVoteRequest struct {
	Name                       string
	Description                string
	EndTime                    string
	ProjectUID                 string
	CommitteeUID               string
	CommitteeUIDs              []string
	CommitteeFilters           []string
	PollQuestions              []PollQuestionRequest
	PollCommentPrompts         []PollCommentPromptRequest
	PseudoAnonymity            bool
	PollType                   string
	NumWinners                 *int
	AllowAbstain               bool
	QuorumPercentage           *int
	WinningThresholdPercentage *int
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
	Name                       string
	Description                string
	EndTime                    string
	ProjectUID                 string
	CommitteeUID               string
	CommitteeUIDs              []string
	CommitteeFilters           []string
	PollQuestions              []PollQuestionRequest
	PollCommentPrompts         []PollCommentPromptRequest
	PseudoAnonymity            bool
	PollType                   string
	NumWinners                 *int
	AllowAbstain               bool
	QuorumPercentage           *int
	WinningThresholdPercentage *int
}
