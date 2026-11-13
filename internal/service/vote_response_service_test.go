// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// Tests for VoteResponseService — the service layer for ballot submissions.
//
// Patterns mirrored from vote_service_test.go: principal guard on every
// method + proxy error propagation + response ID mapping. VoteResponseService
// was previously 0% covered.

import (
	"context"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaauth "goa.design/goa/v3/security"
)

// ---- VoteResponseClient fakes -----------------------------------------------

// returnVoteResponseClient provides configurable responses for any client method.
type returnVoteResponseClient struct {
	domain.VoteResponseClient
	resp *itx.VoteResponse
	err  error
}

func (r *returnVoteResponseClient) CreateVote(_ context.Context, _ *itx.CreateVoteRequest) error {
	return r.err
}
func (r *returnVoteResponseClient) GetVote(_ context.Context, _ string) (*itx.VoteResponse, error) {
	return r.resp, r.err
}
func (r *returnVoteResponseClient) UpdateVote(_ context.Context, _ string, _ *itx.UpdateVoteRequest) error {
	return r.err
}
func (r *returnVoteResponseClient) ResendVote(_ context.Context, _ string) error {
	return r.err
}

func newResponseSvc(client domain.VoteResponseClient, mapper domain.IDMapper) *VoteResponseService {
	return NewVoteResponseService(nil, client, mapper, discardLogger())
}

// prefixIDMapper is a test mapper that prepends "uid:" to every v1→v2 mapping so
// tests can assert the exact transformed value rather than a pass-through echo.
// This distinguishes "mapper was called and its result was used" from "value was
// copied directly without going through the mapper".
type prefixIDMapper struct{}

func (prefixIDMapper) MapProjectV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, nil
}
func (prefixIDMapper) MapProjectV1ToV2(_ context.Context, v1SFID string) (string, error) {
	return "uid:" + v1SFID, nil
}
func (prefixIDMapper) MapCommitteeV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, nil
}
func (prefixIDMapper) MapCommitteeV1ToV2(_ context.Context, v1SFID string) (string, error) {
	return "uid:" + v1SFID, nil
}

// ---- CreateVoteResponse -----------------------------------------------------

func TestCreateVoteResponse_MissingPrincipal(t *testing.T) {
	svc := newResponseSvc(&returnVoteResponseClient{}, identityIDMapper{})

	err := svc.CreateVoteResponse(context.Background(), &CreateVoteResponseRequest{VoteID: "v1"})
	assertValidationError(t, err)
}

