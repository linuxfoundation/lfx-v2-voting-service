// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/mappings"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const V1MappingsBucket = "v1-mappings" // Legacy fixture bucket, never runtime.

func newTestEventProcessor(t *testing.T, cfg eventing.Config, mapper domain.IDMapper, logger *slog.Logger, inviteCfg InviteFeatureConfig) (*EventProcessor, error) {
	t.Helper()
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, err
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	kv, err := js.KeyValue(context.Background(), V1MappingsBucket)
	if err != nil {
		return nil, err
	}
	ep, err := newEventProcessor(cfg, mapper, logger, inviteCfg, mappings.NewKVStore(kv), nil)
	if err == nil {
		t.Cleanup(func() { _ = ep.Stop() })
	}
	return ep, err
}

// startTestNATSServer starts an embedded NATS server with JetStream for testing
func startTestNATSServer(t *testing.T) (*server.Server, string) {
	opts := &server.Options{
		Host:            "127.0.0.1",
		Port:            -1, // Random port
		JetStream:       true,
		JetStreamDomain: "",
		StoreDir:        t.TempDir(), // Isolated store per test to prevent state bleed
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

func TestNewEventProcessor(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("successful initialization", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		// Create the KV buckets
		nc, err := nats.Connect(natsURL)
		require.NoError(t, err)
		defer nc.Close()

		js, err := jetstream.New(nc)
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1MappingsBucket,
		})
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1ObjectsBucket,
		})
		require.NoError(t, err)

		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := newTestEventProcessor(t, cfg, idMapper, logger, InviteFeatureConfig{})
		assert.NoError(t, err)
		assert.NotNil(t, ep)
		assert.NotNil(t, ep.natsConn)
		assert.NotNil(t, ep.jsInstance)
		assert.NotNil(t, ep.publisher)
		assert.NotNil(t, ep.syncer)
	})

	t.Run("fails with invalid NATS URL", func(t *testing.T) {
		cfg := eventing.Config{
			NATSURL:      "nats://invalid:4222",
			ConsumerName: "test-consumer",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
		assert.Error(t, err)
		assert.Nil(t, ep)
		assert.Contains(t, err.Error(), "failed to connect to NATS")
	})

	t.Run("fails when verified Postgres configuration is missing", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		// The bucket must exist before the processor starts — it does not auto-create
		logger := slog.Default()
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
		assert.Error(t, err)
		assert.Nil(t, ep)
		assert.Contains(t, err.Error(), "VOTING_STATE_DATABASE_URL")
	})
}

