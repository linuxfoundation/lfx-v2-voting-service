// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

const inviteSenderTimeout = 10 * time.Second

// NATSInviteSender implements domain.InviteSender using NATS request/reply.
type NATSInviteSender struct {
	nc     Requester
	logger *slog.Logger
}

// NewInviteSender creates a new NATS-based invite sender.
func NewInviteSender(nc Requester, logger *slog.Logger) *NATSInviteSender {
	logger.Info("invite sender initialized", "subject", inviteapi.SendInviteSubject)
	return &NATSInviteSender{nc: nc, logger: logger}
}

// SendInvite sends an LFID invite request to the invite service over NATS and
// returns the invite metadata from the reply.
func (s *NATSInviteSender) SendInvite(ctx context.Context, req inviteapi.SendInviteRequest) (*domain.InviteResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal invite request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, inviteSenderTimeout)
	defer cancel()

	msg, err := s.nc.RequestWithContext(reqCtx, inviteapi.SendInviteSubject, payload)
	if err != nil {
		return nil, fmt.Errorf("invite service request failed: %w", err)
	}

	var resp inviteapi.SendInviteResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse invite service response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("invite service returned error: %s", resp.Error)
	}
	if resp.InviteData == nil {
		return nil, fmt.Errorf("invite service returned empty response")
	}

	return &domain.InviteResult{
		InviteUID:      resp.UID,
		RecipientEmail: resp.Email,
		ExpiresAt:      resp.ExpiresAt,
	}, nil
}

var _ domain.InviteSender = (*NATSInviteSender)(nil)
