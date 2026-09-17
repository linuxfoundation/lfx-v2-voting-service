// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go"
)

const (
	// NATS subject for idmapper lookup
	lookupSubject = "lfx.lookup_v1_mapping"

	// Default request timeout
	defaultTimeout = 5 * time.Second

	// defaultCacheTTL bounds how long a resolved mapping is served from the
	// in-process cache. Mappings are never reassigned once written (the
	// v1-sync-helper only creates them or tombstones them on delete), so the
	// TTL only bounds the post-delete staleness window.
	defaultCacheTTL = 15 * time.Minute

	// defaultMaxCacheEntries bounds cache growth in a long-lived process.
	defaultMaxCacheEntries = 10000
)

// Config holds the configuration for the NATS-based ID mapper
type Config struct {
	URL     string
	Timeout time.Duration
	// CacheTTL overrides how long successful lookups are cached; zero uses defaultCacheTTL.
	CacheTTL time.Duration
	// MaxCacheEntries overrides the cache size cap; zero uses defaultMaxCacheEntries.
	MaxCacheEntries int
}

// cacheEntry is a single cached lookup result with its expiry.
type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// NATSMapper implements IDMapper using NATS messaging to the v1-sync-helper service
type NATSMapper struct {
	conn    *nats.Conn
	timeout time.Duration

	// mu guards cache. The cache is read-through: successful lookups are
	// stored under the lookup key; errors and not-found (empty) responses are
	// never cached, so a create->event race cannot pin a false miss.
	mu              sync.Mutex
	cache           map[string]cacheEntry
	cacheTTL        time.Duration
	maxCacheEntries int
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

	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultCacheTTL
	}

	maxCacheEntries := cfg.MaxCacheEntries
	if maxCacheEntries == 0 {
		maxCacheEntries = defaultMaxCacheEntries
	}

	// Connect to NATS server
	conn, err := nats.Connect(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSMapper{
		conn:            conn,
		timeout:         timeout,
		cache:           make(map[string]cacheEntry),
		cacheTTL:        cacheTTL,
		maxCacheEntries: maxCacheEntries,
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

// MapCommitteeV2ToV1 maps a v2 committee UID to v1 committee SFID
// The NATS response format is {project_sfid}:{committee_sfid}, but we only return the committee SFID
func (m *NATSMapper) MapCommitteeV2ToV1(ctx context.Context, v2UID string) (string, error) {
	if v2UID == "" {
		return "", domain.NewValidationError("v2 committee UID is required")
	}

	// Request format: committee.uid.{v2_uuid} returns {project_sfid}:{committee_sfid}
	key := fmt.Sprintf("committee.uid.%s", v2UID)
	response, err := m.lookup(ctx, key)
	if err != nil {
		return "", err
	}

	// Parse the response to extract only the committee SFID
	// Format: "projectSFID:committeeSFID" -> we want "committeeSFID"
	// If no colon present, assume the response is already just the committee SFID
	parts := strings.Split(response, ":")
	if len(parts) == 1 {
		return response, nil
	}

	if len(parts) != 2 {
		return "", domain.NewUnavailableError(fmt.Sprintf("unexpected committee mapping format: %s", response))
	}

	committeeSFID := parts[1]
	if committeeSFID == "" {
		return "", domain.NewUnavailableError("committee SFID is empty in mapping response")
	}

	return committeeSFID, nil
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

// lookup performs the NATS request/reply lookup, serving repeated lookups for
// the same key from the in-process cache instead of re-issuing a NATS request.
func (m *NATSMapper) lookup(ctx context.Context, key string) (string, error) {
	if value, ok := m.cachedLookup(key); ok {
		return value, nil
	}

	// Send request with timeout
	msg, err := m.conn.RequestWithContext(ctx, lookupSubject, []byte(key))
	if err != nil {
		if err == context.DeadlineExceeded || err == nats.ErrTimeout {
			return "", domain.NewUnavailableError("idmapper lookup timed out", err)
		}
		return "", domain.NewUnavailableError("failed to lookup ID mapping", err)
	}

	// Parse response
	response := string(msg.Data)

	// Check for error response (prefixed with "error: ")
	if errMsg, found := strings.CutPrefix(response, "error: "); found {
		return "", domain.NewUnavailableError(fmt.Sprintf("idmapper error: %s", errMsg))
	}

	// Empty response means not found - return as validation error since client provided invalid ID
	if response == "" {
		return "", domain.NewValidationError(fmt.Sprintf("invalid ID: mapping not found for %s", key))
	}

	m.storeCachedLookup(key, response)
	return response, nil
}

// cachedLookup returns the cached value for key when present and unexpired.
func (m *NATSMapper) cachedLookup(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.cache[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(m.cache, key)
		return "", false
	}
	return entry.value, true
}

// storeCachedLookup caches a successful lookup. Once the cache reaches its cap,
// expired entries are swept first; if nothing is expired, an arbitrary entry is
// evicted so the cache stays bounded.
func (m *NATSMapper) storeCachedLookup(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cache) >= m.maxCacheEntries {
		now := time.Now()
		for k, e := range m.cache {
			if now.After(e.expiresAt) {
				delete(m.cache, k)
			}
		}
		if len(m.cache) >= m.maxCacheEntries {
			for k := range m.cache {
				delete(m.cache, k)
				break
			}
		}
	}
	m.cache[key] = cacheEntry{value: value, expiresAt: time.Now().Add(m.cacheTTL)}
}
