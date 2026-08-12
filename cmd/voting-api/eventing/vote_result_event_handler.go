// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

// handleVoteResultUpdate processes a poll result snapshot update from itx-poll-results records.
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK).
func handleVoteResultUpdate(
	ctx context.Context,
	key string,
	v1Data map[string]interface{},
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	funcLogger := logger.With("key", key, "handler", "vote_result")

	funcLogger.DebugContext(ctx, "processing vote result update")

	pollResultData, err := convertMapToPollResultData(ctx, v1Data, idMapper, funcLogger)
	if err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to convert v1Data to poll result")
		return isTransientError(err)
	}

	if pollResultData.VoteUID == "" {
		funcLogger.ErrorContext(ctx, "missing poll_id in poll result data")
		return false
	}
	funcLogger = funcLogger.With("vote_uid", pollResultData.VoteUID)

	// Parent dependency guard: wait for the parent vote to be indexed first.
	// A tombstoned parent means the vote was deleted; treat it as unavailable.
	voteMappingKey := fmt.Sprintf("vote.%s", pollResultData.PollID)
	if entry, err := mappingsKV.Get(ctx, voteMappingKey); err != nil || isTombstonedMapping(entry.Value()) {
		funcLogger.InfoContext(ctx, "parent vote not found or deleted, will retry vote result sync")
		return true
	}

	mappingKey := fmt.Sprintf("vote_result.%s", pollResultData.VoteUID)
	indexerAction := indexerConstants.ActionCreated
	// A tombstoned mapping means the vote_result was previously deleted; treat as a new create.
	if entry, err := mappingsKV.Get(ctx, mappingKey); err == nil && !isTombstonedMapping(entry.Value()) {
		indexerAction = indexerConstants.ActionUpdated
	}

	if err := publisher.PublishVoteResultEvent(ctx, string(indexerAction), pollResultData); err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to publish vote result event")
		return isTransientError(err)
	}

	if _, err := mappingsKV.Put(ctx, mappingKey, []byte("1")); err != nil {
		funcLogger.With(errKey, err).WarnContext(ctx, "failed to store vote result mapping")
	}

	funcLogger.InfoContext(ctx, "successfully sent vote result indexer message")
	return false
}

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
			voteCount, err := domain.CoerceToInt(c.VoteCount, "vote_count")
			if err != nil {
				return nil, fmt.Errorf("question %s choice %s: invalid vote_count: %w", q.QuestionID, c.ChoiceID, err)
			}
			percentage, err := domain.CoerceToFloat64(c.Percentage, "percentage")
			if err != nil {
				return nil, fmt.Errorf("question %s choice %s: invalid percentage: %w", q.QuestionID, c.ChoiceID, err)
			}
			question.ChoiceResults = append(question.ChoiceResults, domain.ChoiceResult{
				ChoiceID:   c.ChoiceID,
				ChoiceText: c.ChoiceText,
				VoteCount:  voteCount,
				Percentage: percentage,
			})
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

// handleVoteResultDelete processes a poll result delete from itx-poll-results records.
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK).
func handleVoteResultDelete(
	ctx context.Context,
	uid string,
	publisher domain.EventPublisher,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	funcLogger := logger.With("vote_uid", uid, "handler", "vote_result_delete")

	funcLogger.DebugContext(ctx, "processing vote result delete")

	mappingKey := fmt.Sprintf("vote_result.%s", uid)
	if entry, err := mappingsKV.Get(ctx, mappingKey); err == nil && isTombstonedMapping(entry.Value()) {
		funcLogger.DebugContext(ctx, "vote result delete already processed, skipping")
		return false
	}

	pollResultData := &domain.PollResultData{
		VoteUID: uid,
		PollID:  uid,
	}

	if err := publisher.PublishVoteResultEvent(ctx, string(indexerConstants.ActionDeleted), pollResultData); err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to publish vote result delete event")
		return isTransientError(err)
	}

	if _, err := mappingsKV.Put(ctx, mappingKey, []byte(tombstoneMarker)); err != nil {
		funcLogger.With(errKey, err).WarnContext(ctx, "failed to tombstone vote result mapping")
	}

	funcLogger.InfoContext(ctx, "successfully sent vote result delete indexer message")
	return false
}
