// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy

import (
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

// TestMapHTTPError protects the HTTP status → domain error type mapping in mapHTTPError.
//
// This mapping is security-relevant: 401/403 intentionally map to ErrorTypeInternal
// (not ErrorTypeValidation) because the service uses M2M credentials that should
// never be rejected by ITX, so an auth failure implies an infrastructure problem.
// If this mapping is accidentally changed, clients will receive incorrect HTTP
// status codes and downstream error handling will break.
func TestMapHTTPError(t *testing.T) {
	c := &Client{} // mapHTTPError uses only json and domain — no httpClient needed

	tests := []struct {
		name        string
		statusCode  int
		body        []byte
		wantType    domain.ErrorType
		wantMessage string
	}{
		{
			name:       "400 maps to Validation",
			statusCode: 400,
			body:       []byte(`{"message":"invalid request"}`),
			wantType:   domain.ErrorTypeValidation,
		},
		{
			name:       "400 falls back to error field when message absent",
			statusCode: 400,
			body:       []byte(`{"error":"bad input"}`),
			wantType:   domain.ErrorTypeValidation,
		},
		{
			name:        "400 with empty body uses default message",
			statusCode:  400,
			body:        []byte(`{}`),
			wantType:    domain.ErrorTypeValidation,
			wantMessage: "ITX API error: HTTP 400",
		},
		{
			// 401 from ITX indicates broken M2M credentials, not a user auth failure.
			// Mapping it to Internal instead of Validation is deliberate — do not change.
			name:       "401 maps to Internal (not Validation)",
			statusCode: 401,
			body:       []byte(`{"message":"unauthorized"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			// Same reasoning as 401 — bad M2M setup, not a caller permission problem.
			name:       "403 maps to Internal (not Validation)",
			statusCode: 403,
			body:       []byte(`{"message":"forbidden"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			name:       "404 maps to NotFound",
			statusCode: 404,
			body:       []byte(`{"message":"poll not found"}`),
			wantType:   domain.ErrorTypeNotFound,
		},
		{
			name:       "409 maps to Conflict",
			statusCode: 409,
			body:       []byte(`{"message":"already exists"}`),
			wantType:   domain.ErrorTypeConflict,
		},
		{
			name:       "429 maps to Unavailable",
			statusCode: 429,
			body:       []byte(`{"message":"rate limited"}`),
			wantType:   domain.ErrorTypeUnavailable,
		},
		{
			name:       "503 maps to Unavailable",
			statusCode: 503,
			body:       []byte(`{"message":"service unavailable"}`),
			wantType:   domain.ErrorTypeUnavailable,
		},
		{
			name:       "500 maps to Internal",
			statusCode: 500,
			body:       []byte(`{"message":"internal error"}`),
			wantType:   domain.ErrorTypeInternal,
		},
		{
			name:       "unrecognised 5xx maps to Internal",
			statusCode: 502,
			body:       []byte(`{"message":"bad gateway"}`),
			wantType:   domain.ErrorTypeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.mapHTTPError(tt.statusCode, tt.body)
			if err == nil {
				t.Fatal("expected non-nil error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
			}

			if domainErr.Type != tt.wantType {
				t.Errorf("status %d: want ErrorType %d, got %d (message: %q)",
					tt.statusCode, tt.wantType, domainErr.Type, domainErr.Message)
			}

			if tt.wantMessage != "" && domainErr.Message != tt.wantMessage {
				t.Errorf("want message %q, got %q", tt.wantMessage, domainErr.Message)
			}
		})
	}
}
