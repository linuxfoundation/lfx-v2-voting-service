// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	infraeventing "github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service/eventsync"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// kvTestInvitationGuards adapts existing KV test doubles to the pure domain
// guard interface; production never uses this adapter or opens the legacy KV.
type kvTestInvitationGuards struct{ kv jetstream.KeyValue }

func (g *kvTestInvitationGuards) LookupInvitation(ctx context.Context, id string) (domain.InvitationGuard, error) {
	e, err := g.kv.Get(ctx, voteResponseLFIDInviteSentKey(id))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.InvitationGuard{}, nil
	}
	if err != nil {
		return domain.InvitationGuard{}, err
	}
	return domain.InvitationGuard{Exists: true, Pending: string(e.Value()) == "pending", InviteID: string(e.Value()), Revision: e.Revision()}, nil
}
func (g *kvTestInvitationGuards) ClaimInvitation(ctx context.Context, id string) (uint64, error) {
	rev, err := g.kv.Create(ctx, voteResponseLFIDInviteSentKey(id), []byte("pending"))
	if errors.Is(err, jetstream.ErrKeyExists) {
		return 0, domain.ErrEventStateExists
	}
	return rev, err
}
func (g *kvTestInvitationGuards) CompleteInvitation(ctx context.Context, id string, rev uint64, invite string) error {
	_, err := g.kv.Update(ctx, voteResponseLFIDInviteSentKey(id), []byte(invite), rev)
	return err
}
func (g *kvTestInvitationGuards) PrepareInvitation(context.Context, string) error { return nil }
func (g *kvTestInvitationGuards) ResolveInvitation(context.Context, string) error { return nil }
func (g *kvTestInvitationGuards) InvitationPrepared(context.Context, string) (bool, error) {
	return true, nil
}

type stubVoteInviteUserReader struct {
	username string
	err      error
}

func (s stubVoteInviteUserReader) UsernameByEmail(_ context.Context, _ string) (string, error) {
	return s.username, s.err
}

type stubVoteInviteSender struct {
	result *domain.InviteResult
	err    error
	called bool
	last   inviteapi.SendInviteRequest
}

func (s *stubVoteInviteSender) SendInvite(_ context.Context, req inviteapi.SendInviteRequest) (*domain.InviteResult, error) {
	s.called = true
	s.last = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestMaybeInvite(t *testing.T) {
	const (
		voteResponseUID = "vote-123"
		pollID          = "poll-456"
		email           = "guest@example.com"
	)

	inviteSentKey := voteResponseLFIDInviteSentKey(voteResponseUID)
	pollKey := "itx-poll." + pollID
	pollPayload, err := json.Marshal(map[string]any{"name": "Board Election"})
	require.NoError(t, err)

	tests := []struct {
		name         string
		userReader   stubVoteInviteUserReader
		setupObjects func(*mockKeyValue)
		setupMaps    func(*mockKeyValue)
		wantCalled   bool
	}{
		{
			name: "skips when invite already sent",
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).
					Return(mockKeyValueEntry{key: inviteSentKey, value: []byte("invite-old")}, nil)
			},
		},
		{
			name:       "skips when participant already has LFID",
			userReader: stubVoteInviteUserReader{username: "existing"},
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).Return(nil, jetstream.ErrKeyNotFound)
			},
		},
		{
			name:       "skips when vote name cannot be resolved",
			userReader: stubVoteInviteUserReader{err: domain.ErrUserNotFound},
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).Return(nil, jetstream.ErrKeyNotFound)
			},
			setupObjects: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).Return(nil, jetstream.ErrKeyNotFound)
			},
		},
		{
			name:       "sends invite and stores sent marker on success",
			userReader: stubVoteInviteUserReader{err: domain.ErrUserNotFound},
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).Return(nil, jetstream.ErrKeyNotFound)
				kv.On("Create", mock.Anything, inviteSentKey, []byte("pending")).Return(uint64(1), nil)
				kv.On("Update", mock.Anything, inviteSentKey, []byte("invite-new"), uint64(1)).Return(uint64(2), nil)
			},
			setupObjects: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).
					Return(mockKeyValueEntry{key: pollKey, value: pollPayload}, nil)
			},
			wantCalled: true,
		},
		{
			name:       "proceeds with invite on transient auth lookup failure",
			userReader: stubVoteInviteUserReader{err: errors.New("auth unavailable")},
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).Return(nil, jetstream.ErrKeyNotFound)
				kv.On("Create", mock.Anything, inviteSentKey, []byte("pending")).Return(uint64(1), nil)
				kv.On("Update", mock.Anything, inviteSentKey, []byte("invite-new"), uint64(1)).Return(uint64(2), nil)
			},
			setupObjects: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).
					Return(mockKeyValueEntry{key: pollKey, value: pollPayload}, nil)
			},
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectsKV := &mockKeyValue{}
			mappingsKV := &mockKeyValue{}
			if tt.setupObjects != nil {
				tt.setupObjects(objectsKV)
			}
			if tt.setupMaps != nil {
				tt.setupMaps(mappingsKV)
			}

			sender := &stubVoteInviteSender{
				result: &domain.InviteResult{
					InviteUID:      "invite-new",
					RecipientEmail: email,
					ExpiresAt:      time.Now().Add(24 * time.Hour),
				},
			}

			h := &VoteResponseInviteHandler{
				v1ObjectsKV:      objectsKV,
				guards:           &kvTestInvitationGuards{kv: mappingsKV},
				userReader:       tt.userReader,
				inviteSender:     sender,
				selfServeBaseURL: "https://app.dev.lfx.dev",
			}

			require.False(t, h.MaybeInvite(context.Background(), slog.Default(), voteResponseUID, email, "Guest", pollID))

			assert.Equal(t, tt.wantCalled, sender.called)
			if tt.wantCalled {
				assert.Equal(t, votingconstants.InviteRoleVoter, sender.last.Role)
				assert.Equal(t, pollID, sender.last.Resource.UID)
				assert.Equal(t, votingconstants.ResourceTypeVote, sender.last.Resource.Type)
				assert.Equal(t, "Board Election", sender.last.Resource.Name)
			}

			objectsKV.AssertExpectations(t)
			mappingsKV.AssertExpectations(t)
		})
	}
}

