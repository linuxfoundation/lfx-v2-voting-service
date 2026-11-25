// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
)

// convertMapToVoteData converts v1 poll data to v2 format with proper types and UIDs
func convertMapToVoteData(
	ctx context.Context,
	v1Data map[string]interface{},
	idMapper domain.IDMapper,
	logger *slog.Logger,
) (*domain.VoteData, error) {
	// Convert map to JSON bytes, then to PollDBRaw to handle string fields
	jsonBytes, err := json.Marshal(v1Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal v1Data to JSON: %w", err)
	}

	var pollDB domain.PollDBRaw
	if err := json.Unmarshal(jsonBytes, &pollDB); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON into PollDBRaw: %w", err)
	}

	// Default to an empty array so the indexer payload emits [] rather than null
	pollCommentPrompts := pollDB.PollCommentPrompts
	if pollCommentPrompts == nil {
		pollCommentPrompts = []itx.PollCommentPromptOutput{}
	}

	// Build v2 vote data struct
	voteData := &domain.VoteData{
		VoteUID:                       pollDB.PollID,
		PollID:                        pollDB.PollID,
		Name:                          pollDB.Name,
		Description:                   pollDB.Description,
		CreationTime:                  pollDB.CreationTime,
		LastModifiedTime:              pollDB.LastModifiedTime,
		EndTime:                       pollDB.EndTime,
		EndTimeTimezone:               pollDB.EndTimeTimezone,
		EarlyEndTime:                  utils.NormalizeTimestamp(pollDB.EarlyEndTime),
		Status:                        pollDB.Status,
		ProjectID:                     pollDB.ProjectID,
		ProjectName:                   pollDB.ProjectName,
		CommitteeID:                   pollDB.CommitteeID,
		CommitteeName:                 pollDB.CommitteeName,
		CommitteeType:                 pollDB.CommitteeType,
		CommitteeVotingStatus:         pollDB.CommitteeVotingStatus,
		CommitteeFilters:              pollDB.CommitteeFilters,
		TotalVotingRequestInvitations: pollDB.TotalVotingRequestInvitations,
		PollQuestions:                 pollDB.PollQuestions,
		PollCommentPrompts:            pollCommentPrompts,
		NumResponseReceived:           pollDB.NumResponseReceived,
		PollType:                      pollDB.PollType,
		PseudoAnonymity:               pollDB.PseudoAnonymity,
		NumWinners:                    pollDB.NumWinners,
		AllowAbstain:                  pollDB.AllowAbstain,
		AllowVoteChange:               itx.VoteChangeAllowed(pollDB.AllowVoteChange),
	}

	// Map v1 project ID (SFID) to v2 project UID
	if pollDB.ProjectID != "" {
		projectUID, err := idMapper.MapProjectV1ToV2(ctx, pollDB.ProjectID)
		if err != nil {
			logger.With(errKey, err, "field", "project_id", "value", pollDB.ProjectID).
				WarnContext(ctx, "failed to get v2 project UID from v1 project ID")
			// Return error so caller can decide to retry or skip based on error type
			return nil, fmt.Errorf("failed to map project ID: %w", err)
		}
		voteData.ProjectUID = projectUID
	}

	// Map v1 committee ID (SFID) to v2 committee UID
	if pollDB.CommitteeID != "" {
		committeeUID, err := idMapper.MapCommitteeV1ToV2(ctx, pollDB.CommitteeID)
		if err != nil {
			logger.With(errKey, err, "field", "committee_id", "value", pollDB.CommitteeID).
				WarnContext(ctx, "failed to get v2 committee UID from v1 committee ID")
			// Committee mapping is not critical, just log and continue
		} else {
			voteData.CommitteeUID = committeeUID
		}
	}

	return voteData, nil
}

// prefixVote is the v1-objects key prefix of vote (poll) records.
const prefixVote = "itx-poll"

// voteResource is the sync spec for votes. A vote waits on nothing, but is skipped when
// its project has no v2 mapping.
func voteResource() *resource[domain.VoteData] {
	return &resource[domain.VoteData]{
		label:               "vote",
		handlerName:         "vote",
		kind:                domain.MappingKindVote,
		uidLogKey:           "vote_uid",
		convert:             convertMapToVoteData,
		retryOnConvertError: true,
		uid:                 func(v *domain.VoteData) string { return v.VoteUID },
		missingUIDMessage:   "missing or invalid uid in vote data",
		prepare: func(ctx context.Context, _ *Syncer, logger *slog.Logger, v *domain.VoteData) *Outcome {
			if v.ProjectUID == "" {
				logger.With("project_id", v.ProjectID).InfoContext(ctx, "skipping vote sync - parent project not found in mappings")
				return outcome(Ack) // permanent: ACK and skip
			}
			return nil
		},
		publish: func(ctx context.Context, pub domain.EventPublisher, action string, v *domain.VoteData) error {
			return pub.PublishVoteEvent(ctx, action, v)
		},
		deleteItem: func(_ context.Context, _ *slog.Logger, uid string, _ []byte) *domain.VoteData {
			return &domain.VoteData{VoteUID: uid, PollID: uid}
		},
	}
}