func TestCreateVoteResponse_PropagatesProxyError(t *testing.T) {
	proxyErr := domain.NewConflictError("ballot already submitted")
	svc := newResponseSvc(&returnVoteResponseClient{err: proxyErr}, identityIDMapper{})

	err := svc.CreateVoteResponse(ctxWithPrincipal("user"), &CreateVoteResponseRequest{VoteID: "v1"})
	if domain.GetErrorType(err) != domain.ErrorTypeConflict {
		t.Errorf("want ErrorTypeConflict, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// ---- GetVoteResponse --------------------------------------------------------

func TestGetVoteResponse_MissingPrincipal(t *testing.T) {
	svc := newResponseSvc(&returnVoteResponseClient{}, identityIDMapper{})

	_, err := svc.GetVoteResponse(context.Background(), "vote-1")
	assertValidationError(t, err)
}

func TestGetVoteResponse_PropagatesNotFound(t *testing.T) {
	svc := newResponseSvc(
		&returnVoteResponseClient{err: domain.NewNotFoundError("vote response not found")},
		identityIDMapper{},
	)

	_, err := svc.GetVoteResponse(ctxWithPrincipal("user"), "missing-vote")
	if domain.GetErrorType(err) != domain.ErrorTypeNotFound {
		t.Errorf("want ErrorTypeNotFound, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// TestGetVoteResponse_ResponseIDMappingPopulatesProjectUID mirrors the equivalent
// VoteService test: verifies the response mapping path is actually invoked so a
// caller does not receive a raw v1 SFID in the ProjectID field.
//
// prefixIDMapper returns "uid:" + input for v1→v2 mapping. If the mapping call is
// skipped and resp.ProjectID is copied directly, the assertion fails because the
// raw SFID "proj-sfid" != the transformed value "uid:proj-sfid".
func TestGetVoteResponse_ResponseIDMappingPopulatesProjectUID(t *testing.T) {
	svc := newResponseSvc(
		&returnVoteResponseClient{resp: &itx.VoteResponse{
			VoteID:    "vote-1",
			ProjectID: "proj-sfid",
		}},
		prefixIDMapper{},
	)

	result, err := svc.GetVoteResponse(ctxWithPrincipal("user"), "vote-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Ballot.ProjectID != "uid:proj-sfid" {
		t.Errorf("expected ProjectID %q (mapper applied), got %q", "uid:proj-sfid", result.Ballot.ProjectID)
	}
}

// TestGetVoteResponse_ResponseIDMappingFailurePropagates verifies that a mapper
// error on the response path is returned rather than silently emitting a v1 SFID.
func TestGetVoteResponse_ResponseIDMappingFailurePropagates(t *testing.T) {
	svc := newResponseSvc(
		&returnVoteResponseClient{resp: &itx.VoteResponse{
			VoteID:    "vote-1",
			ProjectID: "sfid-xyz",
		}},
		&failingIDMapper{err: domain.NewInternalError("mapper unavailable")},
	)

	_, err := svc.GetVoteResponse(ctxWithPrincipal("user"), "vote-1")
	if err == nil {
		t.Fatal("expected error from failing ID mapper, got nil")
	}
	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
}

func TestWouldEndVote(t *testing.T) {
	pending := &itx.VoteResponse{VoteStatus: itx.VoteStatusAwaitingResponse}
	activePoll := func(received, total int) *itx.PollResponse {
		return &itx.PollResponse{Status: itx.PollStatusActive, NumResponseReceived: received, TotalVotingRequestInvitations: total}
	}
	cases := []struct {
		name   string
		poll   *itx.PollResponse
		ballot *itx.VoteResponse
		want   bool
	}{
		{name: "pending last voter", poll: activePoll(8, 9), ballot: pending, want: true},
		{name: "pending with stale over-count", poll: activePoll(9, 9), ballot: pending, want: true},
		{name: "pending with two outstanding", poll: activePoll(7, 9), ballot: pending},
		{name: "responded ballot", poll: activePoll(8, 9), ballot: &itx.VoteResponse{VoteStatus: itx.VoteStatusResponded}},
		{name: "ended ballot", poll: activePoll(8, 9), ballot: &itx.VoteResponse{VoteStatus: itx.VoteStatusEnded}},
		{name: "unsubmitted ballot on ended poll", poll: activePoll(8, 9), ballot: &itx.VoteResponse{VoteStatus: "awaiting_response_but_poll_ended"}},
		{name: "removed voter", poll: activePoll(8, 9), ballot: &itx.VoteResponse{VoteStatus: itx.VoteStatusAwaitingResponse, VoterRemoved: true}},
		{name: "ended poll", poll: &itx.PollResponse{Status: "ended", NumResponseReceived: 8, TotalVotingRequestInvitations: 9}, ballot: pending},
		{name: "disabled poll", poll: &itx.PollResponse{Status: "disabled", NumResponseReceived: 0, TotalVotingRequestInvitations: 1}, ballot: pending},
		{name: "no invitations", poll: activePoll(0, 0), ballot: pending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wouldEndVote(tc.poll, tc.ballot); got != tc.want {
				t.Errorf("wouldEndVote = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetVoteResponse_WouldEndVote(t *testing.T) {
	lastVoterPoll := &itx.PollResponse{PollID: "vote-1", Status: itx.PollStatusActive, NumResponseReceived: 8, TotalVotingRequestInvitations: 9}
	cases := []struct {
		name            string
		ballot          *itx.VoteResponse
		pollErr         error
		noReader        bool
		want            bool
		wantUnavailable bool
		wantPollID      string
	}{
		{
			name:       "pending last voter",
			ballot:     &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: itx.VoteStatusAwaitingResponse},
			want:       true,
			wantPollID: "vote-1",
		},
		{
			name:   "responded ballot skips the poll read",
			ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: itx.VoteStatusResponded},
		},
		{
			name:   "removed voter skips the poll read",
			ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: itx.VoteStatusAwaitingResponse, VoterRemoved: true},
		},
		{
			name:            "poll read fails",
			ballot:          &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: itx.VoteStatusAwaitingResponse},
			pollErr:         domain.NewUnavailableError("ITX down"),
			wantUnavailable: true,
			wantPollID:      "vote-1",
		},
		{
			name:            "no poll reader",
			ballot:          &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: itx.VoteStatusAwaitingResponse},
			noReader:        true,
			wantUnavailable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			polls := &ballotPollReader{poll: lastVoterPoll, err: tc.pollErr}
			var opts []VoteResponseServiceOption
			if !tc.noReader {
				opts = append(opts, WithPollReader(polls))
			}
			svc := NewVoteResponseService(nil, &ballotVoteClient{ballot: tc.ballot}, identityIDMapper{}, discardLogger(), opts...)

			got, err := svc.GetVoteResponse(ctxWithPrincipal("user"), "ballot-1")
			if polls.requested != tc.wantPollID {
				t.Errorf("poll requested = %q, want %q", polls.requested, tc.wantPollID)
			}
			if tc.wantUnavailable {
				assertErrorType(t, err, domain.ErrorTypeUnavailable)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.WouldEndVote != tc.want {
				t.Errorf("WouldEndVote = %v, want %v", got.WouldEndVote, tc.want)
			}
			if got.Ballot != tc.ballot {
				t.Error("Ballot must be the ballot read from ITX")
			}
		})
	}
}

// ---- UpdateVoteResponse -----------------------------------------------------

func TestUpdateVoteResponse_MissingPrincipal(t *testing.T) {
	svc := newResponseSvc(&returnVoteResponseClient{}, identityIDMapper{})

	err := svc.UpdateVoteResponse(context.Background(), "vote-1", &UpdateVoteResponseRequest{})
	assertValidationError(t, err)
}

func TestUpdateVoteResponse_PropagatesProxyError(t *testing.T) {
	svc := newResponseSvc(
		&returnVoteResponseClient{err: domain.NewNotFoundError("vote not found")},
		identityIDMapper{},
	)

	err := svc.UpdateVoteResponse(ctxWithPrincipal("user"), "vote-1", &UpdateVoteResponseRequest{})
	if domain.GetErrorType(err) != domain.ErrorTypeNotFound {
		t.Errorf("want ErrorTypeNotFound, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// TestUpdateVoteResponse_NilCommentResponsesPreservesExisting verifies the
// pointer-to-slice semantic: a nil CommentResponses in the request should cause
// the proxy call to be made without the comment_responses field, telling ITX to
// leave existing comments untouched.
func TestUpdateVoteResponse_NilCommentResponsesPreservesExisting(t *testing.T) {
	captured := &captureVoteResponseClient{}
	svc := newResponseSvc(captured, identityIDMapper{})

	err := svc.UpdateVoteResponse(ctxWithPrincipal("user"), "vote-1", &UpdateVoteResponseRequest{
		CommentResponses: nil, // omitted = preserve
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.lastUpdateReq == nil {
		t.Fatal("expected UpdateVote to be called")
	}
	if captured.lastUpdateReq.CommentResponses != nil {
		t.Errorf("expected nil CommentResponses forwarded to proxy (preserve semantics), got %+v",
			captured.lastUpdateReq.CommentResponses)
	}
}

// ---- ResendVoteResponse -----------------------------------------------------

func TestResendVoteResponse_MissingPrincipal(t *testing.T) {
	svc := newResponseSvc(&returnVoteResponseClient{}, identityIDMapper{})

	err := svc.ResendVoteResponse(context.Background(), "vote-1")
	assertValidationError(t, err)
}

func TestResendVoteResponse_PropagatesUnavailable(t *testing.T) {
	svc := newResponseSvc(
		&returnVoteResponseClient{err: domain.NewUnavailableError("ITX down")},
		identityIDMapper{},
	)

	err := svc.ResendVoteResponse(ctxWithPrincipal("user"), "vote-1")
	if domain.GetErrorType(err) != domain.ErrorTypeUnavailable {
		t.Errorf("want ErrorTypeUnavailable, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// ---- shared helpers ---------------------------------------------------------

// assertValidationError is a shared helper for the missing-principal check.
func assertValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if dErr.Type != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d", dErr.Type)
	}
}

// captureVoteResponseClient records the UpdateVote request for semantic assertions.
type captureVoteResponseClient struct {
	domain.VoteResponseClient
	lastUpdateReq *itx.UpdateVoteRequest
}

func (c *captureVoteResponseClient) UpdateVote(_ context.Context, _ string, req *itx.UpdateVoteRequest) error {
	c.lastUpdateReq = req
	return nil
}

func TestCreateVoteResponseByLink_UsesBallotFromToken(t *testing.T) {
	client := &captureLinkVoteClient{}
	svc := NewVoteResponseService(nil, client, identityIDMapper{}, discardLogger(), WithVoteLinkVerifier(fakeLinkSigner{}))

	req := &CreateVoteResponseRequest{
		VoteID:  "attacker-chosen-ballot",
		Abstain: true,
	}
	// No principal in context: link requests are authorized by the token alone.
	if err := svc.CreateVoteResponseByLink(context.Background(), "tok:vote-1:ballot-1", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.createReq.VoteID != "ballot-1" {
		t.Errorf("ITX vote_id = %q, want ballot-1 from the token", client.createReq.VoteID)
	}
	if !client.createReq.Abstain {
		t.Error("abstain not forwarded")
	}
}

func TestUpdateVoteResponseByLink_UsesBallotFromToken(t *testing.T) {
	client := &captureLinkVoteClient{}
	svc := NewVoteResponseService(nil, client, identityIDMapper{}, discardLogger(), WithVoteLinkVerifier(fakeLinkSigner{}))

	comments := []CommentResponseRequest{}
	req := &UpdateVoteResponseRequest{CommentResponses: &comments}
	if err := svc.UpdateVoteResponseByLink(context.Background(), "tok:vote-1:ballot-1", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.updateVoteID != "ballot-1" {
		t.Errorf("ITX vote_id = %q, want ballot-1", client.updateVoteID)
	}
	if client.updateReq.CommentResponses == nil || len(*client.updateReq.CommentResponses) != 0 {
		t.Error("an explicit empty comment_responses must be forwarded as [] (replace), not omitted")
	}
}

func TestVoteResponseByLink_Errors(t *testing.T) {
	itxErr := domain.NewValidationError("poll has ended")
	cases := []struct {
		name   string
		opts   []VoteResponseServiceOption
		token  string
		itxErr error
		want   domain.ErrorType
	}{
		{"not configured", nil, "tok:vote-1:ballot-1", nil, domain.ErrorTypeUnavailable},
		{"invalid token", []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{})}, "forged", nil, domain.ErrorTypeNotFound},
		{"ITX rejects", []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{})}, "tok:vote-1:ballot-1", itxErr, domain.ErrorTypeValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/create", func(t *testing.T) {
			client := &captureLinkVoteClient{err: tc.itxErr}
			svc := NewVoteResponseService(nil, client, identityIDMapper{}, discardLogger(), tc.opts...)
			err := svc.CreateVoteResponseByLink(context.Background(), tc.token, &CreateVoteResponseRequest{})
			assertErrorType(t, err, tc.want)
			if tc.itxErr == nil && client.createReq != nil {
				t.Error("ITX must not be called for a rejected token")
			}
		})
		t.Run(tc.name+"/update", func(t *testing.T) {
			client := &captureLinkVoteClient{err: tc.itxErr}
			svc := NewVoteResponseService(nil, client, identityIDMapper{}, discardLogger(), tc.opts...)
			err := svc.UpdateVoteResponseByLink(context.Background(), tc.token, &UpdateVoteResponseRequest{})
			assertErrorType(t, err, tc.want)
			if tc.itxErr == nil && client.updateReq != nil {
				t.Error("ITX must not be called for a rejected token")
			}
		})
	}
}

// ballotVoteClient returns a fixed ballot from GetVote and records the requested ID.
type ballotVoteClient struct {
	domain.VoteResponseClient
	ballot    *itx.VoteResponse
	err       error
	requested string
}

// ballotPollReader returns a fixed poll from GetPoll and records the requested ID.
type ballotPollReader struct {
	domain.PollClient
	poll      *itx.PollResponse
	err       error
	requested string
}

func TestGetBallotByLink_ReturnsVoteAndBallotFromToken(t *testing.T) {
	for _, pseudo := range []bool{false, true} {
		votes := &ballotVoteClient{ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: "responded"}}
		polls := &ballotPollReader{poll: &itx.PollResponse{PollID: "vote-1", ProjectID: "sfid-1", PseudoAnonymity: pseudo, PseudoAnonymityKnown: true}}
		svc := NewVoteResponseService(nil, votes, prefixProjectIDMapper{}, discardLogger(),
			WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(polls))

		got, err := svc.GetBallotByLink(context.Background(), "tok:vote-1:ballot-1")
		if err != nil {
			t.Fatalf("pseudo=%v: unexpected error: %v", pseudo, err)
		}
		if votes.requested != "ballot-1" || polls.requested != "vote-1" {
			t.Errorf("pseudo=%v: ITX called with ballot %q, poll %q; want ids from the token", pseudo, votes.requested, polls.requested)
		}
		if got.IncludeBallotContents == pseudo {
			t.Errorf("pseudo=%v: IncludeBallotContents = %v, want %v", pseudo, got.IncludeBallotContents, !pseudo)
		}
		if got.ProjectUID != "uid-sfid-1" {
			t.Errorf("pseudo=%v: ProjectUID = %q, want the UID mapped from the poll's project SFID", pseudo, got.ProjectUID)
		}
	}
}

func TestGetBallotByLink_Errors(t *testing.T) {
	okBallot := &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1"}
	okPoll := &itx.PollResponse{PollID: "vote-1"}
	cases := []struct {
		name     string
		opts     func(*ballotPollReader) []VoteResponseServiceOption
		token    string
		ballot   *itx.VoteResponse
		voteErr  error
		pollErr  error
		mapper   domain.IDMapper
		want     domain.ErrorType
		noUpcall bool
	}{
		{
			name:     "not configured",
			opts:     func(*ballotPollReader) []VoteResponseServiceOption { return nil },
			token:    "tok:vote-1:ballot-1",
			want:     domain.ErrorTypeUnavailable,
			noUpcall: true,
		},
		{
			name: "no poll reader",
			opts: func(*ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{})}
			},
			token:    "tok:vote-1:ballot-1",
			want:     domain.ErrorTypeUnavailable,
			noUpcall: true,
		},
		{
			name: "invalid token",
			opts: func(p *ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(p)}
			},
			token:    "forged",
			want:     domain.ErrorTypeNotFound,
			noUpcall: true,
		},
		{
			name: "ballot belongs to another vote",
			opts: func(p *ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(p)}
			},
			token:  "tok:vote-1:ballot-1",
			ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-2"},
			want:   domain.ErrorTypeNotFound,
		},
		{
			name: "ITX ballot not found",
			opts: func(p *ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(p)}
			},
			token:   "tok:vote-1:ballot-1",
			voteErr: domain.NewNotFoundError("vote not found"),
			want:    domain.ErrorTypeNotFound,
		},
		{
			name: "ITX poll unavailable",
			opts: func(p *ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(p)}
			},
			token:   "tok:vote-1:ballot-1",
			pollErr: domain.NewUnavailableError("ITX down"),
			want:    domain.ErrorTypeUnavailable,
		},
		{
			name: "project ID mapping fails",
			opts: func(p *ballotPollReader) []VoteResponseServiceOption {
				return []VoteResponseServiceOption{WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(p)}
			},
			token:  "tok:vote-1:ballot-1",
			mapper: &failingIDMapper{err: domain.NewUnavailableError("NATS down")},
			want:   domain.ErrorTypeUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ballot := tc.ballot
			if ballot == nil {
				ballot = okBallot
			}
			votes := &ballotVoteClient{ballot: ballot, err: tc.voteErr}
			polls := &ballotPollReader{poll: okPoll, err: tc.pollErr}
			var mapper domain.IDMapper = identityIDMapper{}
			if tc.mapper != nil {
				mapper = tc.mapper
			}
			svc := NewVoteResponseService(nil, votes, mapper, discardLogger(), tc.opts(polls)...)

			_, err := svc.GetBallotByLink(context.Background(), tc.token)
			assertErrorType(t, err, tc.want)
			if tc.noUpcall && (votes.requested != "" || polls.requested != "") {
				t.Error("ITX must not be called")
			}
		})
	}
}

