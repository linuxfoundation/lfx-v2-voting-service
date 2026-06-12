// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

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

func TestDecodeKVData(t *testing.T) {
	t.Run("decodes JSON", func(t *testing.T) {
		payload := map[string]any{"name": "Board Election"}
		raw, err := json.Marshal(payload)
		require.NoError(t, err)

		got, err := decodeKVData(raw)
		require.NoError(t, err)
		assert.Equal(t, "Board Election", got["name"])
	})

	t.Run("decodes msgpack", func(t *testing.T) {
		payload := map[string]any{"name": "Board Election"}
		raw, err := msgpack.Marshal(payload)
		require.NoError(t, err)

		got, err := decodeKVData(raw)
		require.NoError(t, err)
		assert.Equal(t, "Board Election", got["name"])
	})

	t.Run("returns combined error when both decoders fail", func(t *testing.T) {
		_, err := decodeKVData([]byte("not-json-or-msgpack"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json:")
		assert.Contains(t, err.Error(), "msgpack:")
	})
}

func TestShouldSendVoteResponseInvite(t *testing.T) {
	assert.True(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionUpdated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "existing", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", ""))
}

func TestMaybeSendInvite(t *testing.T) {
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
				kv.On("Put", mock.Anything, inviteSentKey, []byte("invite-new")).Return(uint64(1), nil)
			},
			setupObjects: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).
					Return(mockKeyValueEntry{key: pollKey, value: pollPayload}, nil)
			},
			wantCalled: true,
		},
		{
			name:       "skips on transient auth lookup failure",
			userReader: stubVoteInviteUserReader{err: errors.New("auth unavailable")},
			setupMaps: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, inviteSentKey).Return(nil, jetstream.ErrKeyNotFound)
			},
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
				v1MappingsKV:     mappingsKV,
				userReader:       tt.userReader,
				inviteSender:     sender,
				selfServeBaseURL: "https://app.dev.lfx.dev",
			}

			h.maybeSendInvite(context.Background(), slog.Default(), voteResponseUID, email, "Guest", pollID)

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
