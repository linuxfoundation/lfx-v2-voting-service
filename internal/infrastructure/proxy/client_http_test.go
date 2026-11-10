// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

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
