// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go"
)

const (
	// NATS subject for v1-sync-helper lookup
	lookupSubject = "lfx.lookup_v1_mapping"

	// Default request timeout
	defaultTimeout = 5 * time.Second
)

// Config holds the configuration for the NATS-based ID mapper
type Config struct {
	URL     string
	Timeout time.Duration
}

// NATSMapper implements IDMapper using NATS messaging to the v1-sync-helper service
type NATSMapper struct {
	conn    *nats.Conn
	timeout time.Duration
}

// NewNATSMapper creates a new NATS-based ID mapper
func NewNATSMapper(cfg Config) (*NATSMapper, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("NATS URL is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	// Connect to NATS server
	conn, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSMapper{
		conn:    conn,
		timeout: timeout,
	}, nil
}

// Close closes the NATS connection
func (m *NATSMapper) Close() {
	if m.conn != nil {
		m.conn.Close()
	}
}

// MapProjectV2ToV1 maps a v2 project UID to v1 project SFID
func (m *NATSMapper) MapProjectV2ToV1(ctx context.Context, v2UID string) (string, error) {
	if v2UID == "" {
		return "", domain.NewValidationError("v2 project UID is required")
	}

	// Request format: project.uid.{v2_uuid} returns {v1_sfid}
	key := fmt.Sprintf("project.uid.%s", v2UID)
	return m.lookup(ctx, key)
}

// MapProjectV1ToV2 maps a v1 project SFID to v2 project UID
func (m *NATSMapper) MapProjectV1ToV2(ctx context.Context, v1SFID string) (string, error) {
	if v1SFID == "" {
		return "", domain.NewValidationError("v1 project SFID is required")
	}

	// Request format: project.sfid.{v1_sfid} returns {v2_uuid}
	key := fmt.Sprintf("project.sfid.%s", v1SFID)
	return m.lookup(ctx, key)
}

// MapCommitteeV2ToV1 maps a v2 committee UID to v1 committee identifiers
// Returns format: {project_sfid}:{committee_sfid}
func (m *NATSMapper) MapCommitteeV2ToV1(ctx context.Context, v2UID string) (string, error) {
	if v2UID == "" {
		return "", domain.NewValidationError("v2 committee UID is required")
	}

	// Request format: committee.uid.{v2_uuid} returns {project_sfid}:{committee_sfid}
	key := fmt.Sprintf("committee.uid.%s", v2UID)
	return m.lookup(ctx, key)
}

// MapCommitteeV1ToV2 maps a v1 committee SFID to v2 committee UID
func (m *NATSMapper) MapCommitteeV1ToV2(ctx context.Context, v1SFID string) (string, error) {
	if v1SFID == "" {
		return "", domain.NewValidationError("v1 committee SFID is required")
	}

	// Request format: committee.sfid.{v1_sfid} returns {v2_uuid}
	key := fmt.Sprintf("committee.sfid.%s", v1SFID)
	return m.lookup(ctx, key)
}

// lookup performs the NATS request/reply lookup
func (m *NATSMapper) lookup(ctx context.Context, key string) (string, error) {
	// Send request with timeout
	msg, err := m.conn.RequestWithContext(ctx, lookupSubject, []byte(key))
	if err != nil {
		if err == context.DeadlineExceeded || err == nats.ErrTimeout {
			return "", domain.NewUnavailableError("v1-sync-helper lookup timed out", err)
		}
		return "", domain.NewUnavailableError("failed to lookup ID mapping", err)
	}

	// Parse response
	response := string(msg.Data)

	// Check for error response (prefixed with "error: ")
	if strings.HasPrefix(response, "error: ") {
		errMsg := strings.TrimPrefix(response, "error: ")
		return "", domain.NewUnavailableError(fmt.Sprintf("v1-sync-helper error: %s", errMsg))
	}

	// Empty response means not found - return as validation error since client provided invalid ID
	if response == "" {
		return "", domain.NewValidationError(fmt.Sprintf("invalid ID: mapping not found for %s", key))
	}

	return response, nil
}
