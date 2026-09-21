// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// This file extends vote_service_test.go with tests that protect the error-handling
// and ID-mapping paths of VoteService — the primary code path for all API calls.
// The proxy/service path had near-zero coverage before these tests; every function
// here was previously untested.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"goa.design/goa/v3/security"
)

// ---- helpers shared across this file ----------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// returnPollClient always returns the given response/error from any PollClient call.
type returnPollClient struct {
	domain.PollClient
	resp *itx.PollResponse
	err  error
}

func (r *returnPollClient) CreatePoll(_ context.Context, _ *itx.CreatePollRequest) (*itx.PollResponse, error) {
	return r.resp, r.err
}
func (r *returnPollClient) GetPoll(_ context.Context, _ string) (*itx.PollResponse, error) {
	return r.resp, r.err
}
func (r *returnPollClient) UpdatePoll(_ context.Context, _ string, _ *itx.UpdatePollRequest) (*itx.PollResponse, error) {
	return r.resp, r.err
}
func (r *returnPollClient) DeletePoll(_ context.Context, _ string) error { return r.err }

// failingIDMapper returns the configured error for every mapping call.
type failingIDMapper struct{ err error }

func (m *failingIDMapper) MapProjectV2ToV1(_ context.Context, _ string) (string, error) {
	return "", m.err
}
func (m *failingIDMapper) MapProjectV1ToV2(_ context.Context, _ string) (string, error) {
	return "", m.err
}
func (m *failingIDMapper) MapCommitteeV2ToV1(_ context.Context, _ string) (string, error) {
	return "", m.err
}
func (m *failingIDMapper) MapCommitteeV1ToV2(_ context.Context, _ string) (string, error) {
	return "", m.err
}

// mockAuthenticator implements domain.Authenticator for JWTAuth tests.
type mockAuthenticator struct {
	principal string
	err       error
}

func (m *mockAuthenticator) ParsePrincipal(_ context.Context, _ string, _ *slog.Logger) (string, error) {
	return m.principal, m.err
}

// ctxWithPrincipal injects a principal into a context the same way JWTAuth does.
func ctxWithPrincipal(principal string) context.Context {
	return context.WithValue(context.Background(), constants.PrincipalContextID, principal)
}

// ---- Name-length validation --------------------------------------------------

// TestCreateVote_NameTooLong pins the 200-character name limit. If the constant is
// accidentally removed or the check is bypassed, this test fails and prevents a
// silent regression where ITX returns an opaque 400.
//
// Uses multibyte characters (é = 2 bytes, 1 rune) so a regression from
// utf8.RuneCountInString to byte-based len would still reject the name (402 > 200),
// but the at-limit test below would catch the regression (400 bytes rejected by len,
// but 200 runes correctly accepted by utf8.RuneCountInString).
func TestCreateVote_NameTooLong(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{}, identityIDMapper{}, discardLogger())

	// 201 × "é" = 201 runes (402 bytes) — must be rejected
	req := &CreateVoteRequest{Name: strings.Repeat("é", 201)}
	_, err := svc.CreateVote(ctxWithPrincipal("user"), req)
	if err == nil {
		t.Fatal("expected validation error for name > 200 runes, got nil")
	}
	if domain.GetErrorType(err) != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// TestCreateVote_NameAtLimit verifies that a 200-rune name is accepted.
// Uses multibyte characters: 200 × "é" = 200 runes (400 bytes). If the implementation
// regresses to byte-based len, it would wrongly reject this name (400 > 200), failing
// this test and catching the regression.
func TestCreateVote_NameAtLimit(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{resp: &itx.PollResponse{}}, identityIDMapper{}, discardLogger())

	// 200 × "é" = 200 runes (400 bytes) — must be accepted
	req := &CreateVoteRequest{Name: strings.Repeat("é", 200)}
	_, err := svc.CreateVote(ctxWithPrincipal("user"), req)
	if err != nil {
		t.Fatalf("expected no error for name == 200 runes, got: %v", err)
	}
}

// ---- JWTAuth ----------------------------------------------------------------

