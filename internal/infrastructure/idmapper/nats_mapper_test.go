// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestNATSServer starts an embedded NATS server for testing. Core NATS is
// enough: the mapper's lookup is plain request/reply, no JetStream involved.
func startTestNATSServer(t *testing.T) (*server.Server, string) {
	t.Helper()
	opts := &server.Options{
		Host: "127.0.0.1",
		Port: -1, // Random port
	}

	ns, err := server.NewServer(opts)
	require.NoError(t, err)

	go ns.Start()

	// Wait for server to be ready
	if !ns.ReadyForConnections(4 * time.Second) {
		t.Fatal("NATS server not ready")
	}

	return ns, ns.ClientURL()
}

// TestLookupCachesRepeatedRequests asserts the read-through cache: repeated
// lookups for the same key must issue exactly one NATS request (AC-4).
func TestLookupCachesRepeatedRequests(t *testing.T) {
	ns, natsURL := startTestNATSServer(t)
	defer ns.Shutdown()

	// Responder that counts how many lookup requests it serves.
	var requests atomic.Int32
	conn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer conn.Close()
	sub, err := conn.Subscribe(lookupSubject, func(msg *nats.Msg) {
		requests.Add(1)
		_ = msg.Respond([]byte("v1-sfid-123"))
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	mapper, err := NewNATSMapper(Config{URL: natsURL})
	require.NoError(t, err)
	defer mapper.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		got, err := mapper.MapProjectV2ToV1(ctx, "some-v2-uid")
		require.NoError(t, err)
		assert.Equal(t, "v1-sfid-123", got)
	}

	assert.Equal(t, int32(1), requests.Load(),
		"repeated lookups for the same key must issue exactly one NATS request")
}

// TestLookupDoesNotCacheNotFound asserts empty (not-found) responses are never
// cached: a later lookup must re-issue the request so mappings created after a
// first miss are picked up (the create→event race, where a KV record can arrive
// before the v1-sync-helper has written its mapping).
func TestLookupDoesNotCacheNotFound(t *testing.T) {
	ns, natsURL := startTestNATSServer(t)
	defer ns.Shutdown()

	// Responder answers "not found" (empty) on the first request, then a real mapping.
	var requests atomic.Int32
	conn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer conn.Close()
	sub, err := conn.Subscribe(lookupSubject, func(msg *nats.Msg) {
		if requests.Add(1) == 1 {
			_ = msg.Respond([]byte(""))
			return
		}
		_ = msg.Respond([]byte("v1-sfid-123"))
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	mapper, err := NewNATSMapper(Config{URL: natsURL})
	require.NoError(t, err)
	defer mapper.Close()

	ctx := context.Background()
	if _, err := mapper.MapProjectV2ToV1(ctx, "some-v2-uid"); err == nil {
		t.Fatal("expected a not-found error on the first lookup")
	}

	got, err := mapper.MapProjectV2ToV1(ctx, "some-v2-uid")
	require.NoError(t, err)
	assert.Equal(t, "v1-sfid-123", got)

	// Third lookup is served from the cache: still exactly two NATS requests.
	got, err = mapper.MapProjectV2ToV1(ctx, "some-v2-uid")
	require.NoError(t, err)
	assert.Equal(t, "v1-sfid-123", got)
	assert.Equal(t, int32(2), requests.Load(),
		"a not-found response must not be cached; the resolved mapping must be")
}

// TestLookupCacheConcurrentAccess hammers the cache from many goroutines so the
// -race detector can catch unsynchronized map access: the KV consumer and the
// HTTP handlers share one mapper instance (AC-5).
func TestLookupCacheConcurrentAccess(t *testing.T) {
	ns, natsURL := startTestNATSServer(t)
	defer ns.Shutdown()

	var requests atomic.Int32
	conn, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer conn.Close()
	sub, err := conn.Subscribe(lookupSubject, func(msg *nats.Msg) {
		requests.Add(1)
		_ = msg.Respond([]byte("v1-sfid-123"))
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	mapper, err := NewNATSMapper(Config{URL: natsURL})
	require.NoError(t, err)
	defer mapper.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			project, err := mapper.MapProjectV2ToV1(ctx, "some-v2-uid")
			assert.NoError(t, err)
			assert.Equal(t, "v1-sfid-123", project)
			committee, err := mapper.MapCommitteeV2ToV1(ctx, "another-v2-uid")
			assert.NoError(t, err)
			assert.Equal(t, "v1-sfid-123", committee)
		}()
	}
	wg.Wait()

	// Concurrent first-touch misses may each issue a request (no single-flight);
	// the invariant under test is race-free access with correct values, and that
	// requests never exceed one per goroutine pair.
	assert.LessOrEqual(t, requests.Load(), int32(64))
	assert.GreaterOrEqual(t, requests.Load(), int32(2))
}
