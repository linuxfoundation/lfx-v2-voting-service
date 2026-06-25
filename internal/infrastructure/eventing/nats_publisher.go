// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	fgaconstants "github.com/linuxfoundation/lfx-v2-fga-sync/pkg/constants"
	fgatypes "github.com/linuxfoundation/lfx-v2-fga-sync/pkg/types"
	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	indexerTypes "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/types"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NATS subject constants for voting operations
const (
	// IndexVoteSubject is the subject for vote indexing
	IndexVoteSubject = "lfx.index.vote"

	// IndexVoteResponseSubject is the subject for vote response indexing
	IndexVoteResponseSubject = "lfx.index.vote_response"
)

// NATSPublisher implements the EventPublisher interface
type NATSPublisher struct {
	conn   *nats.Conn
	logger *slog.Logger
}

// NewNATSPublisher creates a new NATS publisher
func NewNATSPublisher(conn *nats.Conn, logger *slog.Logger) *NATSPublisher {
	return &NATSPublisher{
		conn:   conn,
		logger: logger,
	}
}

// PublishVoteEvent publishes a vote (poll) event to indexer and FGA-sync
func (p *NATSPublisher) PublishVoteEvent(ctx context.Context, action string, vote *domain.VoteData) error {
	// Send to indexer
	if err := p.sendVoteIndexerMessage(ctx, IndexVoteSubject, indexerConstants.MessageAction(action), vote); err != nil {
		return fmt.Errorf("failed to send vote indexer message: %w", err)
	}

	// Send to FGA-sync - different message for delete vs create/update
	if action == string(indexerConstants.ActionDeleted) {
		if err := p.sendDeleteAccessMessage(ctx, "vote", vote.VoteUID); err != nil {
			return fmt.Errorf("failed to send vote delete access message: %w", err)
		}
	} else {
		if err := p.sendVoteAccessMessage(ctx, vote); err != nil {
			return fmt.Errorf("failed to send vote access message: %w", err)
		}
	}

	return nil
}

// PublishVoteResponseEvent publishes a vote response event to indexer and FGA-sync
func (p *NATSPublisher) PublishVoteResponseEvent(ctx context.Context, action string, voteResponse *domain.VoteResponseData) error {
	// Send to indexer
	if err := p.sendVoteResponseIndexerMessage(ctx, IndexVoteResponseSubject, indexerConstants.MessageAction(action), voteResponse); err != nil {
		return fmt.Errorf("failed to send vote response indexer message: %w", err)
	}

	// Send to FGA-sync - different message for delete vs create/update
	if action == string(indexerConstants.ActionDeleted) {
		if err := p.sendDeleteAccessMessage(ctx, "vote_response", voteResponse.UID); err != nil {
			return fmt.Errorf("failed to send vote response delete access message: %w", err)
		}
	} else {
		if err := p.sendVoteResponseAccessMessage(ctx, voteResponse); err != nil {
			return fmt.Errorf("failed to send vote response access message: %w", err)
		}
	}

	return nil
}

// Close closes the publisher connection
func (p *NATSPublisher) Close() error {
	// NATS connection is managed by the event processor, so we don't close it here
	return nil
}

// sendVoteIndexerMessage routes to the appropriate indexer message handler based on action
func (p *NATSPublisher) sendVoteIndexerMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, data *domain.VoteData) error {
	// Build IndexingConfig (needed for both create/update and delete)
	nameAndAliases := []string{}
	parentRefs := []string{}

	if data.Name != "" {
		nameAndAliases = append(nameAndAliases, data.Name)
	}
	if data.ProjectUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("project:%s", data.ProjectUID))
	}
	if data.CommitteeUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("committee:%s", data.CommitteeUID))
	}

	tags := []string{}
	if data.CommitteeUID != "" {
		tags = append(tags, fmt.Sprintf("committee_uid:%s", data.CommitteeUID))
	}
	if data.ProjectUID != "" {
		tags = append(tags, fmt.Sprintf("project_uid:%s", data.ProjectUID))
	}

	indexingConfig := &indexerTypes.IndexingConfig{
		ObjectID:             data.VoteUID,
		AccessCheckObject:    fmt.Sprintf("vote:%s", data.VoteUID),
		AccessCheckRelation:  "viewer",
		HistoryCheckObject:   fmt.Sprintf("vote:%s", data.VoteUID),
		HistoryCheckRelation: "auditor",
		SortName:             data.Name,
		NameAndAliases:       nameAndAliases,
		ParentRefs:           parentRefs,
		Fulltext:             fmt.Sprintf("%s %s", data.Name, data.Description),
		Tags:                 tags,
	}

	if action == indexerConstants.ActionDeleted {
		return p.sendIndexerDeleteMessage(ctx, subject, action, data.VoteUID, indexingConfig)
	}

	return p.sendIndexerCreateUpdateMessage(ctx, subject, action, data, indexingConfig)
}

