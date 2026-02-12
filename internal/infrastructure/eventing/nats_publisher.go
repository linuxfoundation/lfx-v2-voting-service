// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	indexerTypes "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/types"
	"github.com/nats-io/nats.go"
)

// NATS subject constants for voting operations
const (
	// IndexVoteSubject is the subject for vote indexing
	IndexVoteSubject = "lfx.index.vote"

	// IndexVoteResponseSubject is the subject for vote response indexing
	IndexVoteResponseSubject = "lfx.index.vote_response"

	// UpdateAccessSubject is the subject for FGA access control updates
	UpdateAccessSubject = "lfx.fga-sync.update_access"
)

// GenericFGAMessage represents a generic FGA message
type GenericFGAMessage struct {
	ObjectType string                 `json:"object_type"`
	Operation  string                 `json:"operation"`
	Data       map[string]interface{} `json:"data"`
}

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
func (p *NATSPublisher) PublishVoteEvent(ctx context.Context, action string, vote interface{}) error {
	// Send to indexer
	if err := p.sendVoteIndexerMessage(ctx, IndexVoteSubject, indexerConstants.MessageAction(action), vote); err != nil {
		return fmt.Errorf("failed to send vote indexer message: %w", err)
	}

	// Send to FGA-sync
	if err := p.sendVoteAccessMessage(vote); err != nil {
		return fmt.Errorf("failed to send vote access message: %w", err)
	}

	return nil
}

// PublishVoteResponseEvent publishes a vote response event to indexer and FGA-sync
func (p *NATSPublisher) PublishVoteResponseEvent(ctx context.Context, action string, voteResponse interface{}) error {
	// Send to indexer
	if err := p.sendVoteResponseIndexerMessage(ctx, IndexVoteResponseSubject, indexerConstants.MessageAction(action), voteResponse); err != nil {
		return fmt.Errorf("failed to send vote response indexer message: %w", err)
	}

	// Send to FGA-sync
	if err := p.sendVoteResponseAccessMessage(voteResponse); err != nil {
		return fmt.Errorf("failed to send vote response access message: %w", err)
	}

	return nil
}

// Close closes the publisher connection
func (p *NATSPublisher) Close() error {
	// NATS connection is managed by the event processor, so we don't close it here
	return nil
}

// sendVoteIndexerMessage sends the message to the NATS server for the vote indexer
func (p *NATSPublisher) sendVoteIndexerMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, data interface{}) error {
	headers := make(map[string]string)

	// Extract authorization from context if available
	if authorization, ok := ctx.Value("authorization").(string); ok {
		headers["authorization"] = authorization
	} else {
		// Fallback for system-generated events
		headers["authorization"] = "Bearer v1-sync-helper"
	}

	// Extract principal from context if available
	if principal, ok := ctx.Value("principal").(string); ok {
		headers["x-on-behalf-of"] = principal
	}

	// Extract vote data fields
	voteData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid vote data type")
	}

	// Construct parent refs and name aliases
	public := false
	nameAndAliases := []string{}
	parentRefs := []string{}

	if name, ok := voteData["name"].(string); ok && name != "" {
		nameAndAliases = append(nameAndAliases, name)
	}
	if projectUID, ok := voteData["project_uid"].(string); ok && projectUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("project:%s", projectUID))
	}
	if committeeUID, ok := voteData["committee_uid"].(string); ok && committeeUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("committee:%s", committeeUID))
	}

	// Construct the indexer message
	message := indexerTypes.IndexerMessageEnvelope{
		Action:  action,
		Headers: headers,
		Data:    data,
		IndexingConfig: &indexerTypes.IndexingConfig{
			ObjectID:             "{{ uid }}",
			Public:               &public,
			AccessCheckObject:    "vote:{{ uid }}",
			AccessCheckRelation:  "viewer",
			HistoryCheckObject:   "vote:{{ uid }}",
			HistoryCheckRelation: "auditor",
			SortName:             "{{ name }}",
			NameAndAliases:       nameAndAliases,
			ParentRefs:           parentRefs,
			Fulltext:             "{{ name }} {{ description }}",
		},
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal indexer message for subject %s: %w", subject, err)
	}

	p.logger.With("subject", subject, "action", action).DebugContext(ctx, "constructed indexer message")

	// Publish the message to NATS
	if err := p.conn.Publish(subject, messageBytes); err != nil {
		return fmt.Errorf("failed to publish indexer message to subject %s: %w", subject, err)
	}

	return nil
}

