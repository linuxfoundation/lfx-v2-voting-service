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

	"bytes"
	"encoding/json"
	"log/slog"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	converters "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/service"
	voteclient "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/client"
	voteserver "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/server"
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/votelink"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
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

// capturedRequest is what the fake ITX server saw.
type capturedRequest struct {
	Method      string
	Path        string
	Body        string
	Scope       string
	ContentType string
}

// newTestClient returns a Client pointed at a fake ITX server. The server records
// the last request and replies with the given status and body. The OAuth2 transport
// is bypassed (the plain test HTTP client is used) because these tests cover the
// request building and response handling, not token management.
func newTestClient(t *testing.T, status int, respBody string) (*Client, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = capturedRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			Body:        string(b),
			Scope:       r.Header.Get("x-scope"),
			ContentType: r.Header.Get("Content-Type"),
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return &Client{httpClient: srv.Client(), config: Config{BaseURL: srv.URL + "/"}}, got
}

// unreachableClient returns a Client whose server has already been shut down.
func unreachableClient(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/"
	srv.Close()
	return &Client{httpClient: http.DefaultClient, config: Config{BaseURL: url}}
}

// operation describes one Client method for the shared request/response tests.
type operation struct {
	name        string
	method      string
	path        string
	reqBody     string // complete expected JSON request body; empty when the request has none
	okStatus    int
	okBody      string                        // response body for the success case
	invoke      func(c *Client) error         // calls the method, discarding any result
	checkResult func(t *testing.T, c *Client) // optional: asserts on the parsed response
	parsesBody  bool                          // true when the method decodes a JSON response
}

func operations() []operation {
	ctx := context.Background()
	pollJSON := `{"poll_id":"poll-1","name":"Board Vote","status":"active","project_id":"proj-1","committee_id":"comm-1"}`

	return []operation{
		{
			name: "CreatePoll", method: http.MethodPost, path: "/v2/voting/poll",
			reqBody: `{"name":"Board Vote","description":"","end_time":"","project_id":"","committee_id":"","poll_questions":null,"pseudo_anonymity":false,"allow_abstain":false}`, okStatus: http.StatusCreated, okBody: pollJSON, parsesBody: true,
			invoke: func(c *Client) error {
				_, err := c.CreatePoll(ctx, &itx.CreatePollRequest{Name: "Board Vote"})
				return err
			},
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.CreatePoll(ctx, &itx.CreatePollRequest{Name: "Board Vote"})
				require.NoError(t, err)
				assert.Equal(t, "poll-1", got.PollID)
				assert.Equal(t, "active", got.Status)
				assert.Equal(t, "proj-1", got.ProjectID)
				assert.Equal(t, "comm-1", got.CommitteeID)
			},
		},
		{
			name: "GetPoll", method: http.MethodGet, path: "/v2/voting/poll/poll-1",
			okStatus: http.StatusOK, okBody: pollJSON, parsesBody: true,
			invoke: func(c *Client) error { _, err := c.GetPoll(ctx, "poll-1"); return err },
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.GetPoll(ctx, "poll-1")
				require.NoError(t, err)
				assert.Equal(t, "Board Vote", got.Name)
			},
		},
		{
			name: "UpdatePoll", method: http.MethodPut, path: "/v2/voting/poll/poll-1",
			reqBody: `{"name":"Renamed","description":"","end_time":"","project_id":"","committee_id":"","poll_questions":null,"pseudo_anonymity":false,"allow_abstain":false}`, okStatus: http.StatusOK, okBody: pollJSON, parsesBody: true,
			invoke: func(c *Client) error {
				_, err := c.UpdatePoll(ctx, "poll-1", &itx.UpdatePollRequest{Name: "Renamed"})
				return err
			},
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.UpdatePoll(ctx, "poll-1", &itx.UpdatePollRequest{Name: "Renamed"})
				require.NoError(t, err)
				assert.Equal(t, "poll-1", got.PollID)
				assert.Equal(t, "Board Vote", got.Name)
				assert.Equal(t, "active", got.Status)
				assert.Equal(t, "proj-1", got.ProjectID)
				assert.Equal(t, "comm-1", got.CommitteeID)
			},
		},
		{
			name: "DeletePoll", method: http.MethodDelete, path: "/v2/voting/poll/poll-1",
			okStatus: http.StatusNoContent,
			invoke:   func(c *Client) error { return c.DeletePoll(ctx, "poll-1") },
		},
		{
			name: "ExtendPoll", method: http.MethodPost, path: "/v2/voting/poll/poll-1/extend",
			reqBody:  `{"end_time":"2026-03-01T00:00:00Z","end_time_timezone":"America/New_York"}`,
			okStatus: http.StatusOK, okBody: pollJSON, parsesBody: true,
			invoke: func(c *Client) error {
				_, err := c.ExtendPoll(ctx, "poll-1", &itx.ExtendPollRequest{EndTime: "2026-03-01T00:00:00Z", EndTimeTimezone: "America/New_York"})
				return err
			},
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.ExtendPoll(ctx, "poll-1", &itx.ExtendPollRequest{EndTime: "2026-03-01T00:00:00Z"})
				require.NoError(t, err)
				assert.Equal(t, "poll-1", got.PollID)
			},
		},
		{
			name: "EnablePoll", method: http.MethodPut, path: "/v2/voting/poll/poll-1/enable",
			okStatus: http.StatusNoContent,
			invoke:   func(c *Client) error { return c.EnablePoll(ctx, "poll-1") },
		},
		{
			name: "BulkResendPoll", method: http.MethodPost, path: "/v2/voting/poll/poll-1/bulk_resend",
			reqBody: `{"recipient_ids":["vote-1","vote-2"]}`, okStatus: http.StatusNoContent,
			invoke: func(c *Client) error {
				return c.BulkResendPoll(ctx, "poll-1", &itx.BulkResendRequest{RecipientIDs: []string{"vote-1", "vote-2"}})
			},
		},
		{
			name: "GetPollResults", method: http.MethodGet, path: "/v2/voting/poll/poll-1/results",
			okStatus: http.StatusOK, parsesBody: true,
			okBody: `{"num_recipients":20,"num_votes_cast":15,"num_abstained":2,"poll_end_time":"2026-02-01T00:00:00Z"}`,
			invoke: func(c *Client) error { _, err := c.GetPollResults(ctx, "poll-1"); return err },
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.GetPollResults(ctx, "poll-1")
				require.NoError(t, err)
				assert.Equal(t, 20, got.NumRecipients)
				assert.Equal(t, 15, got.NumVotesCast)
				require.NotNil(t, got.NumAbstained)
				assert.Equal(t, 2, *got.NumAbstained)
				assert.Equal(t, "2026-02-01T00:00:00Z", got.PollEndTime)
			},
		},
		{
			name: "CreateVote", method: http.MethodPost, path: "/v2/voting/vote",
			reqBody: `{"vote_id":"vote-1","user_vote_content":null,"abstain":false}`, okStatus: http.StatusCreated,
			invoke: func(c *Client) error { return c.CreateVote(ctx, &itx.CreateVoteRequest{VoteID: "vote-1"}) },
		},
		{
			name: "GetVote", method: http.MethodGet, path: "/v2/voting/vote/vote-1",
			okStatus: http.StatusOK, parsesBody: true,
			okBody: `{"vote_id":"vote-1","poll_id":"poll-1","user_email":"alice@example.com","abstained":true}`,
			invoke: func(c *Client) error { _, err := c.GetVote(ctx, "vote-1"); return err },
			checkResult: func(t *testing.T, c *Client) {
				got, err := c.GetVote(ctx, "vote-1")
				require.NoError(t, err)
				assert.Equal(t, "vote-1", got.VoteID)
				assert.Equal(t, "poll-1", got.PollID)
				assert.Equal(t, "alice@example.com", got.UserEmail)
				assert.True(t, got.Abstained)
			},
		},
		{
			name: "UpdateVote", method: http.MethodPut, path: "/v2/voting/vote/vote-1",
			reqBody: `{"user_vote_content":null,"abstain":true}`, okStatus: http.StatusOK,
			invoke: func(c *Client) error { return c.UpdateVote(ctx, "vote-1", &itx.UpdateVoteRequest{Abstain: true}) },
		},
		{
			name: "ResendVote", method: http.MethodPost, path: "/v2/voting/vote/vote-1/resend",
			okStatus: http.StatusNoContent,
			invoke:   func(c *Client) error { return c.ResendVote(ctx, "vote-1") },
		},
		{
			name: "AcceptInvite", method: http.MethodPost, path: "/v2/voting/vote/invite_accepted",
			reqBody: `{"email":"alice@example.com","username":"alice"}`, okStatus: http.StatusNoContent,
			invoke: func(c *Client) error { return c.AcceptInvite(ctx, "alice@example.com", "alice") },
		},
	}
}