func TestNewEventProcessor_PostgresCutoverAndGracefulCompletion(t *testing.T) {
	dsn, state := newPostgresState(t)
	ns, natsURL := startTestNATSServer(t)
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	objects, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: V1ObjectsBucket})
	require.NoError(t, err)
	cfg := eventing.Config{NATSURL: natsURL, StateDatabaseURL: dsn, ConsumerName: "pg-test", StreamName: "KV_v1-objects", FilterSubjects: []string{"$KV.v1-objects.itx-poll.>", "$KV.v1-objects.itx-poll-vote.>"}, MaxDeliver: -1, AckWait: 30 * time.Second, MaxAckPending: 10}
	ep, err := NewEventProcessor(cfg, echoIDMapper{}, slog.Default(), InviteFeatureConfig{})
	require.Error(t, err)
	require.Nil(t, ep)
	require.Contains(t, err.Error(), "cutover not verified")
	// A verified fixture checkpoint, not runtime bootstrap. No legacy bucket exists.
	admin, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "INSERT INTO voting_state_cutover(singleton,source_last_sequence) VALUES(true,1)")
	require.NoError(t, err)
	admin.Close()
	_, err = js.KeyValue(context.Background(), V1MappingsBucket)
	require.Error(t, err)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var sends atomic.Int32
	reply, err := json.Marshal(inviteapi.SendInviteResponse{InviteData: &inviteapi.InviteData{UID: "invite-1", Email: "guest@example.com"}})
	require.NoError(t, err)
	_, err = nc.Subscribe(votingconstants.AuthEmailToUsernameSubject, func(msg *nats.Msg) { assert.NoError(t, msg.Respond(nil)) })
	require.NoError(t, err)
	_, err = nc.Subscribe(inviteapi.SendInviteSubject, func(msg *nats.Msg) {
		sends.Add(1)
		entered <- struct{}{}
		<-release
		assert.NoError(t, msg.Respond(reply))
	})
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	ep, err = NewEventProcessor(cfg, echoIDMapper{}, slog.Default(), InviteFeatureConfig{Enabled: true, SelfServeBaseURL: "https://app.example.com"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)
	go func() { startDone <- ep.Start(ctx) }()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); cancel(); assert.NoError(t, <-startDone); _ = ep.Stop() })
	_, err = objects.Put(ctx, "itx-poll.one", []byte(`{"poll_id":"one","name":"Test Vote","status":"active","project_id":"project","pseudo_anonymity":false}`))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return state.State(ctx, domain.MappingKindVote, "one") == domain.MappingSynced }, 5*time.Second, 10*time.Millisecond)
	consumer, err := js.Consumer(ctx, cfg.StreamName, cfg.ConsumerName)
	require.NoError(t, err)
	require.Equal(t, -1, consumer.CachedInfo().Config.MaxDeliver)
	_, err = objects.Put(ctx, "itx-poll-vote.response", []byte(`{"vote_id":"response","poll_id":"one","project_id":"project","user_email":"guest@example.com"}`))
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("invite dispatch did not start")
	}
	guard, err := state.LookupInvitation(ctx, "response")
	require.NoError(t, err)
	require.True(t, guard.Pending)
	cancel()
	stopDone := make(chan error, 1)
	go func() { stopDone <- ep.Stop() }()
	select {
	case err := <-stopDone:
		t.Fatalf("closed resources during callback: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	require.NoError(t, <-stopDone)
	guard, err = state.LookupInvitation(context.Background(), "response")
	require.NoError(t, err)
	require.Equal(t, "invite-1", guard.InviteID)
	require.False(t, guard.Pending)
	require.Equal(t, domain.MappingSynced, state.State(context.Background(), domain.MappingKindVoteResponse, "response"))
	require.Equal(t, int32(1), sends.Load())
	require.NoError(t, ep.Stop())
}

func TestEventProcessor_Start(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("starts and stops with context cancellation", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		// Create the KV buckets
		nc, err := nats.Connect(natsURL)
		require.NoError(t, err)
		defer nc.Close()

		js, err := jetstream.New(nc)
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1MappingsBucket,
		})
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1ObjectsBucket,
		})
		require.NoError(t, err)

		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer-start",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := newTestEventProcessor(t, cfg, idMapper, logger, InviteFeatureConfig{})
		require.NoError(t, err)
		require.NotNil(t, ep)

		ctx, cancel := context.WithCancel(context.Background())

		// Start in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- ep.Start(ctx)
		}()

		// Give it time to start
		time.Sleep(100 * time.Millisecond)

		// Cancel context to stop. Reading internal fields (ep.consumer, ep.consumeCtx)
		// here would race with the Start() goroutine that writes them; successful
		// return from Start() is sufficient proof that the consumer was created.
		cancel()

		// Wait for Start to return
		select {
		case err := <-errChan:
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("Start did not return after context cancellation")
		}

		// Verify consumer was created. Reads happen after the errChan receive, whose
		// send establishes happens-before with Start's writes to these fields — the
		// previous sleep-based ordering raced with Start under -race.
		assert.NotNil(t, ep.consumer)
		assert.NotNil(t, ep.consumeCtx)
	})

	t.Run("returns error when consumer creation fails", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		// Create the KV buckets
		nc, err := nats.Connect(natsURL)
		require.NoError(t, err)
		defer nc.Close()

		js, err := jetstream.New(nc)
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1MappingsBucket,
		})
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1ObjectsBucket,
		})
		require.NoError(t, err)

		// Use invalid stream name to cause consumer creation to fail
		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer-fail",
			StreamName:   "NonExistentStream",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := newTestEventProcessor(t, cfg, idMapper, logger, InviteFeatureConfig{})
		require.NoError(t, err)

		ctx := context.Background()
		err = ep.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create or update consumer")
	})
}

func TestEventProcessor_Stop(t *testing.T) {
	logging.InitStructureLogConfig()

	t.Run("stops successfully after starting", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		// Create the KV buckets
		nc, err := nats.Connect(natsURL)
		require.NoError(t, err)
		defer nc.Close()

		js, err := jetstream.New(nc)
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1MappingsBucket,
		})
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1ObjectsBucket,
		})
		require.NoError(t, err)

		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer-stop",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := newTestEventProcessor(t, cfg, idMapper, logger, InviteFeatureConfig{})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start in goroutine
		go func() {
			_ = ep.Start(ctx)
		}()

		// Give it time to start
		time.Sleep(100 * time.Millisecond)

		// Stop the event processor
		err = ep.Stop()
		assert.NoError(t, err)
	})

	t.Run("stop is idempotent", func(t *testing.T) {
		ns, natsURL := startTestNATSServer(t)
		defer ns.Shutdown()

		// Create the KV buckets
		nc, err := nats.Connect(natsURL)
		require.NoError(t, err)
		defer nc.Close()

		js, err := jetstream.New(nc)
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1MappingsBucket,
		})
		require.NoError(t, err)

		_, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
			Bucket: V1ObjectsBucket,
		})
		require.NoError(t, err)

		cfg := eventing.Config{
			NATSURL:      natsURL,
			ConsumerName: "test-consumer-idempotent",
			StreamName:   "KV_v1-objects",
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}

		idMapper := idmapper.NewNoOpMapper()

		logger := slog.Default()
		ep, err := newTestEventProcessor(t, cfg, idMapper, logger, InviteFeatureConfig{})
		require.NoError(t, err)

		// Stop multiple times should not error
		err = ep.Stop()
		assert.NoError(t, err)

		err = ep.Stop()
		assert.NoError(t, err)
	})
}
