// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

// Tests for handleError and createVoteResponseError, which are the final
// domain-error → HTTP-error translation layer for every API response.
//
// Risk: if a new ErrorType is added to domain/errors.go and handleError is
// not updated, every operation that returns that type will silently respond
// with 500 instead of the intended status code. These tests pin each mapping.

import (
	"errors"
	"net/http"
	"testing"

	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

// TestHandleError verifies the domain ErrorType → HTTP status code mapping.
//
// The Goa framework uses the concrete error type (e.g. *votesvc.NotFoundError)
// to select the response status code, so the type produced by handleError is
// what determines the HTTP status code seen by the client.
func TestHandleError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		wantCode int
		wantType interface{} // expected concrete Goa error type
	}{
		{
			name:     "Validation → 400 BadRequestError",
			input:    domain.NewValidationError("bad input"),
			wantCode: http.StatusBadRequest,
			wantType: &votesvc.BadRequestError{},
		},
		{
			name:     "NotFound → 404 NotFoundError",
			input:    domain.NewNotFoundError("poll not found"),
			wantCode: http.StatusNotFound,
			wantType: &votesvc.NotFoundError{},
		},
		{
			name:     "Conflict → 409 ConflictError",
			input:    domain.NewConflictError("already exists"),
			wantCode: http.StatusConflict,
			wantType: &votesvc.ConflictError{},
		},
		{
			name:     "Internal → 500 InternalServerError",
			input:    domain.NewInternalError("unexpected failure"),
			wantCode: http.StatusInternalServerError,
			wantType: &votesvc.InternalServerError{},
		},
		{
			name:     "Unavailable → 503 ServiceUnavailableError",
			input:    domain.NewUnavailableError("NATS down"),
			wantCode: http.StatusServiceUnavailable,
			wantType: &votesvc.ServiceUnavailableError{},
		},
		{
			name:     "non-domain error defaults to 500",
			input:    errors.New("unexpected raw error"),
			wantCode: http.StatusInternalServerError,
			wantType: &votesvc.InternalServerError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handleError(tt.input)
			if got == nil {
				t.Fatal("expected non-nil error from handleError")
			}

			// Verify error message is preserved so clients receive actionable detail.
			if got.Error() == "" {
				t.Error("expected non-empty error message")
			}

			// Verify concrete type matches what Goa uses to set the HTTP status code.
			switch tt.wantType.(type) {
			case *votesvc.BadRequestError:
				var e *votesvc.BadRequestError
				if !errors.As(got, &e) {
					t.Errorf("want *votesvc.BadRequestError, got %T", got)
				}
			case *votesvc.NotFoundError:
				var e *votesvc.NotFoundError
				if !errors.As(got, &e) {
					t.Errorf("want *votesvc.NotFoundError, got %T", got)
				}
			case *votesvc.ConflictError:
				var e *votesvc.ConflictError
				if !errors.As(got, &e) {
					t.Errorf("want *votesvc.ConflictError, got %T", got)
				}
			case *votesvc.InternalServerError:
				var e *votesvc.InternalServerError
				if !errors.As(got, &e) {
					t.Errorf("want *votesvc.InternalServerError, got %T", got)
				}
			case *votesvc.ServiceUnavailableError:
				var e *votesvc.ServiceUnavailableError
				if !errors.As(got, &e) {
					t.Errorf("want *votesvc.ServiceUnavailableError, got %T", got)
				}
			}
		})
	}
}

// TestHandleError_MessagePreserved verifies that the original error message reaches
// the client. A future refactor that passes a generic "internal error" string instead
// of the domain message would make debugging significantly harder.
func TestHandleError_MessagePreserved(t *testing.T) {
	msg := "poll abc-123 was not found"
	got := handleError(domain.NewNotFoundError(msg))

	var e *votesvc.NotFoundError
	if !errors.As(got, &e) {
		t.Fatalf("expected *votesvc.NotFoundError, got %T", got)
	}
	if e.Message != msg {
		t.Errorf("want message %q, got %q", msg, e.Message)
	}
}