func TestClient_SendsExpectedRequest(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			c, got := newTestClient(t, op.okStatus, op.okBody)

			require.NoError(t, op.invoke(c))

			assert.Equal(t, op.method, got.Method)
			assert.Equal(t, op.path, got.Path)
			assert.Equal(t, "manage:voting", got.Scope, "every ITX call must carry the manage:voting scope")
			if op.reqBody == "" {
				assert.Empty(t, got.Body)
				return
			}
			assert.JSONEq(t, op.reqBody, got.Body)
			assert.Equal(t, "application/json", got.ContentType)
		})
	}
}

func TestClient_ParsesSuccessfulResponses(t *testing.T) {
	for _, op := range operations() {
		if op.checkResult == nil {
			continue
		}
		t.Run(op.name, func(t *testing.T) {
			c, _ := newTestClient(t, op.okStatus, op.okBody)
			op.checkResult(t, c)
		})
	}
}

func TestClient_MapsITXErrorStatuses(t *testing.T) {
	statuses := []struct {
		status int
		want   domain.ErrorType
	}{
		{http.StatusBadRequest, domain.ErrorTypeValidation},
		{http.StatusNotFound, domain.ErrorTypeNotFound},
		{http.StatusConflict, domain.ErrorTypeConflict},
		{http.StatusForbidden, domain.ErrorTypeInternal},
		{http.StatusServiceUnavailable, domain.ErrorTypeUnavailable},
		{http.StatusInternalServerError, domain.ErrorTypeInternal},
	}

	for _, op := range operations() {
		for _, s := range statuses {
			t.Run(op.name+"/"+http.StatusText(s.status), func(t *testing.T) {
				c, _ := newTestClient(t, s.status, `{"message":"itx said no"}`)

				err := op.invoke(c)

				require.Error(t, err)
				assert.Equal(t, s.want, domain.GetErrorType(err))
				assert.Contains(t, err.Error(), "itx said no")
			})
		}
	}
}

func TestClient_UnreachableITXIsUnavailable(t *testing.T) {
	for _, op := range operations() {
		t.Run(op.name, func(t *testing.T) {
			err := op.invoke(unreachableClient(t))

			require.Error(t, err)
			assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
		})
	}
}

func TestClient_MalformedSuccessBodyIsInternalError(t *testing.T) {
	for _, op := range operations() {
		if !op.parsesBody {
			continue
		}
		t.Run(op.name, func(t *testing.T) {
			c, _ := newTestClient(t, op.okStatus, `{not json`)

			err := op.invoke(c)

			require.Error(t, err)
			assert.Equal(t, domain.ErrorTypeInternal, domain.GetErrorType(err))
			assert.Contains(t, err.Error(), "parse")
		})
	}
}

