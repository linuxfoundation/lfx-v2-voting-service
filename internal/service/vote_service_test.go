// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"fmt"
	"net/url"
	"strings"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"
)

// capturePollClient records the last CreatePoll/UpdatePoll/ExtendPoll request so tests
// can assert on the fields the proxy forwards to ITX. It also stubs GetPoll/EndPoll for the
// EndVote tests; only those methods are exercised here.
type capturePollClient struct {
	domain.PollClient
	lastCreate *itx.CreatePollRequest
	lastUpdate *itx.UpdatePollRequest
	lastExtend *itx.ExtendPollRequest

	getPollResp  *itx.PollResponse
	getPollErr   error
	endPollResp  *itx.PollResponse
	endPollErr   error
	endPollCalls []string
}

func (c *capturePollClient) GetPoll(_ context.Context, _ string) (*itx.PollResponse, error) {
	return c.getPollResp, c.getPollErr
}

func (c *capturePollClient) EndPoll(_ context.Context, pollID string) (*itx.PollResponse, error) {
	c.endPollCalls = append(c.endPollCalls, pollID)
	return c.endPollResp, c.endPollErr
}

func (c *capturePollClient) CreatePoll(_ context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error) {
	c.lastCreate = req
	// Return a response with no IDs so mapPollResponseV1ToV2 is a no-op.
	return &itx.PollResponse{PollID: "poll-1", Status: "disabled"}, nil
}

func (c *capturePollClient) UpdatePoll(_ context.Context, _ string, req *itx.UpdatePollRequest) (*itx.PollResponse, error) {
	c.lastUpdate = req
	// Return a response with no IDs so mapPollResponseV1ToV2 is a no-op.
	return &itx.PollResponse{PollID: "poll-1", Status: "disabled"}, nil
}

func (c *capturePollClient) ExtendPoll(_ context.Context, _ string, req *itx.ExtendPollRequest) (*itx.PollResponse, error) {
	c.lastExtend = req
	// Return a response with no IDs so mapPollResponseV1ToV2 is a no-op.
	return &itx.PollResponse{PollID: "poll-1", Status: "active"}, nil
}

// identityIDMapper maps v2 UIDs to v1 SFIDs by echoing the input, which is all CreateVote
// needs to reach the CreatePoll call.
type identityIDMapper struct{}

func (identityIDMapper) MapProjectV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, nil
}
func (identityIDMapper) MapProjectV1ToV2(_ context.Context, v1SFID string) (string, error) {
	return v1SFID, nil
}
func (identityIDMapper) MapCommitteeV2ToV1(_ context.Context, v2UID string) (string, error) {
	return v2UID, nil
}
func (identityIDMapper) MapCommitteeV1ToV2(_ context.Context, v1SFID string) (string, error) {
	return v1SFID, nil
}

func TestCreateVoteForwardsSelfServeSource(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	if _, err := svc.CreateVote(ctx, &CreateVoteRequest{Name: "poll", ProjectUID: "project-uid"}); err != nil {
		t.Fatalf("CreateVote returned error: %v", err)
	}

	if client.lastCreate == nil {
		t.Fatal("expected CreatePoll to be called")
	}
	if client.lastCreate.CreatedBy == nil || client.lastCreate.CreatedBy.Username != "test-user" {
		t.Fatalf("expected CreatedBy.Username %q, got %+v", "test-user", client.lastCreate.CreatedBy)
	}
	if client.lastCreate.Source != itx.PollSourceSelfServe {
		t.Fatalf("expected Source %q, got %q", itx.PollSourceSelfServe, client.lastCreate.Source)
	}
}

// TestCreateVoteForwardsEndTimeTimezone mirrors TestCreateVoteForwardsSelfServeSource:
// the required timezone must reach the ITX create request untouched.
func TestCreateVoteForwardsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	req := &CreateVoteRequest{Name: "poll", ProjectUID: "project-uid", EndTimeTimezone: "America/New_York"}
	if _, err := svc.CreateVote(ctx, req); err != nil {
		t.Fatalf("CreateVote returned error: %v", err)
	}

	if client.lastCreate == nil {
		t.Fatal("expected CreatePoll to be called")
	}
	if client.lastCreate.EndTimeTimezone != "America/New_York" {
		t.Fatalf("expected EndTimeTimezone %q, got %q", "America/New_York", client.lastCreate.EndTimeTimezone)
	}
}

