// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
)

func replyMsg(data []byte) *natsgo.Msg { return &natsgo.Msg{Data: data} }

func TestNATSUserReader_UsernameByEmail(t *testing.T) {
	tests := []struct {
		name       string
		reply      *natsgo.Msg
		replyErr   error
		wantUser   string
		wantErr    error
		wantErrStr string
	}{
		{
			name:     "plain-text username returned on success",
			reply:    replyMsg([]byte("alice")),
			wantUser: "alice",
		},
		{
			name:     "trailing newline trimmed from username",
			reply:    replyMsg([]byte("alice\n")),
			wantUser: "alice",
		},
		{
			name:    "empty body returns ErrUserNotFound",
			reply:   replyMsg([]byte("")),
			wantErr: domain.ErrUserNotFound,
		},
		{
			name:    "JSON error envelope without error field returns ErrUserNotFound",
			reply:   replyMsg([]byte(`{"success":false}`)),
			wantErr: domain.ErrUserNotFound,
		},
		{
			name:       "JSON error envelope with error field returns service error",
			reply:      replyMsg([]byte(`{"success":false,"error":"auth service unavailable"}`)),
			wantErrStr: "email_to_username failed: auth service unavailable",
		},
		{
			name:    "JSON user-not-found envelope returns ErrUserNotFound",
			reply:   replyMsg([]byte(`{"success":false,"error":"user not found"}`)),
			wantErr: domain.ErrUserNotFound,
		},
		{
			name:       "JSON envelope missing success field returns descriptive error",
			reply:      replyMsg([]byte(`{"error":"something unexpected"}`)),
			wantErrStr: "email_to_username response missing success field",
		},
		{
			name:     "JSON success envelope with username field returns username",
			reply:    replyMsg([]byte(`{"success":true,"username":"alice"}`)),
			wantUser: "alice",
		},
		{
			name:       "JSON success envelope without username field returns unexpected envelope error",
			reply:      replyMsg([]byte(`{"success":true}`)),
			wantErrStr: "unexpected email_to_username success envelope",
		},
		{
			name:       "malformed JSON object returns parse error",
			reply:      replyMsg([]byte(`{"success":"true"}`)),
			wantErrStr: "failed to parse email_to_username response",
		},
		{
			name:       "transport error is wrapped and returned",
			reply:      nil,
			replyErr:   errors.New("nats: connection closed"),
			wantErrStr: "email_to_username request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &MockRequester{}
			mockConn.On("RequestWithContext", mock.Anything, votingconstants.AuthEmailToUsernameSubject, mock.Anything).
				Return(tt.reply, tt.replyErr)

			reader := NewUserReader(mockConn, slog.Default())
			got, err := reader.UsernameByEmail(context.Background(), "test@example.com")

			switch {
			case tt.wantErr != nil:
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)
			case tt.wantErrStr != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrStr)
				assert.Empty(t, got)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.wantUser, got)
			}
			mockConn.AssertExpectations(t)
		})
	}
}
