// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
)

// InviteResult holds the key fields returned by the invite service after an invite is sent.
type InviteResult struct {
	InviteUID      string
	RecipientEmail string
	ExpiresAt      time.Time
}

// InviteSender sends LFID invites via the invite service over NATS.
type InviteSender interface {
	SendInvite(ctx context.Context, req inviteapi.SendInviteRequest) (*InviteResult, error)
}
