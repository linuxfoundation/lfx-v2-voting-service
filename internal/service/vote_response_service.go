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
	idMapper    domain.IDMapper
	logger      *slog.Logger
	linkSigner  domain.VoteLinkSigner
	pollReader  domain.PollClient
}

// VoteResponseServiceOption configures optional VoteResponseService behavior.
type VoteResponseServiceOption func(*VoteResponseService)

// WithVoteLinkVerifier enables the personalized vote link endpoints, verifying their
// tokens with signer. Without it those endpoints return 503.
func WithVoteLinkVerifier(signer domain.VoteLinkSigner) VoteResponseServiceOption {
	return func(s *VoteResponseService) {
		s.linkSigner = signer
	}
}

// WithVoteLinkPolls lets the personalized vote link endpoints read the vote itself (questions,
// choices, status) from ITX. Without it, GetBallotByLink returns 503.
func WithVoteLinkPolls(polls domain.PollClient) VoteResponseServiceOption {
	return func(s *VoteResponseService) {
		s.pollReader = polls
	}
}

// VoteLinkBallot is what a personalized vote link holder may see: the vote and their own
// ballot. IncludeBallotContents is false on pseudo-anonymous votes, where the ballot's
// answers, comments and abstention must not be returned.
type VoteLinkBallot struct {
	Poll                  *itx.PollResponse
	Ballot                *itx.VoteResponse
	IncludeBallotContents bool
}

// NewVoteResponseService creates a new vote response service
func NewVoteResponseService(
	jwtAuth domain.Authenticator,
	proxyClient domain.VoteResponseClient,
	idMapper domain.IDMapper,
	logger *slog.Logger,
	opts ...VoteResponseServiceOption,
) *VoteResponseService {
	s := &VoteResponseService{
		jwtAuth:     jwtAuth,
		proxyClient: proxyClient,
		idMapper:    idMapper,
		logger:      logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
	}

	s.logger.InfoContext(ctx, "Creating vote response",
		"principal", principal,
		"vote_id", req.VoteID,
		"abstain", req.Abstain,
	)

	return s.submitVoteResponse(ctx, req)
}

// CreateVoteResponseByLink submits a vote response authorized by a personalized vote link
// token instead of the caller's identity. The ballot is taken from the verified token;
// ITX rejects the submission if the vote has ended, the voter was removed, or the ballot
// was already submitted.
func (s *VoteResponseService) CreateVoteResponseByLink(ctx context.Context, token string, req *CreateVoteResponseRequest) error {
	claims, err := s.verifyVoteLink(ctx, token)
	if err != nil {
		return err
	}

	// abstain is not logged: next to the ballot ID it would reveal the voter's choice,
	// including on pseudo-anonymous votes.
	s.logger.InfoContext(ctx, "Creating vote response by link",
		"vote_uid", claims.VoteUID,
		"vote_id", claims.VoteResponseUID,
		"key_id", claims.KeyID,
	)

	req.VoteID = claims.VoteResponseUID
	return s.submitVoteResponse(ctx, req)
}

// submitVoteResponse sends a new ballot to ITX for req.VoteID.
func (s *VoteResponseService) submitVoteResponse(ctx context.Context, req *CreateVoteResponseRequest) error {
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

	// Comment responses are allowed alongside abstain — an abstaining voter
	// skips the ballot but may still comment. Omitting the field (empty slice)
	// is fine for create: there are no existing comments to preserve on a new
	// ballot, so ITX accepts the absent field identically to [].
	proxyReq.CommentResponses = make([]itx.CommentResponse, len(req.CommentResponses))
	for i, c := range req.CommentResponses {
		proxyReq.CommentResponses[i] = itx.CommentResponse{
			PromptID:    c.PromptID,
			CommentText: c.CommentText,
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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return nil, err
	}

	s.logger.InfoContext(ctx, "Getting vote response", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	voteResp, err := s.proxyClient.GetVote(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get vote response from ITX", "error", err)
		return nil, err // Return domain error as-is
	}

	// Map response fields from v1 to v2
	if err := s.mapVoteResponseV1ToV2(ctx, voteResp); err != nil {
		s.logger.ErrorContext(ctx, "Failed to map vote response IDs", "error", err)
		return nil, err
	}

	s.logger.InfoContext(ctx, "Vote response retrieved successfully", "vote_id", voteResp.VoteID)

	return voteResp, nil
}

// UpdateVoteResponse updates a vote response (proxies to ITX PUT /voting/vote/{vote_id})
func (s *VoteResponseService) UpdateVoteResponse(ctx context.Context, voteID string, req *UpdateVoteResponseRequest) error {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
	}

	s.logger.InfoContext(ctx, "Updating vote response",
		"principal", principal,
		"vote_id", voteID,
		"abstain", req.Abstain,
	)

	return s.applyVoteResponseUpdate(ctx, voteID, req)
}

// UpdateVoteResponseByLink updates a vote response authorized by a personalized vote link
// token instead of the caller's identity. The ballot is taken from the verified token;
// ITX rejects the update if the vote has ended, the voter was removed, or no ballot has
// been submitted yet.
func (s *VoteResponseService) UpdateVoteResponseByLink(ctx context.Context, token string, req *UpdateVoteResponseRequest) error {
	claims, err := s.verifyVoteLink(ctx, token)
	if err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "Updating vote response by link",
		"vote_uid", claims.VoteUID,
		"vote_id", claims.VoteResponseUID,
		"key_id", claims.KeyID,
	)

	return s.applyVoteResponseUpdate(ctx, claims.VoteResponseUID, req)
}

