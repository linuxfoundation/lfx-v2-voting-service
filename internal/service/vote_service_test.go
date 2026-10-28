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

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
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
