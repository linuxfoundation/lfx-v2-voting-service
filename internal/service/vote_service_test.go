// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// capturePollClient records the last CreatePoll request so tests can assert on the
// fields the proxy forwards to ITX. Only CreatePoll is exercised here.
type capturePollClient struct {
	domain.PollClient
	lastCreate *itx.CreatePollRequest
}

func (c *capturePollClient) CreatePoll(_ context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error) {
	c.lastCreate = req
	// Return a response with no IDs so mapPollResponseV1ToV2 is a no-op.
	return &itx.PollResponse{PollID: "poll-1", Status: "disabled"}, nil
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