// TestUpdateVoteForwardsEndTimeTimezone guards the update hop: dropping a supplied
// field here would silently ignore the requested timezone change — omission preserves
// the previously stored timezone, it does not clear it.
func TestUpdateVoteForwardsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	req := &UpdateVoteRequest{Name: "poll", ProjectUID: "project-uid", EndTimeTimezone: "America/New_York"}
	if _, err := svc.UpdateVote(ctx, "poll-1", req); err != nil {
		t.Fatalf("UpdateVote returned error: %v", err)
	}

	if client.lastUpdate == nil {
		t.Fatal("expected UpdatePoll to be called")
	}
	if client.lastUpdate.EndTimeTimezone != "America/New_York" {
		t.Fatalf("expected EndTimeTimezone %q, got %q", "America/New_York", client.lastUpdate.EndTimeTimezone)
	}
}

// TestExtendVoteForwardsEndTimeTimezone guards the extend hop's scalar parameter.
func TestExtendVoteForwardsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	if _, err := svc.ExtendVote(ctx, "poll-1", "2026-03-01T23:59:59Z", "America/New_York"); err != nil {
		t.Fatalf("ExtendVote returned error: %v", err)
	}

	if client.lastExtend == nil {
		t.Fatal("expected ExtendPoll to be called")
	}
	if client.lastExtend.EndTimeTimezone != "America/New_York" {
		t.Fatalf("expected EndTimeTimezone %q, got %q", "America/New_York", client.lastExtend.EndTimeTimezone)
	}
}

// assertKeyAbsent marshals a captured ITX request and fails if key is present on
// the wire — the `,omitempty` tags must drop empty timezone fields so an empty
// `end_time_timezone` key never reaches ITX. A struct-field check alone cannot guard
// this: removing `omitempty` would send `"key":""` while the struct test still passes.
func assertKeyAbsent(t *testing.T, req any, key string) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, ok := wire[key]; ok {
		t.Fatalf("expected key %q to be absent from the wire, got %s", key, payload)
	}
}

// TestCreateVoteOmitsEndTimeTimezone is a defensive wire invariant: an empty
// end_time_timezone key must never hit the ITX wire ("" + `,omitempty` => key
// absent). The empty string is unreachable via HTTP — the field is required at the
// v2 boundary — but this guards the `,omitempty` tag against accidental removal.
func TestCreateVoteOmitsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	if _, err := svc.CreateVote(ctx, &CreateVoteRequest{Name: "poll", ProjectUID: "project-uid"}); err != nil {
		t.Fatalf("CreateVote returned error: %v", err)
	}

	if client.lastCreate == nil {
		t.Fatal("expected CreatePoll to be called")
	}
	if client.lastCreate.EndTimeTimezone != "" {
		t.Fatalf("expected empty EndTimeTimezone when omitted, got %q", client.lastCreate.EndTimeTimezone)
	}
	assertKeyAbsent(t, client.lastCreate, "end_time_timezone")
}

// TestUpdateVoteOmitsEndTimeTimezone is a defensive wire invariant: an empty
// end_time_timezone key must never hit the ITX wire on update. Unreachable via HTTP
// (the field is required at the v2 boundary); guards the `,omitempty` tag.
func TestUpdateVoteOmitsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	if _, err := svc.UpdateVote(ctx, "poll-1", &UpdateVoteRequest{Name: "poll", ProjectUID: "project-uid"}); err != nil {
		t.Fatalf("UpdateVote returned error: %v", err)
	}

	if client.lastUpdate == nil {
		t.Fatal("expected UpdatePoll to be called")
	}
	if client.lastUpdate.EndTimeTimezone != "" {
		t.Fatalf("expected empty EndTimeTimezone when omitted, got %q", client.lastUpdate.EndTimeTimezone)
	}
	assertKeyAbsent(t, client.lastUpdate, "end_time_timezone")
}

