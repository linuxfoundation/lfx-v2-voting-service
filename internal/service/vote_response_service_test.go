// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// Tests for VoteResponseService — the service layer for ballot submissions.
//
// Patterns mirrored from vote_service_error_test.go: principal guard on every
// method + proxy error propagation + response ID mapping. VoteResponseService
// was previously 0% covered.

import (
	"context"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
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
	if result.ProjectID != "uid:proj-sfid" {
		t.Errorf("expected ProjectID %q (mapper applied), got %q", "uid:proj-sfid", result.ProjectID)
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