func TestClient_CancelledContextIsUnavailable(t *testing.T) {
	c, _ := newTestClient(t, http.StatusNoContent, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.DeletePoll(ctx, "poll-1")

	require.Error(t, err)
	assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
}

func TestClient_PathIDsAreEmbeddedVerbatim(t *testing.T) {
	c, got := newTestClient(t, http.StatusNoContent, "")

	require.NoError(t, c.ResendVote(context.Background(), "00000000-0000-0000-0000-000000000001"))

	assert.Equal(t, "/v2/voting/vote/00000000-0000-0000-0000-000000000001/resend", got.Path)
}

func TestNewClient_RequiresPrivateKey(t *testing.T) {
	assert.PanicsWithValue(t, "ITX_CLIENT_PRIVATE_KEY is required but not set", func() {
		NewClient(Config{Auth0Domain: "auth.example.com", ClientID: "client-1", Audience: "itx"})
	})
}

const (
	linkVoteUID   = "8f3c2a1e-4b5d-4e6f-9a7b-1c2d3e4f5a6b"
	linkBallotUID = "cba14f40-1636-11ec-9621-0242ac130002"
)

type upstreamCall struct {
	method string
	path   string
	body   map[string]json.RawMessage
}

// TestVoteLinkContract drives personalized vote links through the generated transports,
// converters, service, real HMAC signer, and proxy HTTP serialization: bulk resend emits a
// recipient_links map whose token the link endpoints accept, and the link endpoints send
// ITX the ballot from the token.
func TestVoteLinkContract(t *testing.T) {
	signer, err := votelink.NewHMACSigner(map[string][]byte{"k1": []byte(strings.Repeat("s", votelink.MinKeyLength))}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	wire := make(chan upstreamCall, 1)
	mux := voteLinkContractMux(t, wire, signer)

	serve := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	next := func(t *testing.T) upstreamCall {
		t.Helper()
		select {
		case call := <-wire:
			return call
		default:
			t.Fatal("expected an upstream HTTP request")
			return upstreamCall{}
		}
	}

	t.Run("bulk resend without personalized links is unchanged", func(t *testing.T) {
		rec := serve(http.MethodPost, "/votes/"+linkVoteUID+"/bulk_resend", `{"recipient_ids":["`+linkBallotUID+`"]}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.path != "/v2/voting/poll/"+linkVoteUID+"/bulk_resend" {
			t.Errorf("path = %s", call.path)
		}
		if _, present := call.body["recipient_links"]; present {
			t.Errorf("recipient_links must be absent, got %s", call.body["recipient_links"])
		}
	})

	var token string
	t.Run("bulk resend with personalized links", func(t *testing.T) {
		rec := serve(http.MethodPost, "/votes/"+linkVoteUID+"/bulk_resend",
			`{"recipient_ids":["`+linkBallotUID+`"],"personalized_links":true}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var links map[string]string
		if err := json.Unmarshal(next(t).body["recipient_links"], &links); err != nil {
			t.Fatalf("decode recipient_links: %v", err)
		}
		link, err := url.Parse(links[linkBallotUID])
		if err != nil {
			t.Fatalf("parse link %q: %v", links[linkBallotUID], err)
		}
		if link.Scheme != "https" || link.Host != "app.lfx.dev" || link.Path != "/votes/"+linkVoteUID {
			t.Errorf("link = %s", link)
		}
		if link.RawQuery != "" {
			t.Errorf("token must not be in the query string: %s", link)
		}
		fragment, err := url.ParseQuery(link.Fragment)
		if err != nil {
			t.Fatalf("parse fragment: %v", err)
		}
		token = fragment.Get("vote_token")
		claims, err := signer.Verify(token)
		if err != nil {
			t.Fatalf("emailed token does not verify: %v", err)
		}
		if claims.VoteUID != linkVoteUID || claims.VoteResponseUID != linkBallotUID {
			t.Errorf("claims = %+v", claims)
		}
	})

	t.Run("submit by link sends the token's ballot", func(t *testing.T) {
		body := `{"vote_token":"` + token + `","abstain":true,"vote_response_uid":"ignored"}`
		rec := serve(http.MethodPost, "/vote_links/responses", body)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.method != http.MethodPost || call.path != "/v2/voting/vote" {
			t.Errorf("upstream = %s %s", call.method, call.path)
		}
		if string(call.body["vote_id"]) != `"`+linkBallotUID+`"` {
			t.Errorf("vote_id = %s", call.body["vote_id"])
		}
	})

	t.Run("update by link sends the token's ballot", func(t *testing.T) {
		rec := serve(http.MethodPut, "/vote_links/responses", `{"vote_token":"`+token+`","abstain":true}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		call := next(t)
		if call.method != http.MethodPut || call.path != "/v2/voting/vote/"+linkBallotUID {
			t.Errorf("upstream = %s %s", call.method, call.path)
		}
		if _, present := call.body["comment_responses"]; present {
			t.Error("omitted comment_responses must stay omitted so ITX preserves existing comments")
		}
	})

	t.Run("update by link on a locked vote returns ITX's 409", func(t *testing.T) {
		upstream, paths := lockedBallotUpstream(t)
		client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
		responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
			service.WithVoteLinkVerifier(signer))
		updateByLink := func(ctx context.Context, payload any) (any, error) {
			p := payload.(*votesvc.UpdateVoteResponseByLinkPayload)
			req, err := converters.ConvertUpdateVoteResponseByLinkPayloadToDomain(p)
			if err != nil {
				return nil, mirrorHandleError(err)
			}
			return nil, mirrorHandleError(responses.UpdateVoteResponseByLink(ctx, p.VoteToken, req))
		}
		lockedMux := goahttp.NewMuxer()
		voteserver.MountUpdateVoteResponseByLinkHandler(lockedMux, voteserver.NewUpdateVoteResponseByLinkHandler(updateByLink, lockedMux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

		req := httptest.NewRequest(http.MethodPut, "/vote_links/responses", strings.NewReader(`{"vote_token":"`+token+`","abstain":true}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		lockedMux.ServeHTTP(rec, req)

		assertLockedBallotConflict(t, rec, *paths, http.MethodPut+" /v2/voting/vote/"+linkBallotUID)
	})

	t.Run("tampered token is rejected before ITX", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			rec := serve(method, "/vote_links/responses", `{"vote_token":"`+token+`x","abstain":true}`)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s status = %d, want 404: %s", method, rec.Code, rec.Body)
			}
		}
		select {
		case call := <-wire:
			t.Fatalf("rejected request reached ITX: %+v", call)
		default:
		}
	})

	t.Run("missing token fails validation", func(t *testing.T) {
		rec := serve(http.MethodPost, "/vote_links/responses", `{"abstain":true}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
	})
}

func voteLinkContractMux(t *testing.T, wire chan<- upstreamCall, signer *votelink.HMACSigner) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wire <- upstreamCall{method: r.Method, path: r.URL.Path, body: body}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	voting := service.NewVoteService(nil, client, methodContractIDMapper{}, logger,
		service.WithVoteLinks(signer, "https://app.lfx.dev"))
	responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, logger,
		service.WithVoteLinkVerifier(signer))

	bulkResend := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.BulkResendVotePayload)
		return nil, voting.BulkResendVote(ctx, p.UID, p.RecipientIds, p.PersonalizedLinks)
	}
	createByLink := func(ctx context.Context, payload any) (any, error) {
		p := payload.(*votesvc.CreateVoteResponseByLinkPayload)
		req, err := converters.ConvertCreateVoteResponseByLinkPayloadToDomain(p)
		if err != nil {
			return nil, err
		}
		return nil, responses.CreateVoteResponseByLink(ctx, p.VoteToken, req)
	}
	updateByLink := func(ctx context.Context, payload any) (any, error) {
		p := payload.(*votesvc.UpdateVoteResponseByLinkPayload)
		req, err := converters.ConvertUpdateVoteResponseByLinkPayloadToDomain(p)
		if err != nil {
			return nil, err
		}
		return nil, responses.UpdateVoteResponseByLink(ctx, p.VoteToken, req)
	}

	// Map domain errors to statuses the way cmd/voting-api handleError does; leave Goa's
	// own decode/validation errors to the default formatter.
	formatter := func(ctx context.Context, err error) goahttp.Statuser {
		var dErr *domain.DomainError
		if errors.As(err, &dErr) {
			return domainStatus{err: dErr}
		}
		return goahttp.NewErrorResponse(ctx, err)
	}

	mux := goahttp.NewMuxer()
	voteserver.MountBulkResendVoteHandler(mux, voteserver.NewBulkResendVoteHandler(bulkResend, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	voteserver.MountCreateVoteResponseByLinkHandler(mux, voteserver.NewCreateVoteResponseByLinkHandler(createByLink, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	voteserver.MountUpdateVoteResponseByLinkHandler(mux, voteserver.NewUpdateVoteResponseByLinkHandler(updateByLink, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
	return mux
}

// domainStatus renders a domain error with the HTTP status handleError would use.
type domainStatus struct {
	err *domain.DomainError
}

func (d domainStatus) StatusCode() int {
	switch d.err.Type {
	case domain.ErrorTypeValidation:
		return http.StatusBadRequest
	case domain.ErrorTypeNotFound:
		return http.StatusNotFound
	case domain.ErrorTypeConflict:
		return http.StatusConflict
	case domain.ErrorTypeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// TestVoteLinkBallotContract drives POST /vote_links/ballot through the generated transport,
// service, real HMAC signer and proxy client against a fake ITX: the token's ids select the
// ballot and the vote, the response carries only ballot fields plus the project's UID and slug,
// and pseudo-anonymous votes never return ballot contents.
func TestVoteLinkBallotContract(t *testing.T) {
	signer, err := votelink.NewHMACSigner(map[string][]byte{"k1": []byte(strings.Repeat("s", votelink.MinKeyLength))}, "k1")
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(linkVoteUID, linkBallotUID)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		pseudoField string // JSON fragment for pseudo_anonymity, "" = omitted
		wantAnswers bool
	}{
		{name: "regular vote", pseudoField: `"pseudo_anonymity":false,`, wantAnswers: true},
		{name: "pseudo-anonymous vote", pseudoField: `"pseudo_anonymity":true,`},
		{name: "pseudo_anonymity missing fails closed", pseudoField: ``},
		{name: "pseudo_anonymity null fails closed", pseudoField: `"pseudo_anonymity":null,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.Method+" "+r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/voting/vote/" + linkBallotUID:
					_, _ = io.WriteString(w, `{"vote_id":"`+linkBallotUID+`","poll_id":"`+linkVoteUID+`","vote_status":"responded","abstained":false,"user_email":"voter@example.org","voter_removed":false,"allow_vote_change":false,"can_change_vote":false,`+
						`"poll_answers":[{"question_id":"q1","prompt":"Who?","type":"single_choice","user_choice":[{"choice_id":"c-a","choice_text":"Alice"}]}],"comment_responses":[]}`)
				case "/v2/voting/poll/" + linkVoteUID:
					_, _ = io.WriteString(w, `{"poll_id":"`+linkVoteUID+`","name":"TSC election","description":"Pick one","status":"active","end_time":"2030-01-01T00:00:00Z",`+
						`"project_id":"a0941000002wBz4AAE","project_name":"cncf","committee_id":"c1","total_voting_request_invitations":9,"num_response_received":4,`+tc.pseudoField+
						`"allow_abstain":true,"allow_vote_change":false,"poll_type":"generic","poll_questions":[{"question_id":"q1","prompt":"Who?","type":"single_choice","choices":[{"choice_id":"c-a","choice_text":"Alice"}]}]}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer upstream.Close()

			client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
			responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
				service.WithVoteLinkVerifier(signer), service.WithPollReader(client))
			getBallot := func(ctx context.Context, payload any) (any, error) {
				b, err := responses.GetBallotByLink(ctx, payload.(*votesvc.GetVoteBallotByLinkPayload).VoteToken)
				if err != nil {
					return nil, err
				}
				return converters.ConvertVoteLinkBallotToResult(b), nil
			}
			formatter := func(ctx context.Context, err error) goahttp.Statuser {
				var dErr *domain.DomainError
				if errors.As(err, &dErr) {
					return domainStatus{err: dErr}
				}
				return goahttp.NewErrorResponse(ctx, err)
			}
			mux := goahttp.NewMuxer()
			voteserver.MountGetVoteBallotByLinkHandler(mux, voteserver.NewGetVoteBallotByLinkHandler(getBallot, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))

			req := httptest.NewRequest(http.MethodPost, "/vote_links/ballot", strings.NewReader(`{"vote_token":"`+token+`"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if len(paths) != 2 || paths[0] != "GET /v2/voting/vote/"+linkBallotUID || paths[1] != "GET /v2/voting/poll/"+linkVoteUID {
				t.Errorf("upstream calls = %v", paths)
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"vote_uid", "project_uid", "project_slug", "name", "status", "poll_questions", "ballot"} {
				if _, ok := body[key]; !ok {
					t.Errorf("missing %s: %s", key, rec.Body)
				}
			}
			// methodContractIDMapper passes IDs through, so project_uid echoes the poll's project_id.
			if string(body["project_uid"]) != `"a0941000002wBz4AAE"` || string(body["project_slug"]) != `"cncf"` {
				t.Errorf("project_uid = %s, project_slug = %s", body["project_uid"], body["project_slug"])
			}
			for _, leak := range []string{"project_name", "committee", "num_response_received", "total_voting_request_invitations", "voter@example.org", linkBallotUID} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("response leaks %q: %s", leak, rec.Body)
				}
			}
			var ballot map[string]json.RawMessage
			if err := json.Unmarshal(body["ballot"], &ballot); err != nil {
				t.Fatal(err)
			}
			if string(ballot["already_voted"]) != "true" {
				t.Errorf("already_voted = %s", ballot["already_voted"])
			}
			// The fixture ballot is responded, so it cannot end the vote; only the yes/no is
			// returned, never the counts it is computed from.
			if string(ballot["would_end_vote"]) != "false" {
				t.Errorf("would_end_vote = %s, want false", ballot["would_end_vote"])
			}
			// The setting and the ballot's finality are not ballot contents: both are returned
			// on regular and pseudo-anonymous votes alike.
			if string(body["allow_vote_change"]) != "false" || string(ballot["can_change_vote"]) != "false" {
				t.Errorf("allow_vote_change = %s, ballot.can_change_vote = %s, want false/false", body["allow_vote_change"], ballot["can_change_vote"])
			}
			_, hasAnswers := ballot["poll_answers"]
			if hasAnswers != tc.wantAnswers {
				t.Errorf("poll_answers present = %v, want %v: %s", hasAnswers, tc.wantAnswers, body["ballot"])
			}
			if wantPseudo := strconv.FormatBool(!tc.wantAnswers); string(body["pseudo_anonymity"]) != wantPseudo {
				t.Errorf("pseudo_anonymity = %s, want %s", body["pseudo_anonymity"], wantPseudo)
			}
		})
	}

	t.Run("tampered token is rejected before ITX", func(t *testing.T) {
		called := false
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		defer upstream.Close()
		client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
		responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
			service.WithVoteLinkVerifier(signer), service.WithPollReader(client))
		_, err := responses.GetBallotByLink(context.Background(), token+"x")
		var dErr *domain.DomainError
		if !errors.As(err, &dErr) || dErr.Type != domain.ErrorTypeNotFound || called {
			t.Errorf("err = %v, upstream called = %v", err, called)
		}
	})
}

