// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// convertMapToVoteResponseData converts v1 vote response data to v2 format with proper types and UIDs
func convertMapToVoteResponseData(
	ctx context.Context,
	v1Data map[string]interface{},
	idMapper domain.IDMapper,
	logger *slog.Logger,
) (*domain.VoteResponseData, error) {
	// Convert map to JSON bytes, then to VoteDBRaw to handle string/raw fields
	jsonBytes, err := json.Marshal(v1Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal v1Data to JSON: %w", err)
	}

	var voteDB domain.VoteDBRaw
	if err := json.Unmarshal(jsonBytes, &voteDB); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON into VoteDBRaw: %w", err)
	}

	// Convert PollAnswerRaw (string choice_rank) to proper format (int choice_rank)
	pollAnswers := []itx.PollAnswer{}
	for _, rawAnswer := range voteDB.PollAnswers {
		pollAnswer := itx.PollAnswer{
			QuestionID: rawAnswer.QuestionID,
			Prompt:     rawAnswer.Prompt,
			Type:       rawAnswer.Type,
			UserChoice: rawAnswer.UserChoice, // Already correct type
		}

		// Convert ranked choices from string to int choice_rank
		rankedChoices := []itx.RankedPollChoiceAnswer{}
		for _, rc := range rawAnswer.RankedUserChoice {
			rankedChoice := itx.RankedPollChoiceAnswer{
				ChoiceID:   rc.ChoiceID,
				ChoiceText: rc.ChoiceText,
				ChoiceRank: rc.ChoiceRank,
			}

			rankedChoices = append(rankedChoices, rankedChoice)
		}
		pollAnswer.RankedUserChoice = rankedChoices

		pollAnswers = append(pollAnswers, pollAnswer)
	}

	// Default to an empty array so the indexer payload emits [] rather than null
	commentResponses := voteDB.CommentResponses
	if commentResponses == nil {
		commentResponses = []itx.CommentResponse{}
	}

	// Build v2 vote response data struct
	voteResponseData := &domain.VoteResponseData{
		UID:                     voteDB.VoteID,
		VoteID:                  voteDB.VoteID,
		VoteUID:                 voteDB.PollID, // poll_id becomes vote_uid in v2
		PollID:                  voteDB.PollID,
		ProjectID:               voteDB.ProjectID,
		VoteCreationTime:        voteDB.VoteCreationTime,
		LastModifiedTime:        voteDB.LastModifiedTime,
		UserID:                  voteDB.UserID,
		UserEmail:               strings.ToLower(voteDB.UserEmail), // GH #3063: normalize case — query-service term clauses are case-sensitive; also used for invite lookup/send
		UserRole:                voteDB.UserRole,
		UserName:                voteDB.UserName,
		Username:                voteDB.Username,
		ProfilePicture:          voteDB.ProfilePicture,
		UserVotingStatus:        voteDB.UserVotingStatus,
		UserOrgID:               voteDB.UserOrgID,
		UserOrgName:             voteDB.UserOrgName,
		PollAnswers:             pollAnswers,
		CommentResponses:        commentResponses,
		VoteStatus:              voteDB.VoteStatus,
		Abstained:               voteDB.Abstained,
		VoterRemoved:            voteDB.VoterRemoved,
		SESMessageID:            voteDB.SESMessageID,
		SESMessageLastSentTime:  voteDB.SESMessageLastSentTime,
		SESBounceType:           voteDB.SESBounceType,
		SESBounceSubtype:        voteDB.SESBounceSubtype,
		SESDeliverySuccessful:   voteDB.SESDeliverySuccessful,
		SESComplaintExists:      voteDB.SESComplaintExists,
		SESComplaintType:        voteDB.SESComplaintType,
		SESComplaintDate:        voteDB.SESComplaintDate,
		SESEmailOpened:          voteDB.SESEmailOpened,
		SESEmailOpenedFirstTime: voteDB.SESEmailOpenedFirstTime,
		SESEmailOpenedLastTime:  voteDB.SESEmailOpenedLastTime,
		SESLinkClicked:          voteDB.SESLinkClicked,
		SESLinkClickedFirstTime: voteDB.SESLinkClickedFirstTime,
		SESLinkClickedLastTime:  voteDB.SESLinkClickedLastTime,
	}

	// Map v1 project ID (SFID) to v2 project UID
	if voteDB.ProjectID != "" {
		projectUID, err := idMapper.MapProjectV1ToV2(ctx, voteDB.ProjectID)
		if err != nil {
			logger.With(errKey, err, "field", "project_id", "value", voteDB.ProjectID).
				WarnContext(ctx, "failed to get v2 project UID from v1 project ID")
			// Return error so caller can decide to retry or skip based on error type
			return nil, fmt.Errorf("failed to map project ID: %w", err)
		}
		voteResponseData.ProjectUID = projectUID
	}

	return voteResponseData, nil
}

