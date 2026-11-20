// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service/eventsync"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats.go/jetstream"
)

// VoteResponseInviteHandler performs best-effort LFID invite sending for new vote responses.
type VoteResponseInviteHandler struct {
	inviteSender     domain.InviteSender
	userReader       domain.UserReader
	v1ObjectsKV      jetstream.KeyValue
	guards           domain.InvitationGuards
	selfServeBaseURL string
}

// inviteEnabled reports whether outbound invite sending is fully wired up.
func (h *VoteResponseInviteHandler) inviteEnabled() bool {
	return h != nil &&
		h.inviteSender != nil &&
		h.userReader != nil &&
		h.guards != nil &&
		strings.TrimSpace(h.selfServeBaseURL) != ""
}

const voteResponseLFIDInviteSentKeyFmt = "v1_vote_response_lfid_invite_sent.%s"

func voteResponseLFIDInviteSentKey(voteResponseUID string) string {
	return fmt.Sprintf(voteResponseLFIDInviteSentKeyFmt, voteResponseUID)
}

// MaybeInvite performs a best-effort LFID invite for a new vote-response participant
// who has no username. Pre-dispatch storage failures retry; ambiguous dispatch
// outcomes retain the guard for manual reconciliation.
func (h *VoteResponseInviteHandler) MaybeInvite(
	ctx context.Context,
	logger *slog.Logger,
	voteResponseUID, email, displayName, pollID string,
) (retry bool) {
	if !h.inviteEnabled() {
		return false
	}

	email = strings.TrimSpace(email)
	if email == "" {
		return h.resolveIntent(ctx, logger, voteResponseUID)
	}

	guard, err := h.guards.LookupInvitation(ctx, voteResponseUID)
	if err != nil {
		logger.WarnContext(ctx, "failed to read invitation guard; retrying before dispatch")
		return true
	}
	if guard.Exists {
		logger.DebugContext(ctx, "LFID invite already sent for vote response, skipping")
		return false
	}

	username, err := h.userReader.UsernameByEmail(ctx, email)
	if err == nil && username != "" {
		logger.DebugContext(ctx, "vote response participant already has LFID, skipping invite")
		return h.resolveIntent(ctx, logger, voteResponseUID)
	}
	if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		// Transient auth-service error: fall through and attempt the invite anyway.
		// Skipping here would permanently lose the invite opportunity — the KV mapping
		// is already stored so this message won't be redelivered as ActionCreated.
		// The invite service handles the edge case where the user already has an LFID.
		logger.With(errKey, err).WarnContext(ctx, "failed to check LFID for vote response; proceeding with invite as best-effort")
	}

	var voteName string
	pollKey := fmt.Sprintf("itx-poll.%s", pollID)
	if entry, kvErr := h.v1ObjectsKV.Get(ctx, pollKey); kvErr == nil {
		if data, decErr := eventsync.DecodeRecord(entry.Value()); decErr == nil {
			if name, ok := data["name"].(string); ok {
				voteName = strings.TrimSpace(name)
			}
		}
	}
	if voteName == "" {
		logger.WarnContext(ctx, "could not resolve vote name; skipping invite to avoid confusing email")
		return false
	}

	returnURL := fmt.Sprintf("%s/votes/%s", strings.TrimRight(h.selfServeBaseURL, "/"), url.PathEscape(pollID))
	name := strings.TrimSpace(displayName)
	req := inviteapi.SendInviteRequest{
		Recipient: &inviteapi.Recipient{
			Email: email,
			Name:  name,
		},
		Resource: &inviteapi.Resource{
			UID:  pollID,
			Name: voteName,
			Type: votingconstants.ResourceTypeVote,
		},
		Role:           votingconstants.InviteRoleVoter,
		ReturnURL:      returnURL,
		ExpirationDays: 30,
	}

	// Write a "pending" marker before calling SendInvite to close the duplicate-invite
	// window: a concurrent redelivery that passes the Get check above would also see
	// this marker and skip, preventing two goroutines from both calling SendInvite.
	revision, err := h.guards.ClaimInvitation(ctx, voteResponseUID)
	if err != nil {
		logger.With(errKey, err).WarnContext(ctx, "failed to store pending invite marker; skipping to avoid duplicate")
		return !errors.Is(err, domain.ErrEventStateExists)
	}

	if err := domain.CheckSourceFence(ctx); err != nil {
		logger.With(errKey, err).WarnContext(ctx, "source changed or unavailable after invite claim; retaining claim for reconciliation")
		return !errors.Is(err, domain.ErrSourceSuperseded)
	}
	result, sendErr := h.inviteSender.SendInvite(ctx, req)
	if sendErr != nil {
		logger.With(errKey, sendErr).WarnContext(ctx, "failed to send LFID invite for vote response; continuing")
		return false
	}
	if result == nil || result.InviteUID == "" {
		logger.WarnContext(ctx, "invite result unresolved; retaining dispatch claim")
		return false
	}
	if err := h.guards.CompleteInvitation(ctx, voteResponseUID, revision, result.InviteUID); err != nil {
		logger.With(errKey, err).WarnContext(ctx, "failed to update vote response LFID invite sent marker")
	}
	logger.InfoContext(ctx, "sent LFID invite for vote response",
		"invite_uid", result.InviteUID,
		"expires_at", result.ExpiresAt,
	)
	return false
}

// ResolveInvite records terminal eligibility without removing dispatch guards.
func (h *VoteResponseInviteHandler) ResolveInvite(ctx context.Context, id string) error {
	if !h.inviteEnabled() {
		return nil
	}
	return h.guards.ResolveInvitation(ctx, id)
}
func (h *VoteResponseInviteHandler) resolveIntent(ctx context.Context, logger *slog.Logger, id string) bool {
	if err := h.ResolveInvite(ctx, id); err != nil {
		logger.With(errKey, err).WarnContext(ctx, "failed to resolve terminal invitation intent; retrying")
		return true
	}
	return false
}

// PrepareInvite persists new-response intent before processed-state writes and
// reads that intent on replay; imported historical responses acquire no intent.
func (h *VoteResponseInviteHandler) PrepareInvite(ctx context.Context, id string, firstProcessing bool) (bool, error) {
	if !h.inviteEnabled() {
		return false, nil
	}
	if firstProcessing {
		if err := h.guards.PrepareInvitation(ctx, id); err != nil {
			return false, err
		}
	}
	return h.guards.InvitationPrepared(ctx, id)
}