// TestLastVoterBallotContract checks both ballot reads against ITX-shaped responses.
// The states mirror itx-service-voting's submission and membership-change tests; this
// exercises decoding, the service rule, converters and generated HTTP serialization,
// not real DynamoDB consistency or the ITX write path.
func TestLastVoterBallotContract(t *testing.T) {
	signer, err := votelink.NewHMACSigner(map[string][]byte{"k1": []byte(strings.Repeat("s", votelink.MinKeyLength))}, "k1")
	require.NoError(t, err)
	token, err := signer.Sign(linkVoteUID, linkBallotUID)
	require.NoError(t, err)

	cases := []struct {
		name       string
		voteStatus string
		pollStatus string
		received   int
		total      int
		removed    bool
		pollFails  bool
		want       bool
	}{
		{name: "last outstanding voter", voteStatus: "awaiting_response", pollStatus: "active", received: 1, total: 2, want: true},
		{name: "multiple outstanding voters", voteStatus: "awaiting_response", pollStatus: "active", received: 1, total: 3},
		{name: "remaining voter after an answered voter was removed", voteStatus: "awaiting_response", pollStatus: "active", received: 0, total: 1, want: true},
		{name: "answered voter was re-added", voteStatus: "responded", pollStatus: "active", received: 2, total: 3},
		{name: "removed voter", voteStatus: "awaiting_response", pollStatus: "active", received: 1, total: 2, removed: true},
		{name: "ended vote", voteStatus: "awaiting_response_but_poll_ended", pollStatus: "ended", received: 1, total: 2},
		{name: "poll ended between ballot and poll reads", voteStatus: "awaiting_response", pollStatus: "ended", received: 1, total: 2},
		{name: "disabled vote", voteStatus: "awaiting_response", pollStatus: "disabled", received: 1, total: 2},
		{name: "no invitations", voteStatus: "awaiting_response", pollStatus: "active"},
		{name: "poll unavailable", voteStatus: "awaiting_response", pollFails: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, byLink := range []bool{false, true} {
				t.Run(map[bool]string{false: "authenticated", true: "vote link"}[byLink], func(t *testing.T) {
					var paths []string
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						paths = append(paths, r.Method+" "+r.URL.Path)
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/v2/voting/vote/" + linkBallotUID:
							_, _ = io.WriteString(w, `{"vote_id":"`+linkBallotUID+`","poll_id":"`+linkVoteUID+`","project_id":"synthetic-project","vote_status":"`+tc.voteStatus+`","voter_removed":`+strconv.FormatBool(tc.removed)+`,"comment_responses":[]}`)
						case "/v2/voting/poll/" + linkVoteUID:
							if tc.pollFails {
								w.WriteHeader(http.StatusServiceUnavailable)
								_, _ = io.WriteString(w, `{"message":"poll unavailable"}`)
								return
							}
							_, _ = io.WriteString(w, `{"poll_id":"`+linkVoteUID+`","project_id":"synthetic-project","name":"Synthetic election","description":"Contract fixture","status":"`+tc.pollStatus+`","end_time":"2099-01-01T00:00:00Z","pseudo_anonymity":true,"total_voting_request_invitations":`+strconv.Itoa(tc.total)+`,"num_response_received":`+strconv.Itoa(tc.received)+`,"poll_questions":[]}`)
						default:
							w.WriteHeader(http.StatusNotFound)
						}
					}))
					t.Cleanup(upstream.Close)
					client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
					responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
						service.WithPollReader(client), service.WithVoteLinkVerifier(signer))
					getResponse := func(ctx context.Context, payload any) (any, error) {
						if byLink {
							ballot, err := responses.GetBallotByLink(ctx, payload.(*votesvc.GetVoteBallotByLinkPayload).VoteToken)
							if err != nil {
								return nil, err
							}
							return converters.ConvertVoteLinkBallotToResult(ballot), nil
						}
						ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
						details, err := responses.GetVoteResponse(ctx, payload.(*votesvc.GetVoteResponsePayload).VoteResponseUID)
						if err != nil {
							return nil, err
						}
						return converters.ConvertVoteResponseDetailsToResult(details), nil
					}
					formatter := func(ctx context.Context, err error) goahttp.Statuser {
						var dErr *domain.DomainError
						if errors.As(err, &dErr) {
							return domainStatus{err: dErr}
						}
						return goahttp.NewErrorResponse(ctx, err)
					}
					mux := goahttp.NewMuxer()
					req := httptest.NewRequest(http.MethodGet, "/vote_responses/"+linkBallotUID, nil)
					if byLink {
						voteserver.MountGetVoteBallotByLinkHandler(mux, voteserver.NewGetVoteBallotByLinkHandler(getResponse, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
						req = httptest.NewRequest(http.MethodPost, "/vote_links/ballot", strings.NewReader(`{"vote_token":"`+token+`"}`))
						req.Header.Set("Content-Type", "application/json")
					} else {
						voteserver.MountGetVoteResponseHandler(mux, voteserver.NewGetVoteResponseHandler(getResponse, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, formatter))
					}
					rec := httptest.NewRecorder()
					mux.ServeHTTP(rec, req)

					wantPaths := []string{"GET /v2/voting/vote/" + linkBallotUID}
					if byLink || (tc.voteStatus == "awaiting_response" && !tc.removed) {
						wantPaths = append(wantPaths, "GET /v2/voting/poll/"+linkVoteUID)
					}
					require.Equal(t, wantPaths, paths)
					if tc.pollFails {
						require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
						return
					}
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
					var body map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
					if byLink {
						for _, key := range []string{"num_response_received", "total_voting_request_invitations", "poll_answers", "comment_responses"} {
							require.NotContains(t, rec.Body.String(), `"`+key+`"`, "vote links must not expose counts or pseudo-anonymous ballot contents")
						}
						require.NoError(t, json.Unmarshal(body["ballot"], &body))
					}
					require.Equal(t, strconv.FormatBool(tc.want), string(body["would_end_vote"]), "the boolean must always be serialized")
				})
			}
		})
	}
}