// GetBallotByLink returns the vote and the link holder's own ballot for a personalized vote
// link token. Both are identified only by the verified token: the ballot must belong to the
// token's vote, otherwise the link is treated as invalid.
func (s *VoteResponseService) GetBallotByLink(ctx context.Context, token string) (*VoteLinkBallot, error) {
	claims, err := s.verifyVoteLink(ctx, token)
	if err != nil {
		return nil, err
	}
	if s.pollReader == nil {
		s.logger.WarnContext(ctx, "Vote link ballot requested but no poll reader is configured")
		return nil, domain.NewUnavailableError("personalized vote links are not configured")
	}

	s.logger.InfoContext(ctx, "Getting ballot by link",
		"vote_uid", claims.VoteUID,
		"vote_id", claims.VoteResponseUID,
		"key_id", claims.KeyID,
	)

	ballot, err := s.proxyClient.GetVote(ctx, claims.VoteResponseUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get ballot from ITX", "error", err)
		return nil, err
	}
	if ballot.PollID != claims.VoteUID {
		s.logger.WarnContext(ctx, "Vote link ballot does not belong to the token's vote",
			"vote_uid", claims.VoteUID, "ballot_poll_id", ballot.PollID)
		return nil, domain.NewNotFoundError("invalid voting link")
	}

	poll, err := s.pollReader.GetPoll(ctx, claims.VoteUID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get vote from ITX", "error", err)
		return nil, err
	}

	return &VoteLinkBallot{
		Poll:                  poll,
		Ballot:                ballot,
		IncludeBallotContents: !poll.PseudoAnonymity,
	}, nil
}

// verifyVoteLink checks a personalized vote link token. Every invalid token yields the
// same 404 so callers cannot learn which check failed or whether a ballot exists.
func (s *VoteResponseService) verifyVoteLink(ctx context.Context, token string) (*domain.VoteLinkClaims, error) {
	if s.linkSigner == nil {
		s.logger.WarnContext(ctx, "Vote link request received but personalized vote links are not configured")
		return nil, domain.NewUnavailableError("personalized vote links are not configured")
	}
	claims, err := s.linkSigner.Verify(token)
	if err != nil {
		s.logger.WarnContext(ctx, "Rejected invalid vote link token")
		return nil, domain.NewNotFoundError("invalid voting link")
	}
	return claims, nil
}

// applyVoteResponseUpdate sends an updated ballot to ITX for voteID.
func (s *VoteResponseService) applyVoteResponseUpdate(ctx context.Context, voteID string, req *UpdateVoteResponseRequest) error {
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

	// Comment semantics (matching ITX exactly): a nil CommentResponses preserves
	// existing comments untouched; a non-nil value (including an empty slice)
	// validates and replaces them. Abstain does not override either branch —
	// an abstaining voter skips the ballot but may still comment.
	if req.CommentResponses != nil {
		converted := make([]itx.CommentResponse, len(*req.CommentResponses))
		for i, c := range *req.CommentResponses {
			converted[i] = itx.CommentResponse{
				PromptID:    c.PromptID,
				CommentText: c.CommentText,
			}
		}
		proxyReq.CommentResponses = &converted
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
	principal, err := requirePrincipal(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Principal not found in context")
		return err
	}

	s.logger.InfoContext(ctx, "Resending vote email", "principal", principal, "vote_id", voteID)

	// Call ITX proxy
	err = s.proxyClient.ResendVote(ctx, voteID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to resend vote email in ITX", "error", err)
		return err // Return domain error as-is
	}

	s.logger.InfoContext(ctx, "Vote email resent successfully", "vote_id", voteID)

	return nil
}

// CreateVoteResponseRequest is the internal request type for creating a vote response
type CreateVoteResponseRequest struct {
	VoteID           string
	UserVoteContent  []VoteAnswerRequest
	Abstain          bool
	CommentResponses []CommentResponseRequest
}

// UpdateVoteResponseRequest is the internal request type for updating a vote response.
// CommentResponses is a pointer to slice: nil means the field was omitted (preserve
// existing comments); a non-nil pointer (even to an empty slice) means the field was
// included (validate and replace existing comments).
type UpdateVoteResponseRequest struct {
	UserVoteContent  []VoteAnswerRequest
	Abstain          bool
	CommentResponses *[]CommentResponseRequest
}

// CommentResponseRequest represents a voter's free-text response to a comment prompt
type CommentResponseRequest struct {
	PromptID    string
	CommentText string
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

// mapVoteResponseV1ToV2 maps a VoteResponse from ITX (v1 IDs) to v2 UIDs
func (s *VoteResponseService) mapVoteResponseV1ToV2(ctx context.Context, resp *itx.VoteResponse) error {
	// Map v1 project SFID to v2 project UID
	if resp.ProjectID != "" {
		v1ProjectID := resp.ProjectID
		projectUID, err := s.idMapper.MapProjectV1ToV2(ctx, resp.ProjectID)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map project SFID to UID", "error", err, "v1_project_id", v1ProjectID)
			return err
		}
		resp.ProjectID = projectUID
		s.logger.DebugContext(ctx, "Mapped project ID in vote response", "v1_sfid", v1ProjectID, "v2_uid", projectUID)
	}

	return nil
}