// sendVoteAccessMessage sends the message to the NATS server for the vote access control
func (p *NATSPublisher) sendVoteAccessMessage(vote interface{}) error {
	voteData, ok := vote.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid vote data type")
	}

	references := map[string][]string{}

	if projectUID, ok := voteData["project_uid"].(string); ok && projectUID != "" {
		references["project"] = []string{projectUID}
	}
	if committeeUID, ok := voteData["committee_uid"].(string); ok && committeeUID != "" {
		references["committee"] = []string{committeeUID}
	}

	// Skip sending access message if there are no references
	if len(references) == 0 {
		return nil
	}

	uid, _ := voteData["uid"].(string)

	accessMsg := GenericFGAMessage{
		ObjectType: "vote",
		Operation:  "update_access",
		Data: map[string]interface{}{
			"uid":        uid,
			"public":     false,
			"references": references,
		},
	}

	accessMsgBytes, err := json.Marshal(accessMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal access message: %w", err)
	}

	// Publish the message to NATS
	if err := p.conn.Publish(UpdateAccessSubject, accessMsgBytes); err != nil {
		return fmt.Errorf("failed to publish access message to subject %s: %w", UpdateAccessSubject, err)
	}

	return nil
}

// sendVoteResponseIndexerMessage sends the message to the NATS server for the vote response indexer
func (p *NATSPublisher) sendVoteResponseIndexerMessage(ctx context.Context, subject string, action indexerConstants.MessageAction, data interface{}) error {
	headers := make(map[string]string)

	// Extract authorization from context if available
	if authorization, ok := ctx.Value("authorization").(string); ok {
		headers["authorization"] = authorization
	} else {
		// Fallback for system-generated events
		headers["authorization"] = "Bearer v1-sync-helper"
	}

	// Extract principal from context if available
	if principal, ok := ctx.Value("principal").(string); ok {
		headers["x-on-behalf-of"] = principal
	}

	// Extract vote response data fields
	voteResponseData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid vote response data type")
	}

	// Construct parent refs and name aliases
	public := false
	nameAndAliases := []string{}
	parentRefs := []string{}

	if username, ok := voteResponseData["username"].(string); ok && username != "" {
		nameAndAliases = append(nameAndAliases, username)
	}
	if projectUID, ok := voteResponseData["project_uid"].(string); ok && projectUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("project:%s", projectUID))
	}
	if voteUID, ok := voteResponseData["vote_uid"].(string); ok && voteUID != "" {
		parentRefs = append(parentRefs, fmt.Sprintf("vote:%s", voteUID))
	}

	// Construct the indexer message
	indexingConfig := &indexerTypes.IndexingConfig{
		ObjectID:             "{{ uid }}",
		Public:               &public,
		AccessCheckObject:    "vote:{{ uid }}",
		AccessCheckRelation:  "viewer",
		HistoryCheckObject:   "vote_response:{{ uid }}",
		HistoryCheckRelation: "auditor",
		SortName:             "{{ user_name }}",
		NameAndAliases:       nameAndAliases,
		ParentRefs:           parentRefs,
		Fulltext:             "{{ user_name }}",
	}

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
	if err := p.conn.Publish(subject, messageBytes); err != nil {
		return fmt.Errorf("failed to publish indexer message to subject %s: %w", subject, err)
	}

	return nil
}

// sendVoteResponseAccessMessage sends the message to the NATS server for the vote response access control
func (p *NATSPublisher) sendVoteResponseAccessMessage(data interface{}) error {
	voteResponseData, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid vote response data type")
	}

	relations := map[string][]string{}
	if username, ok := voteResponseData["username"].(string); ok && username != "" {
		relations["writer"] = []string{username}
		relations["viewer"] = []string{username}
	}

	references := map[string][]string{}
	if projectUID, ok := voteResponseData["project_uid"].(string); ok && projectUID != "" {
		references["project"] = []string{projectUID}
	}
	if voteUID, ok := voteResponseData["vote_uid"].(string); ok && voteUID != "" {
		references["vote"] = []string{voteUID}
	}

	// Skip sending access message if there are no relations or references
	if len(relations) == 0 && len(references) == 0 {
		return nil
	}

	uid, _ := voteResponseData["uid"].(string)

	accessMsg := GenericFGAMessage{
		ObjectType: "vote_response",
		Operation:  "update_access",
		Data: map[string]interface{}{
			"uid":        uid,
			"public":     false,
			"relations":  relations,
			"references": references,
		},
	}

	accessMsgBytes, err := json.Marshal(accessMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal access message: %w", err)
	}

	// Publish the message to NATS
	if err := p.conn.Publish(UpdateAccessSubject, accessMsgBytes); err != nil {
		return fmt.Errorf("failed to publish access message to subject %s: %w", UpdateAccessSubject, err)
	}

	return nil
}