// TestVoteMethodContract protects omission and explicit intent across the generated
// transports, converters, service and actual proxy HTTP serialization. The sink
// captures requests only; it does not model ITX persistence or preservation.
func TestVoteMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const bodyPrefix = `{"name":"Synthetic method contract","description":"Updated description","end_time":"2030-02-15T23:59:59Z","end_time_timezone":"UTC","project_uid":"synthetic-project","committee_uid":"` + uid + `","poll_questions":[{"prompt":"Rank candidates","type":"multiple_choice","choices":[{"choice_text":"Candidate A"},{"choice_text":"Candidate B"},{"choice_text":"Candidate C"},{"choice_text":"Candidate D"},{"choice_text":"Candidate E"}]}]`
	tests := []struct {
		name        string
		method      string
		extra       string
		wantType    string
		wantWinners string
		wantChange  string
		invalid     string
	}{
		{name: "update omitted", method: http.MethodPut},
		{name: "update null", method: http.MethodPut, extra: `,"poll_type":null,"num_winners":null`},
		{name: "update generic", method: http.MethodPut, extra: `,"poll_type":"generic"`, wantType: `"generic"`},
		{name: "update condorcet", method: http.MethodPut, extra: `,"poll_type":"condorcet_irv"`, wantType: `"condorcet_irv"`},
		{name: "update instant runoff", method: http.MethodPut, extra: `,"poll_type":"instant_runoff_vote"`, wantType: `"instant_runoff_vote"`},
		{name: "update STV four", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":4`, wantType: `"meek_stv"`, wantWinners: "4"},
		{name: "update STV omitted count", method: http.MethodPut, extra: `,"poll_type":"meek_stv"`, wantType: `"meek_stv"`},
		{name: "update count without type", method: http.MethodPut, extra: `,"num_winners":3`, wantWinners: "3"},
		{name: "update empty type", method: http.MethodPut, extra: `,"poll_type":""`, invalid: "poll_type"},
		{name: "update unknown type", method: http.MethodPut, extra: `,"poll_type":"unknown"`, invalid: "poll_type"},
		{name: "update zero count", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":0`, invalid: "num_winners"},
		{name: "update one count", method: http.MethodPut, extra: `,"poll_type":"meek_stv","num_winners":1`, invalid: "num_winners"},
		{name: "update count above maximum", method: http.MethodPut, extra: `,"num_winners":11`, invalid: "num_winners"},
		{name: "update allow_vote_change null", method: http.MethodPut, extra: `,"allow_vote_change":null`},
		{name: "update allow_vote_change false", method: http.MethodPut, extra: `,"allow_vote_change":false`, wantChange: "false"},
		{name: "update allow_vote_change true", method: http.MethodPut, extra: `,"allow_vote_change":true`, wantChange: "true"},
		{name: "create omitted", method: http.MethodPost, wantType: `"generic"`},
		{name: "create generic omitted count", method: http.MethodPost, extra: `,"poll_type":"generic"`, wantType: `"generic"`},
		{name: "create condorcet omitted count", method: http.MethodPost, extra: `,"poll_type":"condorcet_irv"`, wantType: `"condorcet_irv"`},
		{name: "create instant runoff omitted count", method: http.MethodPost, extra: `,"poll_type":"instant_runoff_vote"`, wantType: `"instant_runoff_vote"`},
		{name: "create generic explicit count", method: http.MethodPost, extra: `,"poll_type":"generic","num_winners":4`, wantType: `"generic"`, wantWinners: "4"},
		{name: "create STV omitted count", method: http.MethodPost, extra: `,"poll_type":"meek_stv"`, wantType: `"meek_stv"`, wantWinners: "2"},
		{name: "create STV explicit count", method: http.MethodPost, extra: `,"poll_type":"meek_stv","num_winners":4`, wantType: `"meek_stv"`, wantWinners: "4"},
		{name: "create allow_vote_change false", method: http.MethodPost, extra: `,"allow_vote_change":false`, wantType: `"generic"`, wantChange: "false"},
		{name: "create allow_vote_change true", method: http.MethodPost, extra: `,"allow_vote_change":true`, wantType: `"generic"`, wantChange: "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := make(chan map[string]json.RawMessage, 1)
			mux := voteMethodContractMux(t, wire)
			body := bodyPrefix + tt.extra + "}"
			path := "/votes"
			wantStatus := http.StatusCreated
			if tt.method == http.MethodPut {
				path += "/" + uid
				wantStatus = http.StatusOK
			}
			if tt.invalid != "" {
				wantStatus = http.StatusBadRequest
			}
			request := httptest.NewRequest(tt.method, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != wantStatus {
				t.Fatalf("HTTP status: want %d, got %d: %s", wantStatus, response.Code, response.Body)
			}
			if tt.invalid != "" {
				if !strings.Contains(response.Body.String(), tt.invalid) {
					t.Fatalf("expected %s validation error: %s", tt.invalid, response.Body)
				}
				select {
				case got := <-wire:
					t.Fatalf("invalid request reached ITX: %s", got)
				default:
				}
				return
			}
			assertWire := func(t *testing.T) {
				t.Helper()
				select {
				case got := <-wire:
					for key, want := range map[string]string{"poll_type": tt.wantType, "num_winners": tt.wantWinners, "allow_vote_change": tt.wantChange} {
						value, present := got[key]
						if want == "" && present {
							t.Errorf("%s must be absent, got %s", key, value)
						} else if want != "" && (!present || string(value) != want) {
							t.Errorf("%s: want %s, got %s", key, want, value)
						}
					}
				default:
					t.Fatal("expected an upstream HTTP request")
				}
			}
			assertWire(t)
			// The fake ITX response carries no allow_vote_change, like a legacy poll: the
			// result must still report the resolved setting.
			var result map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if string(result["allow_vote_change"]) != "true" {
				t.Errorf("allow_vote_change: want true for a legacy poll, got %s", result["allow_vote_change"])
			}
			if tt.method == http.MethodPut {
				t.Run("generated CLI and client body", func(t *testing.T) {
					payload, err := voteclient.BuildUpdateVotePayload(body, uid, "")
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(voteclient.NewUpdateVoteRequestBody(payload))
					if err != nil {
						t.Fatal(err)
					}
					request := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(encoded))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, request)
					if response.Code != http.StatusOK {
						t.Fatalf("generated client HTTP status: %d: %s", response.Code, response.Body)
					}
					assertWire(t)
				})
			}
		})
	}
}

