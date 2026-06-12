// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	natsgo "github.com/nats-io/nats.go"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

const (
	inviteAcceptedQueueGroup  = "voting-service-invite-accepted"
	inviteAcceptedCallTimeout = 30 * time.Second
)

// InviteAcceptedSubscriber subscribes to lfx.invite-service.invite_accepted events
// and calls the ITX Voting Service to enrich all vote records tied to the acceptor's
// email with their new username and profile data.
type InviteAcceptedSubscriber struct {
	nc               *natsgo.Conn
	acceptanceClient domain.InviteAcceptanceClient
	logger           *slog.Logger
	sub              *natsgo.Subscription

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewInviteAcceptedSubscriber creates a new subscriber but does not start it.
func NewInviteAcceptedSubscriber(
	nc *natsgo.Conn,
	acceptanceClient domain.InviteAcceptanceClient,
	logger *slog.Logger,
) *InviteAcceptedSubscriber {
	return &InviteAcceptedSubscriber{
		nc:               nc,
		acceptanceClient: acceptanceClient,
		logger:           logger,
	}
}

// Start registers the NATS QueueSubscribe and begins processing acceptance events.
func (s *InviteAcceptedSubscriber) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	sub, err := s.nc.QueueSubscribe(
		inviteapi.InviteServiceAcceptedSubject,
		inviteAcceptedQueueGroup,
		s.handle,
	)
	if err != nil {
		if s.cancel != nil {
			s.cancel()
		}
		return err
	}
	s.sub = sub
	s.logger.Info("invite_accepted subscriber started", "subject", inviteapi.InviteServiceAcceptedSubject)
	return nil
}

// Stop cancels in-flight handlers, drains the subscription, and waits for handlers to finish.
func (s *InviteAcceptedSubscriber) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.sub != nil {
		if err := s.sub.Drain(); err != nil {
			s.logger.With(errKey, err).Warn("error draining invite_accepted subscription")
		}
	}
	s.wg.Wait()
}

func (s *InviteAcceptedSubscriber) handle(msg *natsgo.Msg) {
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(s.ctx, inviteAcceptedCallTimeout)
	defer cancel()

	var evt inviteapi.InviteServiceAcceptedEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		s.logger.With(errKey, err).Warn("failed to parse InviteServiceAcceptedEvent; discarding")
		return
	}

	if err := processInviteAcceptedEvent(ctx, evt, s.acceptanceClient, s.logger); err != nil {
		s.logger.With(errKey, err).Warn("invite_accepted enrichment failed; best-effort, not retrying",
			"email", evt.Recipient.Email,
			"username", evt.AcceptedBy,
		)
	}
}

// processInviteAcceptedEvent validates an invite acceptance event and calls ITX to enrich
// all vote records for the acceptor's email.
func processInviteAcceptedEvent(
	ctx context.Context,
	evt inviteapi.InviteServiceAcceptedEvent,
	client domain.InviteAcceptanceClient,
	logger *slog.Logger,
) error {
	email := evt.Recipient.Email
	username := evt.AcceptedBy

	if email == "" || username == "" {
		logger.Warn("invite_accepted event missing required fields; discarding")
		return nil
	}

	logger.Debug("received invite_accepted event",
		"email", email,
		"username", username,
		"resource_type", evt.Resource.Type,
	)

	if err := client.AcceptInvite(ctx, email, username); err != nil {
		return err
	}

	logger.Info("invite_accepted enrichment complete",
		"email", email,
		"username", username,
	)
	return nil
}
