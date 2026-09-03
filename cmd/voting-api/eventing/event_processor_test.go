// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
		assert.NoError(t, err)
		assert.NotNil(t, ep)
		assert.NotNil(t, ep.natsConn)
		assert.NotNil(t, ep.jsInstance)
		assert.NotNil(t, ep.publisher)
		assert.NotNil(t, ep.mappingsKV)
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

	t.Run("fails when mappings KV bucket does not exist", func(t *testing.T) {
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
		assert.Contains(t, err.Error(), "failed to access v1-mappings KV bucket")
	})
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
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
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
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
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
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
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
		ep, err := NewEventProcessor(cfg, idMapper, logger, InviteFeatureConfig{})
		require.NoError(t, err)

		// Stop multiple times should not error
		err = ep.Stop()
		assert.NoError(t, err)

		err = ep.Stop()
		assert.NoError(t, err)
	})
}