// prefixVoteResponse is the v1-objects key prefix of vote response (ballot) records.
const prefixVoteResponse = "itx-poll-vote"

// voteResponseResource is the sync spec for vote responses. A response waits for its
// parent vote to be synced (a tombstoned parent is accepted), is redacted when the
// parent poll is pseudo-anonymous, and may trigger an LFID invite when newly created.
// Conversion errors are always permanent.
func voteResponseResource() *resource[domain.VoteResponseData] {
	return &resource[domain.VoteResponseData]{
		label:             "vote response",
		handlerName:       "vote_response",
		kind:              domain.MappingKindVoteResponse,
		uidLogKey:         "vote_response_id",
		deleteUIDLogKey:   "vote_response_uid",
		convert:           convertMapToVoteResponseData,
		uid:               func(v *domain.VoteResponseData) string { return v.UID },
		missingUIDMessage: "missing or invalid uid in vote response data",
		prepare: func(ctx context.Context, s *Syncer, logger *slog.Logger, v *domain.VoteResponseData) *Outcome {
			if v.PollID == "" {
				logger.ErrorContext(ctx, "vote response missing required parent vote ID")
				return outcome(Ack) // permanent: ACK and skip
			}
			logger = logger.With("poll_id", v.PollID)

			if s.deps.Mappings.State(ctx, domain.MappingKindVote, v.PollID) == domain.MappingAbsent {
				logger.InfoContext(ctx, "parent vote not found in mappings, will retry vote response sync")
				return outcome(Retry) // parent vote hasn't been processed yet
			}

			// Pseudo-anonymous votes must not index the voter's choices next to their identity.
			// Fail closed: without a reader every poll is treated as pseudo-anonymous.
			pseudoAnonymous := true
			if s.deps.Polls != nil {
				var err error
				pseudoAnonymous, err = s.deps.Polls.IsPseudoAnonymous(ctx, v.PollID)
				if err != nil {
					logger.With(errKey, err).ErrorContext(ctx, "failed to read parent poll privacy setting, will retry vote response sync")
					return outcome(Retry)
				}
			}
			if pseudoAnonymous {
				v.RedactBallot()
				logger.DebugContext(ctx, "redacted ballot for pseudo-anonymous vote response", "ballot_redacted", true)
			}
			return nil
		},
		publish: func(ctx context.Context, pub domain.EventPublisher, action string, v *domain.VoteResponseData) error {
			return pub.PublishVoteResponseEvent(ctx, action, v)
		},
		afterPublish: func(ctx context.Context, s *Syncer, logger *slog.Logger, action indexerConstants.MessageAction, v *domain.VoteResponseData) {
			if s.deps.Inviter != nil && shouldSendVoteResponseInvite(action, v.Username, v.UserEmail) {
				s.deps.Inviter.MaybeInvite(ctx, logger, v.UID, v.UserEmail, v.UserName, v.PollID)
			}
		},
		deleteItem: func(ctx context.Context, logger *slog.Logger, uid string, value []byte) *domain.VoteResponseData {
			item := &domain.VoteResponseData{UID: uid, VoteID: uid}
			// A soft delete still carries the record (voter and vote); a hard delete or purge
			// does not. The record lets the delete also remove the voter's participant tuple
			// on the parent vote, which delete_access on the response does not touch.
			if rec, err := decodeVoteResponseRecord(value); err == nil {
				item.VoteUID = rec.pollID
				item.PollID = rec.pollID
				item.Username = rec.username
			} else {
				logger.DebugContext(ctx, "delete event carries no record, cannot remove the vote participant tuple")
			}
			return item
		},
	}
}

// shouldSendVoteResponseInvite reports whether a new no-LFID vote response should trigger an invite.
func shouldSendVoteResponseInvite(indexerAction indexerConstants.MessageAction, username, email string) bool {
	return indexerAction == indexerConstants.ActionCreated &&
		strings.TrimSpace(username) == "" &&
		strings.TrimSpace(email) != ""
}

// voteResponseRecord is the voter identity the delete path reads from an itx-poll-vote record.
type voteResponseRecord struct {
	pollID   string
	username string
}

// decodeVoteResponseRecord decodes a KV value (JSON or msgpack) with the same string/bool
// coercion as the live handler, since Meltano sends some fields as strings. A hard delete or
// purge has an empty value and returns an error.
func decodeVoteResponseRecord(value []byte) (voteResponseRecord, error) {
	v1Data, err := DecodeRecord(value)
	if err != nil {
		return voteResponseRecord{}, err
	}
	raw, err := json.Marshal(v1Data)
	if err != nil {
		return voteResponseRecord{}, err
	}
	var rec domain.VoteDBRaw
	if err := json.Unmarshal(raw, &rec); err != nil {
		return voteResponseRecord{}, err
	}
	return voteResponseRecord{pollID: rec.PollID, username: rec.Username}, nil
}