func TestGetBallotByLink_UnknownPseudoAnonymityFailsClosed(t *testing.T) {
	votes := &ballotVoteClient{ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: "responded"}}
	polls := &ballotPollReader{poll: &itx.PollResponse{PollID: "vote-1", PseudoAnonymity: false, PseudoAnonymityKnown: false}}
	svc := NewVoteResponseService(nil, votes, identityIDMapper{}, discardLogger(),
		WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(polls))

	got, err := svc.GetBallotByLink(context.Background(), "tok:vote-1:ballot-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IncludeBallotContents {
		t.Error("ballot contents must not be returned when pseudo_anonymity is unknown")
	}
}

func TestGetBallotByLink_WouldEndVote(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{status: itx.VoteStatusAwaitingResponse, want: true},
		{status: itx.VoteStatusResponded, want: false},
	} {
		votes := &ballotVoteClient{ballot: &itx.VoteResponse{VoteID: "ballot-1", PollID: "vote-1", VoteStatus: tc.status}}
		polls := &ballotPollReader{poll: &itx.PollResponse{PollID: "vote-1", Status: itx.PollStatusActive, NumResponseReceived: 8, TotalVotingRequestInvitations: 9}}
		svc := NewVoteResponseService(nil, votes, identityIDMapper{}, discardLogger(),
			WithVoteLinkVerifier(fakeLinkSigner{}), WithPollReader(polls))

		got, err := svc.GetBallotByLink(context.Background(), "tok:vote-1:ballot-1")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.status, err)
		}
		if got.WouldEndVote != tc.want {
			t.Errorf("%s: WouldEndVote = %v, want %v", tc.status, got.WouldEndVote, tc.want)
		}
	}
}