type faultGuards struct {
	domain.InvitationGuards
	lookupErr, claimErr, completeErr error
}

func (g faultGuards) LookupInvitation(ctx context.Context, id string) (domain.InvitationGuard, error) {
	if g.lookupErr != nil {
		return domain.InvitationGuard{}, g.lookupErr
	}
	return g.InvitationGuards.LookupInvitation(ctx, id)
}
func (g faultGuards) ClaimInvitation(ctx context.Context, id string) (uint64, error) {
	if g.claimErr != nil {
		return 0, g.claimErr
	}
	return g.InvitationGuards.ClaimInvitation(ctx, id)
}
func (g faultGuards) CompleteInvitation(ctx context.Context, id string, rev uint64, invite string) error {
	if g.completeErr != nil {
		return g.completeErr
	}
	return g.InvitationGuards.CompleteInvitation(ctx, id, rev, invite)
}

func TestMaybeInvite_GuardFailuresAndAmbiguity(t *testing.T) {
	for _, mode := range []string{"lookup failure", "claim failure", "lost claim", "send failure", "empty result", "completion failure"} {
		t.Run(mode, func(t *testing.T) {
			ns, url := startTestNATSServer(t)
			t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
			nc, err := nats.Connect(url)
			require.NoError(t, err)
			t.Cleanup(nc.Close)
			js, err := jetstream.New(nc)
			require.NoError(t, err)
			ctx := context.Background()
			objects, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "objects"})
			require.NoError(t, err)
			_, err = objects.Put(ctx, "itx-poll.one", []byte(`{"name":"Test Vote"}`))
			require.NoError(t, err)
			kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "guards"})
			require.NoError(t, err)
			base := &kvTestInvitationGuards{kv: kv}
			guards := faultGuards{InvitationGuards: base}
			sender := &stubVoteInviteSender{result: &domain.InviteResult{InviteUID: "invite-1"}}
			wantRetry, wantSend := false, false
			switch mode {
			case "lookup failure":
				guards.lookupErr = domain.NewUnavailableError("state unavailable")
				wantRetry = true
			case "claim failure":
				guards.claimErr = domain.NewUnavailableError("state unavailable")
				wantRetry = true
			case "lost claim":
				guards.claimErr = domain.ErrEventStateExists
			case "send failure":
				sender.err = errors.New("ambiguous response")
				wantSend = true
			case "empty result":
				sender.result = nil
				wantSend = true
			case "completion failure":
				guards.completeErr = domain.NewUnavailableError("state unavailable")
				wantSend = true
			}
			h := &VoteResponseInviteHandler{guards: guards, v1ObjectsKV: objects, userReader: stubVoteInviteUserReader{err: domain.ErrUserNotFound}, inviteSender: sender, selfServeBaseURL: "https://app.example.com"}
			require.Equal(t, wantRetry, h.MaybeInvite(ctx, slog.Default(), "response", "guest@example.com", "Guest", "one"))
			require.Equal(t, wantSend, sender.called)
			if wantSend {
				guard, err := base.LookupInvitation(ctx, "response")
				require.NoError(t, err)
				require.True(t, guard.Pending)
				sender.called = false
				require.False(t, h.MaybeInvite(ctx, slog.Default(), "response", "guest@example.com", "Guest", "one"))
				require.False(t, sender.called, "ambiguous dispatch must not repeat")
			}
		})
	}
}

