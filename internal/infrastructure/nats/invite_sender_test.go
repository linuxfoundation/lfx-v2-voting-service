// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNATSInviteSender_SendInvite(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	expires := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	req := inviteapi.SendInviteRequest{Recipient: &inviteapi.Recipient{Email: "alice@example.com", Name: "Alice Example"}}

	reply := func(v any) *natsgo.Msg {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return &natsgo.Msg{Data: b}
	}

	t.Run("publishes the request on the invite subject and maps the reply", func(t *testing.T) {
		nc := &MockRequester{}
		nc.On("RequestWithContext", mock.Anything, inviteapi.SendInviteSubject, mock.Anything).
			Return(reply(inviteapi.SendInviteResponse{InviteData: &inviteapi.InviteData{
				UID: "invite-1", Email: "alice@example.com", ExpiresAt: expires,
			}}), nil).Once()
		s := NewInviteSender(nc, logger)

		got, err := s.SendInvite(context.Background(), req)

		require.NoError(t, err)
		assert.Equal(t, "invite-1", got.InviteUID)
		assert.Equal(t, "alice@example.com", got.RecipientEmail)
		assert.True(t, expires.Equal(got.ExpiresAt))

		var sent inviteapi.SendInviteRequest
		payload := nc.Calls[0].Arguments.Get(2).([]byte)
		require.NoError(t, json.Unmarshal(payload, &sent))
		assert.Equal(t, req, sent)
		nc.AssertExpectations(t)
	})

	t.Run("applies a request timeout to the call", func(t *testing.T) {
		nc := &MockRequester{}
		var deadline time.Time
		nc.On("RequestWithContext", mock.MatchedBy(func(ctx context.Context) bool {
			d, ok := ctx.Deadline()
			deadline = d
			return ok
		}), inviteapi.SendInviteSubject, mock.Anything).
			Return(reply(inviteapi.SendInviteResponse{InviteData: &inviteapi.InviteData{UID: "i"}}), nil).Once()

		_, err := NewInviteSender(nc, logger).SendInvite(context.Background(), req)

		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(inviteSenderTimeout), deadline, 2*time.Second)
	})

	failures := []struct {
		name    string
		reply   *natsgo.Msg
		err     error
		wantErr string
	}{
		{name: "transport failure", err: errors.New("no responders"), wantErr: "invite service request failed: no responders"},
		{name: "malformed reply", reply: &natsgo.Msg{Data: []byte("not json")}, wantErr: "failed to parse invite service response"},
		{name: "service error in reply", reply: reply(inviteapi.SendInviteResponse{Error: "template missing"}), wantErr: "invite service returned error: template missing"},
		{name: "reply with neither data nor error", reply: reply(map[string]any{}), wantErr: "invite service returned empty response"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			nc := &MockRequester{}
			nc.On("RequestWithContext", mock.Anything, inviteapi.SendInviteSubject, mock.Anything).
				Return(tt.reply, tt.err).Once()

			got, err := NewInviteSender(nc, logger).SendInvite(context.Background(), req)

			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("transport error is wrapped so callers can inspect it", func(t *testing.T) {
		cause := errors.New("connection closed")
		nc := &MockRequester{}
		nc.On("RequestWithContext", mock.Anything, mock.Anything, mock.Anything).Return(nil, cause).Once()

		_, err := NewInviteSender(nc, logger).SendInvite(context.Background(), req)

		assert.ErrorIs(t, err, cause)
	})
}