// createCaptureClient records the CreateVote request and returns a configurable error.
type createCaptureClient struct {
	domain.VoteResponseClient
	last *itx.CreateVoteRequest
	err  error
}

func (c *createCaptureClient) CreateVote(_ context.Context, req *itx.CreateVoteRequest) error {
	c.last = req
	return c.err
}

func TestVoteResponseService_CreateVoteResponse_MapsBallotToITX(t *testing.T) {
	client := &createCaptureClient{}
	svc := newResponseSvc(client, identityIDMapper{})

	err := svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{
		VoteID: "vote-response-1",
		UserVoteContent: []VoteAnswerRequest{
			{QuestionID: "q1", ChoiceIDs: []string{"yes"}},
			{QuestionID: "q2", RankedChoices: []RankedChoiceRequest{{ChoiceID: "a", ChoiceRank: 1}, {ChoiceID: "b", ChoiceRank: 2}}},
		},
		Abstain: true,
		CommentResponses: []CommentResponseRequest{
			{PromptID: "p1", CommentText: "Looks good"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, client.last)
	assert.Equal(t, "vote-response-1", client.last.VoteID)
	assert.True(t, client.last.Abstain)

	require.Len(t, client.last.UserVoteContent, 2)
	assert.Equal(t, "q1", client.last.UserVoteContent[0].QuestionID)
	assert.Equal(t, []string{"yes"}, client.last.UserVoteContent[0].ChoiceIDs)
	assert.Empty(t, client.last.UserVoteContent[0].RankedChoices)
	assert.Equal(t, "q2", client.last.UserVoteContent[1].QuestionID)
	require.Len(t, client.last.UserVoteContent[1].RankedChoices, 2)
	assert.Equal(t, itx.RankedChoiceInput{ChoiceID: "a", ChoiceRank: 1}, client.last.UserVoteContent[1].RankedChoices[0])
	assert.Equal(t, itx.RankedChoiceInput{ChoiceID: "b", ChoiceRank: 2}, client.last.UserVoteContent[1].RankedChoices[1])

	require.Len(t, client.last.CommentResponses, 1)
	assert.Equal(t, itx.CommentResponse{PromptID: "p1", CommentText: "Looks good"}, client.last.CommentResponses[0])
}

func TestVoteResponseService_CreateVoteResponse_EmptyBallot(t *testing.T) {
	client := &createCaptureClient{}
	svc := newResponseSvc(client, identityIDMapper{})

	require.NoError(t, svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{VoteID: "r1", Abstain: true}))

	require.NotNil(t, client.last)
	assert.NotNil(t, client.last.UserVoteContent, "always a list on the wire, never null")
	assert.Empty(t, client.last.UserVoteContent)
	assert.NotNil(t, client.last.CommentResponses)
	assert.Empty(t, client.last.CommentResponses)
	assert.True(t, client.last.Abstain)
}

func TestVoteResponseService_CreateVoteResponse_ITXErrorIsReturnedUnchanged(t *testing.T) {
	client := &createCaptureClient{err: domain.NewConflictError("ballot already submitted")}
	svc := newResponseSvc(client, identityIDMapper{})

	err := svc.CreateVoteResponse(ctxWithPrincipal("alice"), &CreateVoteResponseRequest{VoteID: "r1"})

	require.Error(t, err)
	assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	assert.Equal(t, "ballot already submitted", err.Error())
}

func TestVoteResponseService_JWTAuth(t *testing.T) {
	scheme := &goaauth.JWTScheme{}

	t.Run("a valid token stores the principal in the context", func(t *testing.T) {
		svc := NewVoteResponseService(&mockAuthenticator{principal: "alice"}, nil, identityIDMapper{}, discardLogger())

		ctx, err := svc.JWTAuth(context.Background(), "token", scheme)

		require.NoError(t, err)
		assert.Equal(t, "alice", ctx.Value(constants.PrincipalContextID))
	})

	t.Run("an invalid token is a validation error and leaves no principal", func(t *testing.T) {
		cause := errors.New("signature invalid")
		svc := NewVoteResponseService(&mockAuthenticator{err: cause}, nil, identityIDMapper{}, discardLogger())

		ctx, err := svc.JWTAuth(context.Background(), "token", scheme)

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
		assert.ErrorIs(t, err, cause)
		assert.Nil(t, ctx.Value(constants.PrincipalContextID))
	})
}