// TestEndVoteMethodContract drives POST /votes/{uid}/end through the generated server
// transport, converters and service to the proxy's actual HTTP serialization. The sink
// answers GetPoll and the bodiless end request; it does not model ITX persistence.
func TestEndVoteMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const endedBody = `{"poll_id":"` + uid + `","name":"Synthetic end contract","status":"ended","end_time":"2030-03-01T23:59:59Z","early_end_time":"2030-02-20T09:00:00Z","project_id":"synthetic-project","committee_id":"` + uid + `"}`

	type upstreamCall struct{ method, path, body string }
	newMux := func(t *testing.T, currentStatus string) (http.Handler, *[]upstreamCall) {
		t.Helper()
		var calls []upstreamCall
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			calls = append(calls, upstreamCall{r.Method, r.URL.Path, string(body)})
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `{"poll_id":"`+uid+`","status":"`+currentStatus+`"}`)
				return
			}
			_, _ = io.WriteString(w, endedBody)
		}))
		t.Cleanup(upstream.Close)
		client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
		voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mux := goahttp.NewMuxer()
		end := func(ctx context.Context, payload any) (any, error) {
			ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
			poll, err := voting.EndVote(ctx, payload.(*votesvc.EndVotePayload).UID)
			if err != nil {
				// Mirror cmd/voting-api handleError: a domain conflict becomes the Goa ConflictError (409).
				if domain.GetErrorType(err) == domain.ErrorTypeConflict {
					return nil, &votesvc.ConflictError{Code: "409", Message: err.Error()}
				}
				return nil, err
			}
			return converters.ConvertPollResponseToVoteResult(poll), nil
		}
		voteserver.MountEndVoteHandler(mux, voteserver.NewEndVoteHandler(end, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
		return mux, &calls
	}

	t.Run("active vote returns the ended vote", func(t *testing.T) {
		mux, calls := newMux(t, "active")
		request := httptest.NewRequest(http.MethodPost, "/votes/"+uid+"/end", nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP status: want 200, got %d: %s", response.Code, response.Body)
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if result["status"] != "ended" || result["early_end_time"] != "2030-02-20T09:00:00Z" || result["end_time"] != "2030-03-01T23:59:59Z" {
			t.Fatalf("unexpected response shape: %s", response.Body)
		}
		want := []upstreamCall{
			{http.MethodGet, "/v2/voting/poll/" + uid, ""},
			{http.MethodPost, "/v2/voting/poll/" + uid + "/end", ""},
		}
		if len(*calls) != len(want) {
			t.Fatalf("upstream calls: want %v, got %v", want, *calls)
		}
		for i := range want {
			if (*calls)[i] != want[i] {
				t.Errorf("upstream call %d: want %v, got %v", i, want[i], (*calls)[i])
			}
		}
	})

	t.Run("disabled vote returns 409 without calling ITX end", func(t *testing.T) {
		mux, calls := newMux(t, "disabled")
		request := httptest.NewRequest(http.MethodPost, "/votes/"+uid+"/end", nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("HTTP status: want 409, got %d: %s", response.Code, response.Body)
		}
		for _, call := range *calls {
			if call.method == http.MethodPost {
				t.Fatalf("disabled vote reached ITX end: %v", call)
			}
		}
	})
}

// lockedBallotMessage is the ITX message for a change to a submitted ballot on a poll whose
// allow_vote_change is false.
const lockedBallotMessage = "this poll does not allow changing a submitted vote"

// lockedBallotUpstream fakes ITX rejecting every ballot update with its 409 body and records
// the request paths it received.
func lockedBallotUpstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"409","message":"`+lockedBallotMessage+`"}`)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &paths
}

// mirrorHandleError mirrors cmd/voting-api handleError for the conflict path: a domain
// conflict becomes the Goa ConflictError, which the generated encoder renders only when the
// method declares a Conflict response.
func mirrorHandleError(err error) error {
	if domain.GetErrorType(err) == domain.ErrorTypeConflict {
		return &votesvc.ConflictError{Code: "409", Message: err.Error()}
	}
	return err
}

// assertLockedBallotConflict checks that a ballot update rejected by ITX reached ITX once and
// was returned to the client as 409 with ITX's message.
func assertLockedBallotConflict(t *testing.T, response *httptest.ResponseRecorder, paths []string, wantPath string) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("HTTP status: want 409, got %d: %s", response.Code, response.Body)
	}
	var body struct{ Code, Message string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v: %s", err, response.Body)
	}
	if body.Code != "409" || body.Message != lockedBallotMessage {
		t.Errorf("error body: want code 409 and ITX's message, got %s", response.Body)
	}
	if len(paths) != 1 || paths[0] != wantPath {
		t.Errorf("upstream calls: want [%s], got %v", wantPath, paths)
	}
}

// TestUpdateVoteResponseConflictContract drives PUT /vote_responses/{uid} through the generated
// server transport (with the production nil formatter), converters and service to the proxy's
// HTTP serialization. ITX's 409 for a locked ballot must reach the client as 409 with ITX's
// message rather than falling through to the default 500.
func TestUpdateVoteResponseConflictContract(t *testing.T) {
	const ballotUID = "b03cdbaf-53b1-4d47-bc04-dd7e459dd309"
	upstream, paths := lockedBallotUpstream(t)
	client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
	responses := service.NewVoteResponseService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	update := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.UpdateVoteResponsePayload)
		req, err := converters.ConvertUpdateVoteResponsePayloadToDomain(p)
		if err != nil {
			return nil, mirrorHandleError(err)
		}
		return nil, mirrorHandleError(responses.UpdateVoteResponse(ctx, p.VoteResponseUID, req))
	}
	mux := goahttp.NewMuxer()
	voteserver.MountUpdateVoteResponseHandler(mux, voteserver.NewUpdateVoteResponseHandler(update, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))

	request := httptest.NewRequest(http.MethodPut, "/vote_responses/"+ballotUID, strings.NewReader(`{"abstain":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	assertLockedBallotConflict(t, response, *paths, http.MethodPut+" /v2/voting/vote/"+ballotUID)
}

