// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
	"github.com/nats-io/nats.go/jetstream"
)

const errKey = "error"

// handleVoteUpdate processes a vote (poll) update from itx-poll records
// Returns true if the message should be retried (NAK), false if it should be acknowledged (ACK)
func handleVoteUpdate(
	ctx context.Context,
	key string,
	v1Data map[string]interface{},
	publisher domain.EventPublisher,
	idMapper domain.IDMapper,
	mappingsKV jetstream.KeyValue,
	logger *slog.Logger,
) bool {
	funcLogger := logger.With("key", key, "handler", "vote")

	funcLogger.DebugContext(ctx, "processing vote update")

	// Convert v1Data map to vote data with proper v2 format
	voteData, err := convertMapToVoteData(ctx, v1Data, idMapper, funcLogger)
	if err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to convert v1Data to vote")
		return false // Permanent error, ACK and skip
	}

	// Extract the vote UID
	if voteData.VoteUID == "" {
		funcLogger.ErrorContext(ctx, "missing or invalid uid in vote data")
		return false // Permanent error, ACK and skip
	}
	funcLogger = funcLogger.With("vote_uid", voteData.VoteUID)

	// Check if parent project exists in mappings
	if voteData.ProjectUID == "" {
		funcLogger.With("project_id", voteData.ProjectID).InfoContext(ctx, "skipping vote sync - parent project not found in mappings")
		return false // Permanent issue, ACK and skip
	}

	// Determine action (created vs updated) by checking if mapping exists
	mappingKey := fmt.Sprintf("vote.%s", voteData.VoteUID)
	indexerAction := indexerConstants.ActionCreated
	if _, err := mappingsKV.Get(ctx, mappingKey); err == nil {
		indexerAction = indexerConstants.ActionUpdated
	}

	// Publish to indexer and FGA-sync
	if err := publisher.PublishVoteEvent(ctx, string(indexerAction), voteData); err != nil {
		funcLogger.With(errKey, err).ErrorContext(ctx, "failed to publish vote event")
		// Check if this is a transient error that should be retried
		if isTransientError(err) {
			return true // NAK for retry
		}
		return false // Permanent error, ACK and skip
	}

	// Store mapping to track that we've seen this vote
	if _, err := mappingsKV.Put(ctx, mappingKey, []byte("1")); err != nil {
		funcLogger.With(errKey, err).WarnContext(ctx, "failed to store vote mapping")
		// Don't retry on mapping storage failures
	}

	funcLogger.InfoContext(ctx, "successfully sent vote indexer and access messages")
	return false // Success, ACK the message
}

// convertMapToVoteData converts v1 poll data to v2 format with proper types and UIDs
func convertMapToVoteData(
	ctx context.Context,
	v1Data map[string]interface{},
	idMapper domain.IDMapper,
	logger *slog.Logger,
) (*VoteData, error) {
	// Convert map to JSON bytes, then to PollDBRaw to handle string fields
	jsonBytes, err := json.Marshal(v1Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal v1Data to JSON: %w", err)
	}

	var pollDB PollDBRaw
	if err := json.Unmarshal(jsonBytes, &pollDB); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON into PollDBRaw: %w", err)
	}

	// Build v2 vote data struct
	voteData := &VoteData{
		VoteUID:               pollDB.PollID,
		PollID:                pollDB.PollID,
		Name:                  pollDB.Name,
		Description:           pollDB.Description,
		CreationTime:          pollDB.CreationTime,
		LastModifiedTime:      pollDB.LastModifiedTime,
		EndTime:               pollDB.EndTime,
		Status:                pollDB.Status,
		ProjectID:             pollDB.ProjectID,
		ProjectName:           pollDB.ProjectName,
		CommitteeID:           pollDB.CommitteeID,
		CommitteeName:         pollDB.CommitteeName,
		CommitteeType:         pollDB.CommitteeType,
		CommitteeVotingStatus: pollDB.CommitteeVotingStatus,
		CommitteeFilters:      pollDB.CommitteeFilters,
		PollQuestions:         pollDB.PollQuestions,
		PollType:              pollDB.PollType,
		PseudoAnonymity:       pollDB.PseudoAnonymity,
		AllowAbstain:          pollDB.AllowAbstain,
	}

	// Convert string integers to actual ints
	if pollDB.TotalVotingRequestInvitations != "" {
		if val, err := strconv.Atoi(pollDB.TotalVotingRequestInvitations); err == nil {
			voteData.TotalVotingRequestInvitations = val
		} else {
			logger.With(errKey, err, "field", "total_voting_request_invitations", "value", pollDB.TotalVotingRequestInvitations).
				WarnContext(ctx, "failed to convert string to int")
		}
	}

	if pollDB.NumResponseReceived != "" {
		if val, err := strconv.Atoi(pollDB.NumResponseReceived); err == nil {
			voteData.NumResponseReceived = val
		} else {
			logger.With(errKey, err, "field", "num_response_received", "value", pollDB.NumResponseReceived).
				WarnContext(ctx, "failed to convert string to int")
		}
	}

	if pollDB.NumWinners != "" {
		if val, err := strconv.Atoi(pollDB.NumWinners); err == nil {
			voteData.NumWinners = val
		} else {
			logger.With(errKey, err, "field", "num_winners", "value", pollDB.NumWinners).
				WarnContext(ctx, "failed to convert string to int")
		}
	}

	// Map v1 project ID (SFID) to v2 project UID
	if pollDB.ProjectID != "" {
		projectUID, err := idMapper.MapProjectV1ToV2(ctx, pollDB.ProjectID)
		if err != nil {
			logger.With(errKey, err, "field", "project_id", "value", pollDB.ProjectID).
				WarnContext(ctx, "failed to get v2 project UID from v1 project ID")
			// Don't set project_uid if mapping fails - will be caught by validation
		} else {
			voteData.ProjectUID = projectUID
		}
	}

	// Map v1 committee ID (SFID) to v2 committee UID
	if pollDB.CommitteeID != "" {
		committeeUID, err := idMapper.MapCommitteeV1ToV2(ctx, pollDB.CommitteeID)
		if err != nil {
			logger.With(errKey, err, "field", "committee_id", "value", pollDB.CommitteeID).
				WarnContext(ctx, "failed to get v2 committee UID from v1 committee ID")
			// Don't set committee_uid if mapping fails - not critical
		} else {
			voteData.CommitteeUID = committeeUID
		}
	}

	return voteData, nil
}

// isTransientError determines if an error is transient and should be retried
func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// NATS publish errors, timeouts, connection issues
	if utils.Contains(errStr, "timeout") || utils.Contains(errStr, "connection") ||
		utils.Contains(errStr, "unavailable") || utils.Contains(errStr, "deadline") {
		return true
	}

	return false
}
