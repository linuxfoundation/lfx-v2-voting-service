// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// Tests for personalized vote links: the bulk resend opt-in on VoteService and the
// token-authorized submit/update methods on VoteResponseService.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// fakeLinkSigner signs tokens as "tok:<vote>:<response>" and verifies only that form.
type fakeLinkSigner struct {
	signErr error
}

func (f fakeLinkSigner) Sign(voteUID, voteResponseUID string) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	return "tok:" + voteUID + ":" + voteResponseUID, nil
}

func (fakeLinkSigner) Verify(token string) (*domain.VoteLinkClaims, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "tok" {
		return nil, errors.New("invalid")
	}
	return &domain.VoteLinkClaims{VoteUID: parts[1], VoteResponseUID: parts[2], KeyID: "k1"}, nil
}

// captureBulkResendClient records the last BulkResendPoll call.
type captureBulkResendClient struct {
	domain.PollClient
	pollID string
	req    *itx.BulkResendRequest
	err    error
}

func (c *captureBulkResendClient) BulkResendPoll(_ context.Context, pollID string, req *itx.BulkResendRequest) error {
	c.pollID = pollID
	c.req = req
	return c.err
}

// captureLinkVoteClient records create and update calls made through the vote link methods.
type captureLinkVoteClient struct {
	domain.VoteResponseClient
	createReq    *itx.CreateVoteRequest
	updateVoteID string
	updateReq    *itx.UpdateVoteRequest
	err          error
}

func (c *captureLinkVoteClient) CreateVote(_ context.Context, req *itx.CreateVoteRequest) error {
	c.createReq = req
	return c.err
}

func (c *captureLinkVoteClient) UpdateVote(_ context.Context, voteID string, req *itx.UpdateVoteRequest) error {
	c.updateVoteID = voteID
	c.updateReq = req
	return c.err
}

func principalCtx() context.Context {
	return context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
}

func assertErrorType(t *testing.T, err error, want domain.ErrorType) {
	t.Helper()
	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T (%v)", err, err)
	}
	if dErr.Type != want {
		t.Errorf("error type = %d, want %d (%v)", dErr.Type, want, err)
	}
}

// ---- BulkResendVote ---------------------------------------------------------

func TestBulkResendVote_WithoutPersonalizedLinks_RequestUnchanged(t *testing.T) {
	client := &captureBulkResendClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger(),
		WithVoteLinks(fakeLinkSigner{}, "https://app.lfx.dev"))

	if err := svc.BulkResendVote(principalCtx(), "vote-1", []string{"r1", "r2"}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.req.RecipientLinks != nil {
		t.Errorf("RecipientLinks = %v, want nil", client.req.RecipientLinks)
	}
	body, err := json.Marshal(client.req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(body) != `{"recipient_ids":["r1","r2"]}` {
		t.Errorf("ITX body = %s, want the pre-feature shape", body)
	}
}

func TestBulkResendVote_PersonalizedLinks_BuildsLinkPerRecipient(t *testing.T) {
	client := &captureBulkResendClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger(),
		WithVoteLinks(fakeLinkSigner{}, "https://app.lfx.dev/"))

	err := svc.BulkResendVote(principalCtx(), "vote-1", []string{"r1", "r2", "r1"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.pollID != "vote-1" {
		t.Errorf("pollID = %q", client.pollID)
	}
	if len(client.req.RecipientIDs) != 3 {
		t.Errorf("RecipientIDs should pass through unchanged, got %v", client.req.RecipientIDs)
	}
	want := map[string]string{
		"r1": "https://app.lfx.dev/votes/vote-1#vote_token=" + url.QueryEscape("tok:vote-1:r1"),
		"r2": "https://app.lfx.dev/votes/vote-1#vote_token=" + url.QueryEscape("tok:vote-1:r2"),
	}
	if len(client.req.RecipientLinks) != len(want) {
		t.Fatalf("RecipientLinks = %v, want %v", client.req.RecipientLinks, want)
	}
	for id, link := range want {
		if client.req.RecipientLinks[id] != link {
			t.Errorf("link[%s] = %q, want %q", id, client.req.RecipientLinks[id], link)
		}
	}
}

func TestBulkResendVote_PersonalizedLinks_Errors(t *testing.T) {
	cases := []struct {
		name       string
		opts       []VoteServiceOption
		recipients []string
		want       domain.ErrorType
	}{
		{"not configured", nil, []string{"r1"}, domain.ErrorTypeUnavailable},
		{"empty recipients", []VoteServiceOption{WithVoteLinks(fakeLinkSigner{}, "https://app.lfx.dev")}, []string{}, domain.ErrorTypeValidation},
		{"blank recipient", []VoteServiceOption{WithVoteLinks(fakeLinkSigner{}, "https://app.lfx.dev")}, []string{"r1", " "}, domain.ErrorTypeValidation},
		{"sign failure", []VoteServiceOption{WithVoteLinks(fakeLinkSigner{signErr: errors.New("boom")}, "https://app.lfx.dev")}, []string{"r1"}, domain.ErrorTypeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &captureBulkResendClient{}
			svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger(), tc.opts...)

			err := svc.BulkResendVote(principalCtx(), "vote-1", tc.recipients, true)
			assertErrorType(t, err, tc.want)
			if client.req != nil {
				t.Error("ITX must not be called when link building fails")
			}
		})
	}
}

// ---- CreateVoteResponseByLink / UpdateVoteResponseByLink ------------------

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
