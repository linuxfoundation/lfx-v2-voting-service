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
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"github.com/nats-io/nats.go/jetstream"
)

// handleVoteResponseUpdate processes a vote response update from itx-poll-vote records
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK)
func handleVoteResponseUpdate(
	ctx context.Context,
	key string,
	v1Data map[string]interface{},
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	funcLogger := logger.With("key", key, "handler", "vote_response")

	funcLogger.DebugContext(ctx, "processing vote response update")

	// Convert v1Data map to vote response data with proper v2 format
	voteResponseData, err := convertMapToVoteResponseData(ctx, v1Data, idMapper, funcLogger)
	if err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to convert v1Data to vote response")
		return false // Permanent error, ACK and skip
	}

	// Extract the vote response UID
	if voteResponseData.UID == "" {
		funcLogger.ErrorContext(ctx, "missing or invalid uid in vote response data")
		return false // Permanent error, ACK and skip
	}
	funcLogger = funcLogger.With("vote_response_id", voteResponseData.UID)

	// Check if parent vote exists in mappings before proceeding
	if voteResponseData.PollID == "" {
		funcLogger.ErrorContext(ctx, "vote response missing required parent vote ID")
		return false // Permanent error, ACK and skip
	}
	funcLogger = funcLogger.With("poll_id", voteResponseData.PollID)
	voteMappingKey := fmt.Sprintf("vote.%s", voteResponseData.PollID)
	if _, err := mappingsKV.Get(ctx, voteMappingKey); err != nil {
		funcLogger.With(errKey, err).InfoContext(ctx, "parent vote not found in mappings, will retry vote response sync")
		return true // NAK for retry - parent vote hasn't been processed yet
	}

	// Determine action (created vs updated) by checking if mapping exists
	mappingKey := fmt.Sprintf("vote_response.%s", voteResponseData.UID)
	indexerAction := indexerConstants.ActionCreated
	if _, err := mappingsKV.Get(ctx, mappingKey); err == nil {
		indexerAction = indexerConstants.ActionUpdated
	}

	// Publish to indexer and FGA-sync
	if err := publisher.PublishVoteResponseEvent(ctx, string(indexerAction), voteResponseData); err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to publish vote response event")
		// Check if this is a transient error that should be retried
		if isTransientError(err) {
			return true // NAK for retry
		}
		return false // Permanent error, ACK and skip
	}

	// Store mapping to track that we've seen this vote response
	if _, err := mappingsKV.Put(ctx, mappingKey, []byte("1")); err != nil {
		funcLogger.With(errKey, err).WarnContext(ctx, "failed to store vote response mapping")
		// Don't retry on mapping storage failures
	}

	funcLogger.InfoContext(ctx, "successfully sent vote response indexer and access messages")
	return false // Success, ACK the message
}

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

	// Build v2 vote response data struct
	voteResponseData := &domain.VoteResponseData{
		UID:                     voteDB.VoteID,
		VoteID:                  voteDB.VoteID,
		VoteUID:                 voteDB.PollID, // poll_id becomes vote_uid in v2
		PollID:                  voteDB.PollID,
		ProjectID:               voteDB.ProjectID,
		VoteCreationTime:        voteDB.VoteCreationTime,
		UserID:                  voteDB.UserID,
		UserEmail:               voteDB.UserEmail,
		UserRole:                voteDB.UserRole,
		UserName:                voteDB.UserName,
		Username:                voteDB.Username, // Use UserName as username for now
		ProfilePicture:          voteDB.ProfilePicture,
		UserVotingStatus:        voteDB.UserVotingStatus,
		UserOrgID:               voteDB.UserOrgID,
		UserOrgName:             voteDB.UserOrgName,
		PollAnswers:             pollAnswers,
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

// handleVoteResponseDelete processes a vote response delete from itx-poll-vote records
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK)
func handleVoteResponseDelete(
	ctx context.Context,
	uid string,
	publisher domain.EventPublisher,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	funcLogger := logger.With("vote_response_uid", uid, "handler", "vote_response_delete")

	funcLogger.DebugContext(ctx, "processing vote response delete")

	// Skip if already tombstoned — prevents duplicate delete events on redelivery
	mappingKey := fmt.Sprintf("vote_response.%s", uid)
	if entry, err := mappingsKV.Get(ctx, mappingKey); err == nil && isTombstonedMapping(entry.Value()) {
		funcLogger.DebugContext(ctx, "vote response delete already processed, skipping")
		return false
	}

	// Create minimal vote response data for delete event
	voteResponseData := &domain.VoteResponseData{
		UID:    uid,
		VoteID: uid,
	}

	// Publish delete event to indexer and FGA-sync
	if err := publisher.PublishVoteResponseEvent(ctx, string(indexerConstants.ActionDeleted), voteResponseData); err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to publish vote response delete event")
		// Check if this is a transient error that should be retried
		if isTransientError(err) {
			return true // NAK for retry
		}
		return false // Permanent error, ACK and skip
	}

	// Tombstone mapping instead of hard-deleting, so redelivery is safely skipped
	if _, err := mappingsKV.Put(ctx, mappingKey, []byte(tombstoneMarker)); err != nil {
		funcLogger.With(errKey, err).WarnContext(ctx, "failed to tombstone vote response mapping")
		// Don't retry on mapping failures
	}

	funcLogger.InfoContext(ctx, "successfully sent vote response delete indexer and access messages")
	return false // Success, ACK the message
}
