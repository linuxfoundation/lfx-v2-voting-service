// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

// NewContractTestClient constructs a client for external contract tests without M2M auth.
// It is compiled only into the test binary, not the production package.
func NewContractTestClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, config: Config{BaseURL: baseURL}}
}

// TestEndPoll protects the ITX end request shape (method, path, headers, no body) and the
// mapping of ITX responses onto the domain.
func TestEndPoll(t *testing.T) {
	t.Run("sends a bodiless POST and decodes the ended poll", func(t *testing.T) {
		var gotMethod, gotPath, gotScope, gotAccept, gotContentType string
		var gotBody []byte
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			gotScope, gotAccept = r.Header.Get("x-scope"), r.Header.Get("Accept")
			gotContentType = r.Header.Get("Content-Type")
			gotBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"poll_id":"poll-1","status":"ended","end_time":"2026-03-01T23:59:59Z","early_end_time":"2026-02-20T09:00:00Z"}`)
		}))
		defer upstream.Close()

		c := NewContractTestClient(upstream.Client(), upstream.URL+"/")
		got, err := c.EndPoll(context.Background(), "poll-1")
		if err != nil {
			t.Fatalf("EndPoll returned error: %v", err)
		}
		if gotMethod != http.MethodPost || gotPath != "/v2/voting/poll/poll-1/end" {
			t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
		}
		if gotScope != "manage:voting" {
			t.Errorf("x-scope: want manage:voting, got %q", gotScope)
		}
		if gotAccept != "application/json" {
			t.Errorf("Accept: want application/json, got %q", gotAccept)
		}
		if gotContentType != "" {
			t.Errorf("Content-Type must be unset for a bodiless request, got %q", gotContentType)
		}
		if len(gotBody) != 0 {
			t.Errorf("request body must be empty, got %q", gotBody)
		}
		if got.Status != "ended" || got.EarlyEndTime != "2026-02-20T09:00:00Z" || got.EndTime != "2026-03-01T23:59:59Z" {
			t.Errorf("unexpected decoded poll: %+v", got)
		}
	})

	errorTests := []struct {
		name     string
		status   int
		body     string
		wantType domain.ErrorType
	}{
		{name: "400 maps to validation", status: http.StatusBadRequest, body: `{"code":"400","message":"poll has not started"}`, wantType: domain.ErrorTypeValidation},
		{name: "404 maps to not found", status: http.StatusNotFound, body: `{"code":"404","message":"poll not found"}`, wantType: domain.ErrorTypeNotFound},
		{name: "409 maps to conflict", status: http.StatusConflict, body: `{"code":"409","message":"poll was updated concurrently; retry the end request"}`, wantType: domain.ErrorTypeConflict},
	}
	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer upstream.Close()

			c := NewContractTestClient(upstream.Client(), upstream.URL+"/")
			_, err := c.EndPoll(context.Background(), "poll-1")
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := domain.GetErrorType(err); got != tt.wantType {
				t.Errorf("want ErrorType %d, got %d (%v)", tt.wantType, got, err)
			}
		})
	}
}

// TestGetPollResults protects the ITX results request shape and the decoding of held,
// unlocked, text-only and named results without inventing zero/false metadata.
func TestGetPollResults(t *testing.T) {
	serve := func(t *testing.T, status int, body string) (*Client, *[2]string) {
		t.Helper()
		var seen [2]string // method+path, scope+accept
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen[0] = r.Method + " " + r.URL.Path
			seen[1] = r.Header.Get("x-scope") + "|" + r.Header.Get("Accept")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(upstream.Close)
		return NewContractTestClient(upstream.Client(), upstream.URL+"/"), &seen
	}

	t.Run("held response keeps the flag and counts without result data", func(t *testing.T) {
		c, seen := serve(t, http.StatusOK, `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z","poll_end_time_timezone":"UTC"}`)
		got, err := c.GetPollResults(context.Background(), "poll-1")
		if err != nil {
			t.Fatalf("GetPollResults returned error: %v", err)
		}
		if seen[0] != "GET /v2/voting/poll/poll-1/results" || seen[1] != "manage:voting|application/json" {
			t.Errorf("unexpected request: %v", *seen)
		}
		if got.ResultsHeld == nil || !*got.ResultsHeld {
			t.Errorf("results_held must be preserved as true: %+v", got.ResultsHeld)
		}
		if got.NumAbstained != nil || got.PollResults != nil || got.CommentResults != nil {
			t.Errorf("held response must not gain result data: %+v", got)
		}
		if got.NumRecipients != 10 || got.NumVotesCast != 4 || got.PollEndTime != "2030-01-01T00:00:00Z" {
			t.Errorf("participation or deadline lost: %+v", got)
		}
	})

	t.Run("explicit false and zero survive", func(t *testing.T) {
		c, _ := serve(t, http.StatusOK, `{"results_held":false,"num_abstained":0,"num_votes_cast":2}`)
		got, err := c.GetPollResults(context.Background(), "poll-1")
		if err != nil {
			t.Fatalf("GetPollResults returned error: %v", err)
		}
		if got.ResultsHeld == nil || *got.ResultsHeld {
			t.Errorf("explicit results_held=false must stay present: %+v", got.ResultsHeld)
		}
		if got.NumAbstained == nil || *got.NumAbstained != 0 {
			t.Errorf("explicit num_abstained=0 must stay present: %+v", got.NumAbstained)
		}
	})

	t.Run("text-only anonymous comments stay in upstream order", func(t *testing.T) {
		c, _ := serve(t, http.StatusOK, `{"num_abstained":1,"comment_results":[{"prompt":{"prompt_id":"c1"},"responses":[{"comment_text":"zeta"},{"comment_text":"alpha"},{"comment_text":"alpha"}]}]}`)
		got, err := c.GetPollResults(context.Background(), "poll-1")
		if err != nil {
			t.Fatalf("GetPollResults returned error: %v", err)
		}
		if got.ResultsHeld != nil {
			t.Errorf("missing results_held must stay absent: %v", *got.ResultsHeld)
		}
		responses := got.CommentResults[0].Responses
		if len(responses) != 3 || responses[0].CommentText != "zeta" || responses[1].CommentText != "alpha" || responses[2].CommentText != "alpha" {
			t.Fatalf("comment order or duplicates changed: %+v", responses)
		}
		for i, r := range responses {
			if r.VoteID != nil || r.VoteCreationTime != nil || r.Abstained != nil {
				t.Errorf("response %d gained metadata: %+v", i, r)
			}
		}
	})

	t.Run("named comments keep ids, times and abstained values", func(t *testing.T) {
		c, _ := serve(t, http.StatusOK, `{"comment_results":[{"responses":[{"vote_id":"v1","comment_text":"a","vote_creation_time":"2030-01-01T00:00:00Z","abstained":false}]}]}`)
		got, err := c.GetPollResults(context.Background(), "poll-1")
		if err != nil {
			t.Fatalf("GetPollResults returned error: %v", err)
		}
		r := got.CommentResults[0].Responses[0]
		if r.VoteID == nil || *r.VoteID != "v1" || r.VoteCreationTime == nil || r.Abstained == nil || *r.Abstained {
			t.Errorf("named metadata lost: %+v", r)
		}
	})

	t.Run("wrong scalar type is a parse error", func(t *testing.T) {
		c, _ := serve(t, http.StatusOK, `{"results_held":"yes"}`)
		_, err := c.GetPollResults(context.Background(), "poll-1")
		if err == nil {
			t.Fatal("expected an error")
		}
		if got := domain.GetErrorType(err); got != domain.ErrorTypeInternal {
			t.Errorf("want ErrorTypeInternal, got %d", got)
		}
	})

	t.Run("404 maps to not found", func(t *testing.T) {
		c, _ := serve(t, http.StatusNotFound, `{"message":"poll not found"}`)
		_, err := c.GetPollResults(context.Background(), "poll-1")
		if err == nil {
			t.Fatal("expected an error")
		}
		if got := domain.GetErrorType(err); got != domain.ErrorTypeNotFound {
			t.Errorf("want ErrorTypeNotFound, got %d", got)
		}
	})
}