// TestExtendVoteOmitsEndTimeTimezone is a defensive wire invariant: an empty
// end_time_timezone key must never hit the ITX wire on extend. Unreachable via HTTP
// (the field is required at the v2 boundary); guards the `,omitempty` tag.
func TestExtendVoteOmitsEndTimeTimezone(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	if _, err := svc.ExtendVote(ctx, "poll-1", "2026-03-01T23:59:59Z", ""); err != nil {
		t.Fatalf("ExtendVote returned error: %v", err)
	}

	if client.lastExtend == nil {
		t.Fatal("expected ExtendPoll to be called")
	}
	if client.lastExtend.EndTimeTimezone != "" {
		t.Fatalf("expected empty EndTimeTimezone when omitted, got %q", client.lastExtend.EndTimeTimezone)
	}
	assertKeyAbsent(t, client.lastExtend, "end_time_timezone")
}

// Guards the CreateVote authentication check at the service layer so a future
// refactor that drops or bypasses the principal guard fails this test, not just
// the creatorFromPrincipal unit test.
func TestCreateVoteRejectsWhitespaceOnlyPrincipal(t *testing.T) {
	client := &capturePollClient{}
	svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "   ")
	_, err := svc.CreateVote(ctx, &CreateVoteRequest{Name: "poll", ProjectUID: "project-uid"})

	if err == nil {
		t.Fatal("expected validation error for whitespace-only principal, got nil")
	}
	var dErr *domain.DomainError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if dErr.Type != domain.ErrorTypeValidation {
		t.Fatalf("expected ErrorTypeValidation, got %d", dErr.Type)
	}
	if client.lastCreate != nil {
		t.Fatalf("expected CreatePoll not to be called, but it was with %+v", client.lastCreate)
	}
}

func TestCreatorFromPrincipal(t *testing.T) {
	tests := []struct {
		name         string
		principal    string
		wantNil      bool
		wantUsername string
	}{
		{name: "principal maps to created_by username", principal: "abc123", wantUsername: "abc123"},
		{name: "empty principal yields nil", principal: "", wantNil: true},
		{name: "whitespace-only principal yields nil", principal: "   ", wantNil: true},
		{name: "whitespace trimmed", principal: "  uid  ", wantUsername: "uid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := creatorFromPrincipal(tt.principal)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected created_by, got nil")
			}
			if got.Username != tt.wantUsername {
				t.Fatalf("expected username %q, got %q", tt.wantUsername, got.Username)
			}
			if got.ID != "" || got.Email != "" || got.Name != "" {
				t.Fatalf("expected only username set, got %+v", got)
			}
		})
	}
}