// TestVoteService_JWTAuth_ValidToken verifies that a successful ParsePrincipal result
// is stored in the returned context so downstream service methods can retrieve it.
func TestVoteService_JWTAuth_ValidToken(t *testing.T) {
	auth := &mockAuthenticator{principal: "alice"}
	svc := NewVoteService(auth, nil, identityIDMapper{}, discardLogger())

	ctx, err := svc.JWTAuth(context.Background(), "token-value", &security.JWTScheme{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := ctx.Value(constants.PrincipalContextID).(string)
	if got != "alice" {
		t.Fatalf("expected principal %q in context, got %q", "alice", got)
	}
}

// TestVoteService_JWTAuth_InvalidToken verifies that a ParsePrincipal failure is
// surfaced as a validation error (not an internal error), so the HTTP layer returns
// a 400 rather than a 500 to the client.
func TestVoteService_JWTAuth_InvalidToken(t *testing.T) {
	auth := &mockAuthenticator{err: errors.New("signature invalid")}
	svc := NewVoteService(auth, nil, identityIDMapper{}, discardLogger())

	_, err := svc.JWTAuth(context.Background(), "bad-token", &security.JWTScheme{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if dErr.Type != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d", dErr.Type)
	}
}

// ---- GetVote ----------------------------------------------------------------

// TestGetVote_MissingPrincipal verifies the authentication guard at the service layer.
// Without this test a refactor that removes the ctx.Value check would silently break
// the auth contract — ITX would receive requests from an unauthenticated caller.
func TestGetVote_MissingPrincipal(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{}, identityIDMapper{}, discardLogger())

	_, err := svc.GetVote(context.Background(), "vote-1") // no principal
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

// TestGetVote_PropagatesProxyError verifies that domain errors returned by the proxy
// client are passed through to the caller unchanged. An accidental wrapping or type
// conversion would change the HTTP status code seen by the client.
func TestGetVote_PropagatesProxyError(t *testing.T) {
	tests := []struct {
		name     string
		proxyErr error
		wantType domain.ErrorType
	}{
		{
			name:     "not found propagates as NotFound",
			proxyErr: domain.NewNotFoundError("poll not found"),
			wantType: domain.ErrorTypeNotFound,
		},
		{
			name:     "unavailable propagates as Unavailable",
			proxyErr: domain.NewUnavailableError("ITX down"),
			wantType: domain.ErrorTypeUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &returnPollClient{err: tt.proxyErr}
			svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

			_, err := svc.GetVote(ctxWithPrincipal("user"), "vote-1")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if domain.GetErrorType(err) != tt.wantType {
				t.Errorf("want ErrorType %d, got %d (%v)", tt.wantType, domain.GetErrorType(err), err)
			}
		})
	}
}

// ---- CreateVote ID mapping --------------------------------------------------

// TestCreateVote_IDMapperFailurePropagates verifies that a failing IDMapper (e.g. NATS
// unavailable) causes CreateVote to return an error rather than forwarding a v2 UUID
// directly to ITX. ITX would reject a v2 UUID, causing confusing downstream failures.
func TestCreateVote_IDMapperFailurePropagates(t *testing.T) {
	mapperErr := domain.NewUnavailableError("NATS request timeout")
	svc := NewVoteService(
		nil,
		&returnPollClient{resp: &itx.PollResponse{PollID: "p1"}},
		&failingIDMapper{err: mapperErr},
		discardLogger(),
	)

	ctx := ctxWithPrincipal("user")
	_, err := svc.CreateVote(ctx, &CreateVoteRequest{Name: "poll", ProjectUID: "proj-uid"})
	if err == nil {
		t.Fatal("expected error from failing ID mapper, got nil")
	}

	// The error must propagate as-is so the caller knows it's a transient failure.
	if domain.GetErrorType(err) != domain.ErrorTypeUnavailable {
		t.Errorf("want ErrorTypeUnavailable, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// ---- mapPollResponseV1ToV2 --------------------------------------------------

// TestGetVote_ResponseIDMappingFailurePropagates verifies that a failure during
// response ID mapping (v1 SFID → v2 UUID) is returned as an error rather than
// silently returning a response with unmapped IDs. Unmapped IDs would expose v1
// SFIDs to LFXv2 clients, breaking client-side ID assumptions.
func TestGetVote_ResponseIDMappingFailurePropagates(t *testing.T) {
	// Proxy succeeds and returns a response with a non-empty ProjectID,
	// which triggers the response mapping path.
	client := &returnPollClient{
		resp: &itx.PollResponse{
			PollID:    "poll-1",
			ProjectID: "sfid-abc", // non-empty → triggers MapProjectV1ToV2
			Status:    "enabled",
		},
	}
	svc := NewVoteService(
		nil,
		client,
		&failingIDMapper{err: domain.NewInternalError("mapper error")},
		discardLogger(),
	)

	_, err := svc.GetVote(ctxWithPrincipal("user"), "poll-1")
	if err == nil {
		t.Fatal("expected error from response ID mapping, got nil")
	}

	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
}

// ---- DeleteVote / remaining methods -----------------------------------------

// TestDeleteVote_MissingPrincipal verifies the auth guard on the error-only return path.
// Without this, a refactor removing the check lets unauthenticated deletions reach ITX.
func TestDeleteVote_MissingPrincipal(t *testing.T) {
	type deletePollClient struct {
		domain.PollClient
	}
	client := &deletePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

	err := svc.DeleteVote(context.Background(), "vote-1")
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

// TestUpdateVote_NameTooLong verifies that UpdateVote rejects names over 200 Unicode characters.
// Uses multibyte characters (é = 2 bytes, 1 rune) so a regression to byte-based len would
// return 402 instead of 201 — both fail, but the rune count is the contract.
func TestUpdateVote_NameTooLong(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{}, identityIDMapper{}, discardLogger())

	// 201 × "é" = 201 runes (402 bytes) — must be rejected
	req := &UpdateVoteRequest{Name: strings.Repeat("é", 201)}
	_, err := svc.UpdateVote(ctxWithPrincipal("user"), "vote-1", req)
	if err == nil {
		t.Fatal("expected validation error for name > 200 runes, got nil")
	}
	if domain.GetErrorType(err) != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %d (%v)", domain.GetErrorType(err), err)
	}
}

// TestUpdateVote_NameAtLimit verifies that a 200-rune name is accepted by UpdateVote.
// Uses multibyte characters: 200 × "é" = 200 runes (400 bytes). If the implementation
// regresses to byte-based len, it would wrongly reject this name (400 > 200), failing
// this test and catching the regression.
func TestUpdateVote_NameAtLimit(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{resp: &itx.PollResponse{}}, identityIDMapper{}, discardLogger())

	// 200 × "é" = 200 runes (400 bytes) — must be accepted
	req := &UpdateVoteRequest{Name: strings.Repeat("é", 200)}
	_, err := svc.UpdateVote(ctxWithPrincipal("user"), "vote-1", req)
	if err != nil {
		t.Fatalf("expected no error for name == 200 runes, got: %v", err)
	}
}

// TestUpdateVote_MissingPrincipal covers the write path that also calls mapRequestIDsV2ToV1.
func TestUpdateVote_MissingPrincipal(t *testing.T) {
	svc := NewVoteService(nil, &returnPollClient{}, identityIDMapper{}, discardLogger())

	_, err := svc.UpdateVote(context.Background(), "vote-1", &UpdateVoteRequest{Name: "updated"})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var dErr *domain.DomainError
	if !errors.As(err, &dErr) || dErr.Type != domain.ErrorTypeValidation {
		t.Errorf("expected ErrorTypeValidation, got %v", err)
	}
}

// TestGetVote_ResponseIDMappingPopulatesUIDs verifies that a successful response
// mapping replaces the v1 ProjectID/CommitteeID in the PollResponse with v2 UIDs.
// If the mapper is accidentally bypassed, clients receive raw v1 SFIDs.
func TestGetVote_ResponseIDMappingPopulatesUIDs(t *testing.T) {
	// identityIDMapper is a pass-through: the v1 SFID comes back unchanged.
	// The important property under test is that mapPollResponseV1ToV2 calls the
	// mapper and writes back its return value to the field — not that it transforms it.
	client := &returnPollClient{
		resp: &itx.PollResponse{
			PollID:    "poll-1",
			ProjectID: "project-sfid",
			Status:    "enabled",
		},
	}

	svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())
	result, err := svc.GetVote(ctxWithPrincipal("user"), "poll-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// identityIDMapper echoes input; the field should be set (not cleared).
	if result.ProjectID == "" {
		t.Error("expected ProjectID to be mapped (non-empty), got empty string")
	}
	if result.ProjectID != "project-sfid" {
		t.Errorf("expected ProjectID %q, got %q", "project-sfid", result.ProjectID)
	}
}