func voteMethodContractMux(t *testing.T, wire chan<- map[string]json.RawMessage) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wire <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"poll_id":"a02bdbaf-53b1-4d47-bc04-dd7e459dd308","name":"Synthetic method contract","description":"Wire response only","status":"disabled","project_id":"synthetic-project","committee_id":"a02bdbaf-53b1-4d47-bc04-dd7e459dd308"}`)
	}))
	t.Cleanup(upstream.Close)
	client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
	voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := goahttp.NewMuxer()
	create := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		poll, err := voting.CreateVote(ctx, converters.ConvertCreateVotePayloadToDomain(payload.(*votesvc.CreateVotePayload)))
		if err != nil {
			return nil, err
		}
		return converters.ConvertPollResponseToVoteResult(poll), nil
	}
	update := func(ctx context.Context, payload any) (any, error) {
		ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
		p := payload.(*votesvc.UpdateVotePayload)
		poll, err := voting.UpdateVote(ctx, p.UID, converters.ConvertUpdateVotePayloadToDomain(p))
		if err != nil {
			return nil, err
		}
		return converters.ConvertPollResponseToVoteResult(poll), nil
	}
	voteserver.MountCreateVoteHandler(mux, voteserver.NewCreateVoteHandler(create, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	voteserver.MountUpdateVoteHandler(mux, voteserver.NewUpdateVoteHandler(update, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	return mux
}

type methodContractIDMapper struct{}

func (methodContractIDMapper) MapProjectV2ToV1(_ context.Context, id string) (string, error) {
	return id, nil
}

func (methodContractIDMapper) MapProjectV1ToV2(_ context.Context, id string) (string, error) {
	return id, nil
}

func (methodContractIDMapper) MapCommitteeV2ToV1(_ context.Context, id string) (string, error) {
	return id, nil
}

func (methodContractIDMapper) MapCommitteeV1ToV2(_ context.Context, id string) (string, error) {
	return id, nil
}

// TestVoteResultsMethodContract drives stub ITX results JSON through the real proxy,
// VoteService.GetVoteResults, the results converter and the generated GET handler with the
// standard Goa encoder, then decodes the body with the generated client. The stub only
// answers the results request; it does not model ITX hold/unlock decisions.
func TestVoteResultsMethodContract(t *testing.T) {
	const uid = "a02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const voteID = "b02bdbaf-53b1-4d47-bc04-dd7e459dd308"
	const (
		prompt1   = "c02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		question1 = "d02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		question2 = "e02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceA   = "f02bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceB   = "f12bdbaf-53b1-4d47-bc04-dd7e459dd308"
		choiceX   = "f22bdbaf-53b1-4d47-bc04-dd7e459dd308"
	)

	serve := func(t *testing.T, upstreamBody string) *http.Response {
		t.Helper()
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v2/voting/poll/"+uid+"/results" {
				t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, upstreamBody)
		}))
		t.Cleanup(upstream.Close)
		client := NewContractTestClient(upstream.Client(), upstream.URL+"/")
		voting := service.NewVoteService(nil, client, methodContractIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		mux := goahttp.NewMuxer()
		get := func(ctx context.Context, payload any) (any, error) {
			ctx = context.WithValue(ctx, constants.PrincipalContextID, "synthetic-contract-user")
			results, err := voting.GetVoteResults(ctx, payload.(*votesvc.GetVoteResultsPayload).UID)
			if err != nil {
				return nil, err
			}
			return converters.ConvertVoteResultsToResult(results), nil
		}
		voteserver.MountGetVoteResultsHandler(mux, voteserver.NewGetVoteResultsHandler(get, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/votes/"+uid+"/results", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP status: want 200, got %d: %s", response.Code, response.Body)
		}
		return response.Result()
	}
	decodeMap := func(t *testing.T, response *http.Response) map[string]json.RawMessage {
		t.Helper()
		var body map[string]json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body
	}
	keys := func(m map[string]json.RawMessage) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	decodeClient := func(t *testing.T, upstreamBody string) *votesvc.VoteResultsResult {
		t.Helper()
		response := serve(t, upstreamBody)
		got, err := voteclient.DecodeGetVoteResultsResponse(goahttp.ResponseDecoder, false)(response)
		if err != nil {
			t.Fatalf("generated client rejected the response: %v", err)
		}
		return got.(*votesvc.VoteResultsResult)
	}

	t.Run("held response returns only the flag, participation and deadline", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z","poll_end_time_timezone":"America/New_York"}`))
		want := []string{"num_recipients", "num_votes_cast", "poll_end_time", "poll_end_time_timezone", "results_held"}
		if got := keys(body); !reflect.DeepEqual(got, want) {
			t.Fatalf("held keys: want %v, got %v", want, got)
		}
		if string(body["results_held"]) != "true" {
			t.Errorf("results_held: %s", body["results_held"])
		}
		got := decodeClient(t, `{"results_held":true,"num_recipients":10,"num_votes_cast":4,"poll_end_time":"2030-01-01T00:00:00Z"}`)
		if got.ResultsHeld == nil || !*got.ResultsHeld || got.NumAbstained != nil || got.PollResults != nil || got.CommentResults != nil {
			t.Errorf("generated client lost the held shape: %+v", got)
		}
	})

	t.Run("held response without a timezone omits the timezone key", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":true,"num_recipients":3,"num_votes_cast":1,"poll_end_time":"2030-01-01T00:00:00Z"}`))
		if _, present := body["poll_end_time_timezone"]; present {
			t.Errorf("timezone must be absent: %s", body["poll_end_time_timezone"])
		}
	})

	t.Run("final anonymous comments are text-only, duplicated and ordered", func(t *testing.T) {
		upstream := `{"num_recipients":5,"num_votes_cast":3,"num_abstained":1,"poll_end_time":"2030-01-01T00:00:00Z","poll_results":[],"comment_results":[{"prompt":{"prompt_id":"` + prompt1 + `","prompt":"Any comments?"},"responses":[{"comment_text":"zeta"},{"comment_text":"alpha"},{"comment_text":"alpha"}]}]}`
		body := decodeMap(t, serve(t, upstream))
		if _, present := body["results_held"]; present {
			t.Errorf("missing results_held must stay absent: %s", body["results_held"])
		}
		if _, present := body["poll_results"]; present {
			t.Errorf("empty optional poll_results is omitted under existing Goa behavior: %s", body["poll_results"])
		}
		var comments []struct {
			Responses []map[string]json.RawMessage `json:"responses"`
		}
		if err := json.Unmarshal(body["comment_results"], &comments); err != nil {
			t.Fatalf("decode comment_results: %v", err)
		}
		responses := comments[0].Responses
		if len(responses) != 3 {
			t.Fatalf("want 3 responses, got %d", len(responses))
		}
		for i, text := range []string{`"zeta"`, `"alpha"`, `"alpha"`} {
			if got := keys(responses[i]); !reflect.DeepEqual(got, []string{"comment_text"}) {
				t.Errorf("response %d keys: want [comment_text], got %v", i, got)
			}
			if string(responses[i]["comment_text"]) != text {
				t.Errorf("response %d text: want %s, got %s", i, text, responses[i]["comment_text"])
			}
		}
		got := decodeClient(t, upstream)
		if got.NumAbstained == nil || *got.NumAbstained != 1 {
			t.Errorf("num_abstained lost: %+v", got.NumAbstained)
		}
		for i, r := range got.CommentResults[0].Responses {
			if r.VoteID != nil || r.VoteCreationTime != nil || r.Abstained != nil {
				t.Errorf("generated client accepted but invented metadata for response %d: %+v", i, r)
			}
		}
	})

	t.Run("explicit false and zero survive the encoder", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"results_held":false,"num_recipients":2,"num_votes_cast":2,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z"}`))
		if string(body["results_held"]) != "false" || string(body["num_abstained"]) != "0" {
			t.Errorf("results_held=%s num_abstained=%s", body["results_held"], body["num_abstained"])
		}
	})

	t.Run("named results keep totals, ids, times, zero counts and both abstained values", func(t *testing.T) {
		upstream := `{"num_recipients":4,"num_votes_cast":2,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z",` +
			`"poll_results":[` +
			`{"question":{"question_id":"` + question1 + `","prompt":"Pick","type":"multiple_choice"},"generic_choice_votes":[{"choice_id":"` + choiceB + `","vote_count":0,"percentage":0},{"choice_id":"` + choiceA + `","vote_count":2,"percentage":100}]},` +
			`{"question":{"question_id":"` + question2 + `","prompt":"Rank","type":"multiple_choice"},"ranked_choice_votes":[{"choice_id":"` + choiceX + `","rank_counts":[{"rank":1,"count":2}]}]}],` +
			`"comment_results":[{"prompt":{"prompt_id":"` + prompt1 + `","prompt":"Why?"},"responses":[` +
			`{"vote_id":"` + voteID + `","user_id":"u1","comment_text":"a","vote_creation_time":"2030-01-01T00:00:00Z","abstained":false},` +
			`{"vote_id":"` + uid + `","comment_text":"b","vote_creation_time":"2030-01-02T00:00:00Z","abstained":true}]}]}`
		got := decodeClient(t, upstream)
		if got.NumAbstained == nil || *got.NumAbstained != 0 {
			t.Errorf("num_abstained: %+v", got.NumAbstained)
		}
		if len(got.PollResults) != 2 {
			t.Fatalf("poll_results: %+v", got.PollResults)
		}
		generic := got.PollResults[0].GenericChoiceVotes
		if len(generic) != 2 || generic[0].ChoiceID != choiceB || generic[0].VoteCount != 0 || generic[1].ChoiceID != choiceA || generic[1].VoteCount != 2 {
			t.Errorf("generic totals or order changed: %+v", generic)
		}
		if ranked := got.PollResults[1].RankedChoiceVotes; len(ranked) != 1 || ranked[0].RankCounts[0].Count != 2 {
			t.Errorf("ranked totals changed: %+v", ranked)
		}
		responses := got.CommentResults[0].Responses
		if len(responses) != 2 || responses[0].VoteID == nil || *responses[0].VoteID != voteID || responses[0].VoteCreationTime == nil {
			t.Fatalf("named metadata lost: %+v", responses)
		}
		if responses[0].Abstained == nil || *responses[0].Abstained || responses[1].Abstained == nil || !*responses[1].Abstained {
			t.Errorf("abstained values changed: %+v %+v", responses[0].Abstained, responses[1].Abstained)
		}
	})

	t.Run("a prompt with no responses keeps a required empty array", func(t *testing.T) {
		body := decodeMap(t, serve(t, `{"num_recipients":1,"num_votes_cast":0,"num_abstained":0,"poll_end_time":"2030-01-01T00:00:00Z","comment_results":[{"prompt":{"prompt_id":"c1","prompt":"Why?"},"responses":[]}]}`))
		var comments []map[string]json.RawMessage
		if err := json.Unmarshal(body["comment_results"], &comments); err != nil {
			t.Fatalf("decode comment_results: %v", err)
		}
		if string(comments[0]["responses"]) != "[]" {
			t.Errorf("responses: want [], got %s", comments[0]["responses"])
		}
	})

	t.Run("supplied values are still validated by the generated client", func(t *testing.T) {
		tests := []struct {
			name      string
			voteID    string
			wantError bool
		}{
			{name: "valid vote_id", voteID: voteID},
			{name: "invalid vote_id", voteID: "not-a-uuid", wantError: true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := serve(t, `{"num_recipients":1,"num_votes_cast":1,"poll_end_time":"2030-01-01T00:00:00Z","comment_results":[{"prompt":{"prompt_id":"`+prompt1+`","prompt":"Why?"},"responses":[{"vote_id":"`+tt.voteID+`","comment_text":"x"}]}]}`)
				_, err := voteclient.DecodeGetVoteResultsResponse(goahttp.ResponseDecoder, false)(response)
				if !tt.wantError {
					if err != nil {
						t.Fatalf("valid control must decode successfully: %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), "body.vote_id") {
					t.Fatalf("want vote_id validation error, got %v", err)
				}
			})
		}
	})
}