// TestVoteNumWinnersResolution pins the #2922 rule on both write hops: num_winners
// reaches ITX only when the client explicitly provided it — except meek_stv creates,
// which default to 2 winners when omitted. Updates leave resolution to ITX.
// The wire-level assertKeyAbsent guards the
// `,omitempty` tag so a nil pointer can never leak onto the wire as a value.
func TestVoteNumWinnersResolution(t *testing.T) {
	tests := []struct {
		name       string
		pollType   string
		numWinners *int
		wantCreate *int
		wantUpdate *int
	}{
		{name: "generic omitted stays omitted", pollType: "generic"},
		{name: "condorcet_irv omitted stays omitted", pollType: "condorcet_irv"},
		{name: "instant_runoff_vote omitted stays omitted", pollType: "instant_runoff_vote"},
		{name: "meek_stv omitted defaults only on create", pollType: "meek_stv", wantCreate: utils.IntPtr(2)},
		{name: "meek_stv explicit passes through", pollType: "meek_stv", numWinners: utils.IntPtr(5), wantCreate: utils.IntPtr(5), wantUpdate: utils.IntPtr(5)},
		{name: "generic explicit passes through", pollType: "generic", numWinners: utils.IntPtr(4), wantCreate: utils.IntPtr(4), wantUpdate: utils.IntPtr(4)},
		{name: "omitted type and count stay omitted"},
		{name: "omitted type explicit count passes through", numWinners: utils.IntPtr(4), wantCreate: utils.IntPtr(4), wantUpdate: utils.IntPtr(4)},
	}

	assertNumWinners := func(t *testing.T, got, want *int, req any) {
		t.Helper()
		if want == nil {
			if got != nil {
				t.Fatalf("expected NumWinners nil, got %d", *got)
			}
			assertKeyAbsent(t, req, "num_winners")
			return
		}
		if got == nil {
			t.Fatalf("expected NumWinners %d, got nil", *want)
		}
		if *got != *want {
			t.Fatalf("expected NumWinners %d, got %d", *want, *got)
		}
	}

	for _, tt := range tests {
		t.Run("create/"+tt.name, func(t *testing.T) {
			client := &capturePollClient{}
			svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")

			req := &CreateVoteRequest{Name: "poll", ProjectUID: "project-uid", PollType: tt.pollType, NumWinners: tt.numWinners}
			if _, err := svc.CreateVote(ctx, req); err != nil {
				t.Fatalf("CreateVote returned error: %v", err)
			}
			if client.lastCreate == nil {
				t.Fatal("expected CreatePoll to be called")
			}
			assertNumWinners(t, client.lastCreate.NumWinners, tt.wantCreate, client.lastCreate)
		})

		t.Run("update/"+tt.name, func(t *testing.T) {
			client := &capturePollClient{}
			svc := NewVoteService(nil, client, identityIDMapper{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")

			req := &UpdateVoteRequest{Name: "poll", ProjectUID: "project-uid", PollType: tt.pollType, NumWinners: tt.numWinners}
			if _, err := svc.UpdateVote(ctx, "poll-1", req); err != nil {
				t.Fatalf("UpdateVote returned error: %v", err)
			}
			if client.lastUpdate == nil {
				t.Fatal("expected UpdatePoll to be called")
			}
			assertNumWinners(t, client.lastUpdate.NumWinners, tt.wantUpdate, client.lastUpdate)
			if tt.pollType == "" {
				assertKeyAbsent(t, client.lastUpdate, "poll_type")
			}
		})
	}
}

func TestEndVote(t *testing.T) {
	ctx := context.WithValue(context.Background(), constants.PrincipalContextID, "test-user")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("active vote is ended and the ITX response returned", func(t *testing.T) {
		ended := &itx.PollResponse{PollID: "poll-1", Status: "ended", EarlyEndTime: "2026-02-20T09:00:00Z"}
		client := &capturePollClient{getPollResp: &itx.PollResponse{PollID: "poll-1", Status: "active"}, endPollResp: ended}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		got, err := svc.EndVote(ctx, "poll-1")
		if err != nil {
			t.Fatalf("EndVote returned error: %v", err)
		}
		if len(client.endPollCalls) != 1 || client.endPollCalls[0] != "poll-1" {
			t.Fatalf("expected one EndPoll call for poll-1, got %v", client.endPollCalls)
		}
		if got.Status != "ended" || got.EarlyEndTime != "2026-02-20T09:00:00Z" {
			t.Fatalf("unexpected response: %+v", got)
		}
	})

	t.Run("disabled vote returns conflict without calling ITX end", func(t *testing.T) {
		client := &capturePollClient{getPollResp: &itx.PollResponse{PollID: "poll-1", Status: "disabled"}}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		_, err := svc.EndVote(ctx, "poll-1")
		if err == nil {
			t.Fatal("expected an error for a disabled vote")
		}
		if got := domain.GetErrorType(err); got != domain.ErrorTypeConflict {
			t.Fatalf("expected ErrorTypeConflict, got %v", got)
		}
		if len(client.endPollCalls) != 0 {
			t.Fatalf("expected EndPoll not to be called, got %v", client.endPollCalls)
		}
	})

	t.Run("already ended vote is returned unchanged", func(t *testing.T) {
		current := &itx.PollResponse{PollID: "poll-1", Status: "ended", EarlyEndTime: "2026-02-20T09:00:00Z"}
		client := &capturePollClient{getPollResp: current, endPollResp: current}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		got, err := svc.EndVote(ctx, "poll-1")
		if err != nil {
			t.Fatalf("EndVote returned error: %v", err)
		}
		if got.Status != "ended" || got.EarlyEndTime != "2026-02-20T09:00:00Z" {
			t.Fatalf("unexpected response: %+v", got)
		}
	})

	t.Run("EndPoll error is propagated", func(t *testing.T) {
		endErr := domain.NewValidationError("poll is already past its scheduled end time")
		client := &capturePollClient{getPollResp: &itx.PollResponse{PollID: "poll-1", Status: "active"}, endPollErr: endErr}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		_, err := svc.EndVote(ctx, "poll-1")
		if !errors.Is(err, endErr) {
			t.Fatalf("expected EndPoll error to propagate, got %v", err)
		}
	})

	t.Run("GetPoll error is propagated without calling EndPoll", func(t *testing.T) {
		getErr := domain.NewNotFoundError("poll not found")
		client := &capturePollClient{getPollErr: getErr}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		_, err := svc.EndVote(ctx, "poll-1")
		if !errors.Is(err, getErr) {
			t.Fatalf("expected GetPoll error to propagate, got %v", err)
		}
		if len(client.endPollCalls) != 0 {
			t.Fatalf("expected EndPoll not to be called, got %v", client.endPollCalls)
		}
	})

	t.Run("missing principal is rejected", func(t *testing.T) {
		client := &capturePollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, logger)

		if _, err := svc.EndVote(context.Background(), "poll-1"); err == nil {
			t.Fatal("expected an error without a principal")
		}
		if len(client.endPollCalls) != 0 {
			t.Fatalf("expected EndPoll not to be called, got %v", client.endPollCalls)
		}
	})
}

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

func (c *ballotVoteClient) GetVote(_ context.Context, voteID string) (*itx.VoteResponse, error) {
	c.requested = voteID
	return c.ballot, c.err
}

func (r *ballotPollReader) GetPoll(_ context.Context, pollID string) (*itx.PollResponse, error) {
	r.requested = pollID
	return r.poll, r.err
}

// prefixProjectIDMapper maps a v1 project SFID to "uid-<sfid>", so tests can tell a mapped
// UID from the raw SFID.
type prefixProjectIDMapper struct{ identityIDMapper }

func (prefixProjectIDMapper) MapProjectV1ToV2(_ context.Context, v1SFID string) (string, error) {
	return "uid-" + v1SFID, nil
}

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

// recordingPollClient records the IDs/arguments passed to the poll operations that
// have no dedicated capture client and returns canned values.
type recordingPollClient struct {
	domain.PollClient

	err error

	deleteID  string
	enableID  string
	resultsID string
	results   *itx.VoteResults
}

func (c *recordingPollClient) DeletePoll(_ context.Context, id string) error {
	c.deleteID = id
	return c.err
}

func (c *recordingPollClient) EnablePoll(_ context.Context, id string) error {
	c.enableID = id
	return c.err
}

func (c *recordingPollClient) GetPollResults(_ context.Context, id string) (*itx.VoteResults, error) {
	c.resultsID = id
	if c.err != nil {
		return nil, c.err
	}
	return c.results, nil
}

// directionIDMapper maps IDs by adding a direction-specific prefix so tests can tell
// exactly which mapper method was called for which field. Any ID listed in failOn
// returns an error instead.
type directionIDMapper struct {
	failOn map[string]error
	calls  []string
}

func (m *directionIDMapper) do(kind, id string) (string, error) {
	m.calls = append(m.calls, kind+":"+id)
	if err, ok := m.failOn[id]; ok {
		return "", err
	}
	return kind + "/" + id, nil
}

func (m *directionIDMapper) MapProjectV2ToV1(_ context.Context, id string) (string, error) {
	return m.do("project-v1", id)
}

func (m *directionIDMapper) MapProjectV1ToV2(_ context.Context, id string) (string, error) {
	return m.do("project-v2", id)
}

func (m *directionIDMapper) MapCommitteeV2ToV1(_ context.Context, id string) (string, error) {
	return m.do("committee-v1", id)
}

func (m *directionIDMapper) MapCommitteeV1ToV2(_ context.Context, id string) (string, error) {
	return m.do("committee-v2", id)
}

func TestVoteService_DeleteVote(t *testing.T) {
	t.Run("forwards the vote ID to ITX", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(ctxWithPrincipal("alice"), "vote-1")

		require.NoError(t, err)
		assert.Equal(t, "vote-1", client.deleteID)
	})

	t.Run("propagates the proxy error unchanged", func(t *testing.T) {
		proxyErr := domain.NewNotFoundError("vote not found")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(ctxWithPrincipal("alice"), "vote-1")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.DeleteVote(context.Background(), "vote-1")

		assertValidationError(t, err)
		assert.Empty(t, client.deleteID)
	})
}

