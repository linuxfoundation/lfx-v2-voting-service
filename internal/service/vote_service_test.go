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
)

// capturePollClient records the last CreatePoll/UpdatePoll/ExtendPoll request so tests
// can assert on the fields the proxy forwards to ITX. Only those methods are exercised here.
type capturePollClient struct {
	domain.PollClient
	lastCreate *itx.CreatePollRequest
	lastUpdate *itx.UpdatePollRequest
	lastExtend *itx.ExtendPollRequest
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
// the optional timezone must reach the ITX create request untouched.
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