// sendVoteAccessMessage sends the message to the NATS server for the vote access control
func (p *NATSPublisher) sendVoteAccessMessage(ctx context.Context, vote *domain.VoteData) error {
	references := map[string][]string{}

	if vote.ProjectUID != "" {
		references["project"] = []string{vote.ProjectUID}
	}
	if vote.CommitteeUID != "" {
		references["committee"] = []string{vote.CommitteeUID}
	}

	// Skip sending access message if there are no references
	if len(references) == 0 {
		return nil
	}

	accessMsg := fgatypes.GenericFGAMessage{
		ObjectType: "vote",
		Operation:  "update_access",
		Data: fgatypes.GenericAccessData{
			UID:        vote.VoteUID,
			Public:     false,
			References: references,
		},
	}

	accessMsgBytes, err := json.Marshal(accessMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal access message: %w", err)
	}

	// Publish the message to NATS
	if err := p.publishWithSpan(ctx, fgaconstants.GenericUpdateAccessSubject, accessMsgBytes); err != nil {
		return fmt.Errorf("failed to publish access message to subject %s: %w", fgaconstants.GenericUpdateAccessSubject, err)
	}

	return nil
}

// sendVoteResponseIndexerMessage routes to the appropriate indexer message handler based on action
func (p *NATSPublisher) sendVoteResponseIndexerMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, data *domain.VoteResponseData) error {
	// Build IndexingConfig (needed for both create/update and delete)
	nameAndAliases := []string{}
	parentRefs := []string{}

	if data.Username != "" {
		nameAndAliases = append(nameAndAliases, data.Username)
	}
	if data.ProjectUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("project:%s", data.ProjectUID))
	}
	if data.VoteUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("vote:%s", data.VoteUID))
	}

	tags := []string{}
	if data.VoteUID != "" {
		tags = append(tags, fmt.Sprintf("vote_uid:%s", data.VoteUID))
	}
	if data.ProjectUID != "" {
		tags = append(tags, fmt.Sprintf("project_uid:%s", data.ProjectUID))
	}

	indexingConfig := &indexerTypes.IndexingConfig{
		ObjectID:             data.UID,
		AccessCheckObject:    fmt.Sprintf("vote:%s", data.VoteUID),
		AccessCheckRelation:  "viewer",
		HistoryCheckObject:   fmt.Sprintf("vote_response:%s", data.UID),
		HistoryCheckRelation: "auditor",
		SortName:             data.Username,
		NameAndAliases:       nameAndAliases,
		ParentRefs:           parentRefs,
		Fulltext:             data.Username,
		Tags:                 tags,
	}

	if action == indexerConstants.ActionDeleted {
		return p.sendIndexerDeleteMessage(ctx, subject, action, data.UID, indexingConfig)
	}

	return p.sendIndexerCreateUpdateMessage(ctx, subject, action, data, indexingConfig)
}

// sendVoteResponseAccessMessage sends the message to the NATS server for the vote response access control
func (p *NATSPublisher) sendVoteResponseAccessMessage(ctx context.Context, data *domain.VoteResponseData) error {
	relations := map[string][]string{}
	if data.Username != "" {
		relations["owner"] = []string{data.Username}
	}

	references := map[string][]string{}
	if data.VoteUID != "" {
		references["vote"] = []string{data.VoteUID}
	}

	// Skip sending access message if there are no relations or references
	if len(relations) == 0 && len(references) == 0 {
		return nil
	}

	accessMsg := fgatypes.GenericFGAMessage{
		ObjectType: "vote_response",
		Operation:  "update_access",
		Data: fgatypes.GenericAccessData{
			UID:        data.UID,
			Public:     false,
			Relations:  relations,
			References: references,
		},
	}

	accessMsgBytes, err := json.Marshal(accessMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal access message: %w", err)
	}

	// Publish the message to NATS
	if err := p.publishWithSpan(ctx, fgaconstants.GenericUpdateAccessSubject, accessMsgBytes); err != nil {
		return fmt.Errorf("failed to publish access message to subject %s: %w", fgaconstants.GenericUpdateAccessSubject, err)
	}

	return nil
}