func TestVoteService_EnableVote(t *testing.T) {
	t.Run("forwards the vote ID to ITX", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(ctxWithPrincipal("alice"), "vote-2")

		require.NoError(t, err)
		assert.Equal(t, "vote-2", client.enableID)
	})

	t.Run("propagates the proxy error unchanged", func(t *testing.T) {
		proxyErr := domain.NewConflictError("vote already enabled")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(ctxWithPrincipal("alice"), "vote-2")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		err := svc.EnableVote(context.Background(), "vote-2")

		assertValidationError(t, err)
		assert.Empty(t, client.enableID)
	})
}

func TestVoteService_GetVoteResults(t *testing.T) {
	t.Run("returns the ITX results for the vote", func(t *testing.T) {
		want := &itx.VoteResults{NumRecipients: 10, NumVotesCast: 7, NumAbstained: utils.IntPtr(1), PollEndTime: "2026-01-02T03:04:05Z"}
		client := &recordingPollClient{results: want}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(ctxWithPrincipal("alice"), "vote-3")

		require.NoError(t, err)
		assert.Same(t, want, got)
		assert.Equal(t, "vote-3", client.resultsID)
	})

	t.Run("propagates the proxy error and returns no results", func(t *testing.T) {
		proxyErr := domain.NewUnavailableError("itx down")
		svc := NewVoteService(nil, &recordingPollClient{err: proxyErr}, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(ctxWithPrincipal("alice"), "vote-3")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
	})

	t.Run("does not call ITX without a principal", func(t *testing.T) {
		client := &recordingPollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.GetVoteResults(context.Background(), "vote-3")

		assertValidationError(t, err)
		assert.Nil(t, got)
		assert.Empty(t, client.resultsID)
	})
}

func TestVoteService_mapPollResponseV1ToV2(t *testing.T) {
	ctx := context.Background()

	t.Run("maps both project and committee IDs", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.NoError(t, err)
		assert.Equal(t, "project-v2/p1", resp.ProjectID)
		assert.Equal(t, "committee-v2/c1", resp.CommitteeID)
	})

	t.Run("skips the mapper for empty IDs", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.NoError(t, err)
		assert.Empty(t, mapper.calls)
		assert.Empty(t, resp.ProjectID)
		assert.Empty(t, resp.CommitteeID)
	})

	t.Run("maps only the committee when there is no project", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{CommitteeID: "c1"}

		require.NoError(t, svc.mapPollResponseV1ToV2(ctx, resp))

		assert.Equal(t, []string{"committee-v2:c1"}, mapper.calls)
		assert.Empty(t, resp.ProjectID)
		assert.Equal(t, "committee-v2/c1", resp.CommitteeID)
	})

	t.Run("project mapping failure is returned and leaves the response untouched", func(t *testing.T) {
		boom := domain.NewInternalError("mapper down")
		mapper := &directionIDMapper{failOn: map[string]error{"p1": boom}}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.ErrorIs(t, err, boom)
		assert.Equal(t, "p1", resp.ProjectID)
		assert.Equal(t, "c1", resp.CommitteeID)
		assert.Equal(t, []string{"project-v2:p1"}, mapper.calls, "committee must not be mapped after a project failure")
	})

	t.Run("committee mapping failure is returned", func(t *testing.T) {
		boom := domain.NewNotFoundError("no such committee")
		mapper := &directionIDMapper{failOn: map[string]error{"c1": boom}}
		svc := NewVoteService(nil, nil, mapper, discardLogger())
		resp := &itx.PollResponse{ProjectID: "p1", CommitteeID: "c1"}

		err := svc.mapPollResponseV1ToV2(ctx, resp)

		require.ErrorIs(t, err, boom)
		assert.Equal(t, "c1", resp.CommitteeID)
	})
}

