// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

// convertMapToPollResultData converts v1 poll result data to v2 format with ID mapping.
func convertMapToPollResultData(
	ctx context.Context,
	v1Data map[string]interface{},
	idMapper domain.IDMapper,
	logger *slog.Logger,
) (*domain.PollResultData, error) {
	jsonBytes, err := json.Marshal(v1Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal v1Data to JSON: %w", err)
	}

	var raw domain.PollResultDBRaw
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON into PollResultDBRaw: %w", err)
	}

	result := &domain.PollResultData{
		VoteUID:          raw.PollID,
		PollID:           raw.PollID,
		CommitteeID:      raw.CommitteeID,
		ProjectID:        raw.ProjectID,
		Status:           raw.Status,
		NumRecipients:    raw.NumRecipients,
		NumVotesCast:     raw.NumVotesCast,
		NumAbstained:     raw.NumAbstained,
		PollEndTime:      raw.PollEndTime,
		CreatedTime:      raw.CreatedTime,
		LastModifiedTime: raw.LastModifiedTime,
	}

	for _, q := range raw.PollQuestionsResult {
		question := domain.PollQuestionResult{
			QuestionID: q.QuestionID,
			Prompt:     q.Prompt,
		}
		for _, c := range q.ChoiceResults {
			question.ChoiceResults = append(question.ChoiceResults, domain.ChoiceResult(c))
		}
		result.PollQuestionsResult = append(result.PollQuestionsResult, question)
	}

	if raw.ProjectID != "" {
		projectUID, err := idMapper.MapProjectV1ToV2(ctx, raw.ProjectID)
		if err != nil {
			logger.With(errKey, err, "field", "project_id", "value", raw.ProjectID).
				WarnContext(ctx, "failed to get v2 project UID from v1 project ID")
			return nil, fmt.Errorf("failed to map project ID: %w", err)
		}
		result.ProjectUID = projectUID
	}

	if raw.CommitteeID != "" {
		committeeUID, err := idMapper.MapCommitteeV1ToV2(ctx, raw.CommitteeID)
		if err != nil {
			logger.With(errKey, err, "field", "committee_id", "value", raw.CommitteeID).
				WarnContext(ctx, "failed to get v2 committee UID from v1 committee ID")
			// Committee mapping is non-critical; continue without it.
		} else {
			result.CommitteeUID = committeeUID
		}
	}

	return result, nil
}

// prefixVoteResult is the v1-objects key prefix of poll result snapshot records.
const prefixVoteResult = "itx-poll-results"

// voteResultResource is the sync spec for vote results. A result waits for its parent vote
// to be synced and not deleted, and a tombstoned result mapping counts as a new create.
func voteResultResource() *resource[domain.PollResultData] {
	return &resource[domain.PollResultData]{
		label:               "vote result",
		handlerName:         "vote_result",
		kind:                domain.MappingKindVoteResult,
		uidLogKey:           "vote_uid",
		convert:             convertMapToPollResultData,
		retryOnConvertError: true,
		uid:                 func(r *domain.PollResultData) string { return r.VoteUID },
		missingUIDMessage:   "missing poll_id in poll result data",
		tombstoneIsCreate:   true,
		prepare: func(ctx context.Context, s *Syncer, logger *slog.Logger, r *domain.PollResultData) *Outcome {
			// Parent dependency guard: wait for the parent vote to be indexed first.
			// A tombstoned parent means the vote was deleted; treat it as unavailable.
			if s.deps.Mappings.State(ctx, domain.MappingKindVote, r.PollID) != domain.MappingSynced {
				logger.InfoContext(ctx, "parent vote not found or deleted, will retry vote result sync")
				return outcome(Retry)
			}
			return nil
		},
		publish: func(ctx context.Context, pub domain.EventPublisher, action string, r *domain.PollResultData) error {
			return pub.PublishVoteResultEvent(ctx, action, r)
		},
		deleteItem: func(_ context.Context, _ *slog.Logger, uid string, _ []byte) *domain.PollResultData {
			return &domain.PollResultData{VoteUID: uid, PollID: uid}
		},
	}
}