// sendDeleteAccessMessage sends a delete access message to FGA-sync
// This removes all tuples for a resource (typically used when a resource is deleted)
func (p *NATSPublisher) sendDeleteAccessMessage(ctx context.Context, objectType string, uid string) error {
	// Construct delete access message
	deleteMsg := fgatypes.GenericFGAMessage{
		ObjectType: objectType,
		Operation:  "delete_access",
		Data:       fgatypes.GenericDeleteData{UID: uid},
	}

	deleteMsgBytes, err := json.Marshal(deleteMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal delete access message: %w", err)
	}

	// Publish the message to NATS
	if err := p.publishWithSpan(ctx, fgaconstants.GenericDeleteAccessSubject, deleteMsgBytes); err != nil {
		return fmt.Errorf("failed to publish delete access message to subject %s: %w", fgaconstants.GenericDeleteAccessSubject, err)
	}

	return nil
}

// sendIndexerDeleteMessage sends a generic delete message to the indexer with just the UID
func (p *NATSPublisher) sendIndexerDeleteMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, uid string, indexingConfig *indexerTypes.IndexingConfig) error {
	headers := p.buildHeaders(ctx)

	message := indexerTypes.IndexerMessageEnvelope{
		Action:         action,
		Headers:        headers,
		Data:           uid,
		IndexingConfig: indexingConfig,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal indexer delete message for subject %s: %w", subject, err)
	}

	p.logger.With("subject", subject, "action", action, "uid", uid).DebugContext(ctx, "constructed indexer delete message")

	// Publish the message to NATS
	if err := p.publishWithSpan(ctx, subject, messageBytes); err != nil {
		return fmt.Errorf("failed to publish indexer delete message to subject %s: %w", subject, err)
	}

	return nil
}

// sendIndexerCreateUpdateMessage sends a generic create/update message to the indexer with full object and IndexingConfig
func (p *NATSPublisher) sendIndexerCreateUpdateMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, data interface{}, indexingConfig *indexerTypes.IndexingConfig) error {
	headers := p.buildHeaders(ctx)

	public := false
	indexingConfig.Public = &public

	// Construct the indexer message
	message := indexerTypes.IndexerMessageEnvelope{
		Action:         action,
		Headers:        headers,
		Data:           data,
		IndexingConfig: indexingConfig,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal indexer message for subject %s: %w", subject, err)
	}

	p.logger.With("subject", subject, "action", action).DebugContext(ctx, "constructed indexer message")

	// Publish the message to NATS
	if err := p.publishWithSpan(ctx, subject, messageBytes); err != nil {
		return fmt.Errorf("failed to publish indexer message to subject %s: %w", subject, err)
	}

	return nil
}

// publishWithSpan wraps conn.PublishMsg with an OTel producer span and injects
// trace context into the NATS message headers.
func (p *NATSPublisher) publishWithSpan(ctx context.Context, subject string, data []byte) error {
	ctx, span := tracer.Start(ctx, "nats.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", subject),
			attribute.Int("messaging.message.body.size", len(data)),
		),
	)
	defer span.End()

	msg := nats.NewMsg(subject)
	msg.Data = data
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

	if err := p.conn.PublishMsg(msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return domain.NewInternalError(fmt.Sprintf("failed to publish to subject %s", subject), err)
	}
	return nil
}

// buildHeaders extracts headers from context for NATS messages
func (p *NATSPublisher) buildHeaders(ctx context.Context) map[string]string {
	headers := make(map[string]string)

	// Extract authorization from context if available
	if authorization, ok := ctx.Value(constants.AuthorizationContextID).(string); ok {
		headers["authorization"] = authorization
	} else {
		// Fallback for system-generated events
		headers["authorization"] = "Bearer voting-service"
	}

	// Extract principal from context if available
	if principal, ok := ctx.Value(constants.PrincipalContextID).(string); ok {
		headers["x-on-behalf-of"] = principal
	}

	return headers
}