func TestVoteService_mapRequestIDsV2ToV1(t *testing.T) {
	ctx := context.Background()

	t.Run("maps project, committee and committee list", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())

		project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "c1", []string{"c2", "c3"})

		require.NoError(t, err)
		assert.Equal(t, "project-v1/p1", project)
		assert.Equal(t, "committee-v1/c1", committee)
		assert.Equal(t, []string{"committee-v1/c2", "committee-v1/c3"}, committees, "order must be preserved")
	})

	t.Run("empty committee and nil list are not mapped", func(t *testing.T) {
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, nil, mapper, discardLogger())

		project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "", nil)

		require.NoError(t, err)
		assert.Equal(t, "project-v1/p1", project)
		assert.Empty(t, committee)
		assert.Nil(t, committees)
		assert.Equal(t, []string{"project-v1:p1"}, mapper.calls)
	})

	t.Run("empty list (non-nil) yields nil committee IDs", func(t *testing.T) {
		svc := NewVoteService(nil, nil, &directionIDMapper{}, discardLogger())

		_, _, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "", []string{})

		require.NoError(t, err)
		assert.Nil(t, committees)
	})

	failures := []struct {
		name   string
		failOn string
	}{
		{"project mapping failure", "p1"},
		{"single committee mapping failure", "c1"},
		{"committee list mapping failure", "c3"},
	}
	for _, tc := range failures {
		t.Run(tc.name+" returns zero values and the error", func(t *testing.T) {
			boom := fmt.Errorf("cannot map %s", tc.failOn)
			mapper := &directionIDMapper{failOn: map[string]error{tc.failOn: boom}}
			svc := NewVoteService(nil, nil, mapper, discardLogger())

			project, committee, committees, err := svc.mapRequestIDsV2ToV1(ctx, "p1", "c1", []string{"c2", "c3"})

			require.ErrorIs(t, err, boom)
			assert.Empty(t, project)
			assert.Empty(t, committee)
			assert.Nil(t, committees)
		})
	}
}

