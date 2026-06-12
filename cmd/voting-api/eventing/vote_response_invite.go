// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

// VoteResponseInviteHandler performs best-effort LFID invite sending for new vote responses.
type VoteResponseInviteHandler struct {
	inviteSender     domain.InviteSender
	userReader       domain.UserReader
	v1ObjectsKV      jetstream.KeyValue
	v1MappingsKV     jetstream.KeyValue
	selfServeBaseURL string
}

// inviteEnabled reports whether outbound invite sending is fully wired up.
func (h *VoteResponseInviteHandler) inviteEnabled() bool {
	return h != nil &&
		h.inviteSender != nil &&
		h.userReader != nil &&
		strings.TrimSpace(h.selfServeBaseURL) != ""
}

const voteResponseLFIDInviteSentKeyFmt = "v1_vote_response_lfid_invite_sent.%s"

func voteResponseLFIDInviteSentKey(voteResponseUID string) string {
	return fmt.Sprintf(voteResponseLFIDInviteSentKeyFmt, voteResponseUID)
}

// maybeSendInvite performs a best-effort LFID invite for a new vote-response participant
// who has no username. All errors are logged and swallowed.
func (h *VoteResponseInviteHandler) maybeSendInvite(
	ctx context.Context,
	logger *slog.Logger,
	voteResponseUID, email, displayName, pollID string,
) {
	if !h.inviteEnabled() {
		return
	}

	email = strings.TrimSpace(email)
	if email == "" {
		return
	}

	inviteSentKey := voteResponseLFIDInviteSentKey(voteResponseUID)
	if _, err := h.v1MappingsKV.Get(ctx, inviteSentKey); err == nil {
		logger.DebugContext(ctx, "LFID invite already sent for vote response, skipping")
		return
	} else if !errors.Is(err, jetstream.ErrKeyNotFound) {
		logger.With(errKey, err).WarnContext(ctx, "failed to check LFID invite sent marker; skipping invite")
		return
	}

	username, err := h.userReader.UsernameByEmail(ctx, email)
	if err == nil && username != "" {
		logger.DebugContext(ctx, "vote response participant already has LFID, skipping invite")
		return
	}
	if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		logger.With(errKey, err).WarnContext(ctx, "failed to check LFID for vote response; skipping invite")
		return
	}

	var voteName string
	pollKey := fmt.Sprintf("itx-poll.%s", pollID)
	if entry, kvErr := h.v1ObjectsKV.Get(ctx, pollKey); kvErr == nil {
		if data, decErr := decodeKVData(entry.Value()); decErr == nil {
			if name, ok := data["name"].(string); ok {
				voteName = strings.TrimSpace(name)
			}
		}
	}
	if voteName == "" {
		logger.WarnContext(ctx, "could not resolve vote name; skipping invite to avoid confusing email")
		return
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

	result, sendErr := h.inviteSender.SendInvite(ctx, req)
	if sendErr != nil {
		logger.With(errKey, sendErr).WarnContext(ctx, "failed to send LFID invite for vote response; continuing")
		return
	}
	if _, err := h.v1MappingsKV.Put(ctx, inviteSentKey, []byte(result.InviteUID)); err != nil {
		logger.With(errKey, err).WarnContext(ctx, "failed to store vote response LFID invite sent marker")
	}
	logger.InfoContext(ctx, "sent LFID invite for vote response",
		"invite_uid", result.InviteUID,
		"expires_at", result.ExpiresAt,
	)
}

func decodeKVData(data []byte) (map[string]any, error) {
	var jsonResult map[string]any
	jsonErr := json.Unmarshal(data, &jsonResult)
	if jsonErr == nil {
		return jsonResult, nil
	}

	var msgpackResult map[string]any
	msgpackErr := msgpack.Unmarshal(data, &msgpackResult)
	if msgpackErr == nil {
		return msgpackResult, nil
	}

	return nil, fmt.Errorf("failed to decode KV data as JSON or msgpack: json: %w; msgpack: %w", jsonErr, msgpackErr)
}

// shouldSendVoteResponseInvite reports whether a new no-LFID vote response should trigger an invite.
func shouldSendVoteResponseInvite(indexerAction indexerConstants.MessageAction, username, email string) bool {
	return indexerAction == indexerConstants.ActionCreated &&
		strings.TrimSpace(username) == "" &&
		strings.TrimSpace(email) != ""
}