func TestMaybeInvite_TerminalIntentResolution(t *testing.T) {
	_, state := newPostgresState(t)
	ctx := context.Background()
	for _, mode := range []string{"existing LFID", "missing email"} {
		t.Run(mode, func(t *testing.T) {
			id := strings.ReplaceAll(mode, " ", "-")
			require.NoError(t, state.PrepareInvitation(ctx, id))
			sender := &stubVoteInviteSender{}
			h := &VoteResponseInviteHandler{guards: state, userReader: stubVoteInviteUserReader{username: "existing"}, inviteSender: sender, selfServeBaseURL: "https://app.example.com"}
			email := "old@example.com"
			if mode == "missing email" {
				email = ""
			}
			require.False(t, h.MaybeInvite(ctx, slog.Default(), id, email, "Guest", "one"))
			eligible, err := h.PrepareInvite(ctx, id, false)
			require.NoError(t, err)
			require.False(t, eligible)
			h.userReader = stubVoteInviteUserReader{err: domain.ErrUserNotFound}
			eligible, err = h.PrepareInvite(ctx, id, true)
			require.NoError(t, err)
			require.False(t, eligible, "later edits/replays cannot reactivate terminal eligibility")
			require.False(t, sender.called)
		})
	}
}

func TestMaybeInvite_TerminalDecisionThenEmailEdit(t *testing.T) {
	_, state := newPostgresState(t)
	ctx := context.Background()
	require.NoError(t, state.MarkSynced(ctx, domain.MappingKindVote, "one"))
	ns, url := startTestNATSServer(t)
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	objects, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: V1ObjectsBucket})
	require.NoError(t, err)
	_, err = objects.Put(ctx, "itx-poll.one", []byte(`{"name":"Test Vote","pseudo_anonymity":false}`))
	require.NoError(t, err)
	for _, mode := range []string{"existing-lfid", "missing-email", "username-present"} {
		t.Run(mode, func(t *testing.T) {
			sender := &stubVoteInviteSender{result: &domain.InviteResult{InviteUID: "unexpected"}}
			h := &VoteResponseInviteHandler{guards: state, v1ObjectsKV: objects, userReader: stubVoteInviteUserReader{username: "existing"}, inviteSender: sender, selfServeBaseURL: "https://app.example.com"}
			syncer := eventsync.New(eventsync.Deps{Mappings: state, IDMapper: echoIDMapper{}, Publisher: infraeventing.NewNATSPublisher(nc, slog.Default()), Polls: pollPrivacyReader{objectsKV: objects, logger: slog.Default()}, Inviter: h, Logger: slog.Default()})
			data := map[string]any{"vote_id": mode, "poll_id": "one", "project_id": "project", "user_email": "old@example.com", "username": ""}
			if mode == "missing-email" {
				data["user_email"] = ""
			}
			if mode == "username-present" {
				data["username"] = "existing"
			}
			raw, err := json.Marshal(data)
			require.NoError(t, err)
			entry := eventsync.Entry{Key: "itx-poll-vote." + mode, Op: eventsync.OpPut, Value: raw}
			require.Equal(t, eventsync.Ack, syncer.Sync(ctx, entry))
			require.False(t, sender.called)
			prepared, err := state.InvitationPrepared(ctx, mode)
			require.NoError(t, err)
			require.False(t, prepared)
			h.userReader = stubVoteInviteUserReader{err: domain.ErrUserNotFound}
			data["user_email"] = "new@example.com"
			data["username"] = ""
			entry.Value, err = json.Marshal(data)
			require.NoError(t, err)
			require.Equal(t, eventsync.Ack, syncer.Sync(ctx, entry))
			require.False(t, sender.called, "updated email must not trigger a new invite")
		})
	}
}