// failingPollClient returns a configured error (and optionally a response) from UpdatePoll
// and ExtendPoll.
type failingPollClient struct {
	domain.PollClient
	resp *itx.PollResponse
	err  error
}

func (c *failingPollClient) UpdatePoll(_ context.Context, _ string, _ *itx.UpdatePollRequest) (*itx.PollResponse, error) {
	return c.resp, c.err
}

func (c *failingPollClient) ExtendPoll(_ context.Context, _ string, _ *itx.ExtendPollRequest) (*itx.PollResponse, error) {
	return c.resp, c.err
}

func TestVoteService_UpdateVote_MapsRequestToITX(t *testing.T) {
	client := &capturePollClient{}
	mapper := &directionIDMapper{}
	svc := NewVoteService(nil, client, mapper, discardLogger())

	_, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{
		Name:                       "Board Vote",
		Description:                "Annual vote",
		EndTime:                    "2026-03-01T17:00:00Z",
		EndTimeTimezone:            "America/New_York",
		ProjectUID:                 "proj-1",
		CommitteeUID:               "comm-1",
		CommitteeUIDs:              []string{"comm-2", "comm-3"},
		CommitteeFilters:           []string{"Voting Rep"},
		PseudoAnonymity:            true,
		PollType:                   "meek_stv",
		NumWinners:                 utils.IntPtr(3),
		AllowAbstain:               true,
		QuorumPercentage:           utils.IntPtr(50),
		WinningThresholdPercentage: utils.IntPtr(66),
		PollQuestions: []PollQuestionRequest{
			{Prompt: "Who?", Type: "ranked_choice", Choices: []PollChoiceRequest{{ChoiceText: "Alice Example"}, {ChoiceText: "Bob Fixture"}}},
		},
		PollCommentPrompts: []PollCommentPromptRequest{{Prompt: "Why?"}},
	})

	require.NoError(t, err)
	req := client.lastUpdate
	require.NotNil(t, req)
	assert.Equal(t, "Board Vote", req.Name)
	assert.Equal(t, "Annual vote", req.Description)
	assert.Equal(t, "2026-03-01T17:00:00Z", req.EndTime)
	assert.Equal(t, "America/New_York", req.EndTimeTimezone)
	assert.Equal(t, "project-v1/proj-1", req.ProjectID, "v2 UIDs must be mapped to v1 IDs before reaching ITX")
	assert.Equal(t, "committee-v1/comm-1", req.CommitteeID)
	assert.Equal(t, []string{"committee-v1/comm-2", "committee-v1/comm-3"}, req.CommitteeIDs)
	assert.Equal(t, []string{"Voting Rep"}, req.CommitteeFilters)
	assert.True(t, req.PseudoAnonymity)
	assert.Equal(t, "meek_stv", req.PollType)
	require.NotNil(t, req.NumWinners)
	assert.Equal(t, 3, *req.NumWinners)
	assert.True(t, req.AllowAbstain)
	require.NotNil(t, req.QuorumPercentage)
	assert.Equal(t, 50, *req.QuorumPercentage)
	require.NotNil(t, req.WinningThresholdPercentage)
	assert.Equal(t, 66, *req.WinningThresholdPercentage)

	require.Len(t, req.PollQuestions, 1)
	assert.Equal(t, "Who?", req.PollQuestions[0].Prompt)
	assert.Equal(t, "ranked_choice", req.PollQuestions[0].Type)
	assert.Equal(t, []string{"Alice Example", "Bob Fixture"}, req.PollQuestions[0].Choices)
	require.Len(t, req.PollCommentPrompts, 1)
	assert.Equal(t, "Why?", req.PollCommentPrompts[0].Prompt)
}

func TestVoteService_UpdateVote_ResponseIDsAreMappedBack(t *testing.T) {
	client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1", CommitteeID: "c1"}}
	svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

	got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

	require.NoError(t, err)
	assert.Equal(t, "project-v2/p1", got.ProjectID)
	assert.Equal(t, "committee-v2/c1", got.CommitteeID)
}

func TestVoteService_UpdateVote_Errors(t *testing.T) {
	t.Run("an ID mapping failure stops before ITX is called", func(t *testing.T) {
		client := &capturePollClient{}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"proj-1": domain.NewNotFoundError("no such project")}}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
		assert.Nil(t, client.lastUpdate)
	})

	t.Run("an ITX error is returned unchanged", func(t *testing.T) {
		client := &failingPollClient{err: domain.NewConflictError("vote is active")}
		svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{ProjectUID: "proj-1"})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeConflict, domain.GetErrorType(err))
	})

	t.Run("a response ID mapping failure is returned", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"p1": domain.NewInternalError("mapper down")}}, discardLogger())

		got, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{})

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeInternal, domain.GetErrorType(err))
	})

	t.Run("a name over 200 characters is rejected before any lookup", func(t *testing.T) {
		client := &capturePollClient{}
		mapper := &directionIDMapper{}
		svc := NewVoteService(nil, client, mapper, discardLogger())

		_, err := svc.UpdateVote(ctxWithPrincipal("alice"), "vote-1", &UpdateVoteRequest{Name: strings.Repeat("x", 201), ProjectUID: "proj-1"})

		assertValidationError(t, err)
		assert.Empty(t, mapper.calls)
		assert.Nil(t, client.lastUpdate)
	})
}

func TestVoteService_ExtendVote_Errors(t *testing.T) {
	t.Run("requires a principal", func(t *testing.T) {
		client := &capturePollClient{}
		svc := NewVoteService(nil, client, identityIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(context.Background(), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		assertValidationError(t, err)
		assert.Nil(t, got)
		assert.Nil(t, client.lastExtend)
	})

	t.Run("an ITX error is returned unchanged", func(t *testing.T) {
		svc := NewVoteService(nil, &failingPollClient{err: domain.NewValidationError("vote already ended")}, identityIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, "vote already ended", err.Error())
	})

	t.Run("response IDs are mapped back to v2 UIDs", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", ProjectID: "p1", CommitteeID: "c1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.NoError(t, err)
		assert.Equal(t, "project-v2/p1", got.ProjectID)
		assert.Equal(t, "committee-v2/c1", got.CommitteeID)
	})

	t.Run("a response ID mapping failure is returned", func(t *testing.T) {
		client := &failingPollClient{resp: &itx.PollResponse{PollID: "poll-1", CommitteeID: "c1"}}
		svc := NewVoteService(nil, client, &directionIDMapper{failOn: map[string]error{"c1": domain.NewNotFoundError("no such committee")}}, discardLogger())

		got, err := svc.ExtendVote(ctxWithPrincipal("alice"), "vote-1", "2026-03-01T17:00:00Z", "UTC")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.Equal(t, domain.ErrorTypeNotFound, domain.GetErrorType(err))
	})
}
