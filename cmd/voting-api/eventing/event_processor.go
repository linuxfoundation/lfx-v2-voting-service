// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventstate"
	infraNATS "github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/nats"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service/eventsync"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// EventProcessor handles NATS KV bucket event processing
type EventProcessor struct {
	natsConn         *nats.Conn
	jsInstance       jetstream.JetStream
	consumer         jetstream.Consumer
	consumeCtx       jetstream.ConsumeContext
	publisher        domain.EventPublisher
	syncer           *eventsync.Syncer
	logger           *slog.Logger
	config           eventing.Config
	stateStore       *eventstate.Store
	state            domain.MappingStore
	mu               sync.Mutex
	started          bool
	stopRequested    bool
	ready            chan struct{}
	processingCancel context.CancelFunc
	startupCancel    context.CancelFunc
	active           sync.WaitGroup
	callbacksDone    chan struct{}
	stopOnce         sync.Once
	stopErr          error
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	cfg eventing.Config,
	idMapper domain.IDMapper,
	logger *slog.Logger,
	inviteCfg InviteFeatureConfig,
) (*EventProcessor, error) {
	return newEventProcessor(cfg, idMapper, logger, inviteCfg, nil, nil)
}

// newEventProcessor lets package tests inject domain stores. Public startup
// always requires the verified Postgres store and never opens legacy KV.
func newEventProcessor(cfg eventing.Config, idMapper domain.IDMapper, logger *slog.Logger, inviteCfg InviteFeatureConfig, state domain.MappingStore, guards domain.InvitationGuards) (*EventProcessor, error) {
	// Connect to NATS
	conn, err := nats.Connect(cfg.NATSURL,
		nats.DrainTimeout(30*time.Second),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			if sub != nil {
				logger.With("error", err, "subject", sub.Subject).Error("NATS async error encountered")
			} else {
				logger.With("error", err).Error("NATS async error encountered")
			}
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Warn("NATS connection closed")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Create JetStream context
	jsContext, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	// Initialize publisher
	publisher := eventing.NewNATSPublisher(conn, logger, eventing.WithParticipantGrants(cfg.ParticipantGrantsEnabled))

	var stateStore *eventstate.Store
	if state == nil {
		stateStore, err = eventstate.Open(context.Background(), cfg.StateDatabaseURL)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if err := stateStore.Ready(context.Background()); err != nil {
			stateStore.Close()
			conn.Close()
			return nil, err
		}
		state = stateStore
		guards = stateStore
	}

	// Access the V1 objects KV bucket. The vote response handler always reads parent
	// itx-poll records from it to decide whether ballot data must be redacted.
	v1ObjectsKV, err := jsContext.KeyValue(context.Background(), V1ObjectsBucket)
	if err != nil {
		if stateStore != nil {
			stateStore.Close()
		}
		conn.Close()
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", V1ObjectsBucket, err)
	}

	var inviteHandler *VoteResponseInviteHandler
	if inviteCfg.Enabled &&
		strings.TrimSpace(inviteCfg.SelfServeBaseURL) != "" {
		inviteHandler = &VoteResponseInviteHandler{
			inviteSender:     infraNATS.NewInviteSender(conn, logger),
			userReader:       infraNATS.NewUserReader(conn, logger),
			v1ObjectsKV:      v1ObjectsKV,
			guards:           guards,
			selfServeBaseURL: inviteCfg.SelfServeBaseURL,
		}
	}

	deps := eventsync.Deps{
		Publisher: publisher,
		IDMapper:  idMapper,
		Mappings:  state,
		Polls:     pollPrivacyReader{objectsKV: v1ObjectsKV, logger: logger},
		Logger:    logger,
	}
	// Only set the inviter when invites are enabled; a typed-nil handler in the interface
	// would not compare equal to nil.
	if inviteHandler != nil {
		deps.Inviter = inviteHandler
	}

	return &EventProcessor{
		natsConn:      conn,
		jsInstance:    jsContext,
		publisher:     publisher,
		syncer:        eventsync.New(deps),
		logger:        logger,
		config:        cfg,
		stateStore:    stateStore,
		state:         state,
		callbacksDone: make(chan struct{}),
	}, nil
}

// Start starts the event processor
func (ep *EventProcessor) Start(ctx context.Context) error {
	processingCtx, cancelProcessing := context.WithCancel(context.WithoutCancel(ctx))
	startupCtx, cancelStartup := context.WithCancel(ctx)
	defer cancelStartup()
	ep.mu.Lock()
	if ep.started {
		ep.mu.Unlock()
		cancelProcessing()
		return fmt.Errorf("event processor already started")
	}
	ep.started = true
	ep.ready = make(chan struct{})
	ep.processingCancel = cancelProcessing
	ep.startupCancel = cancelStartup
	ready := ep.ready
	stopped := ep.stopRequested
	if ep.callbacksDone == nil {
		ep.callbacksDone = make(chan struct{})
	}
	ep.mu.Unlock()
	var once sync.Once
	signalReady := func() { once.Do(func() { close(ready) }) }
	defer signalReady()
	if stopped {
		cancelProcessing()
		return context.Canceled
	}
	ep.logger.Info("Starting event processor", "consumer_name", ep.config.ConsumerName)

	// Create or update consumer
	consumer, err := ep.jsInstance.CreateOrUpdateConsumer(startupCtx, ep.config.StreamName, jetstream.ConsumerConfig{
		Name:           ep.config.ConsumerName,
		Durable:        ep.config.ConsumerName,
		DeliverPolicy:  jetstream.DeliverLastPerSubjectPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: ep.config.FilterSubjects,
		MaxDeliver:     ep.config.MaxDeliver,
		AckWait:        ep.config.AckWait,
		MaxAckPending:  ep.config.MaxAckPending,
		Description:    "Durable/shared KV bucket watcher for voting service",
	})
	if err != nil {
		cancelProcessing()
		return fmt.Errorf("failed to create or update consumer: %w", err)
	}
	ep.consumer = consumer
	ep.mu.Lock()
	stopped = ep.stopRequested
	ep.mu.Unlock()
	if stopped {
		cancelProcessing()
		return context.Canceled
	}

	// Start consuming messages
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		ep.mu.Lock()
		if ep.stopRequested {
			ep.mu.Unlock()
			return
		}
		ep.active.Add(1)
		ep.mu.Unlock()
		defer ep.active.Done()
		ep.processMessage(processingCtx, msg)
	}, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		ep.logger.With("error", err).Error("KV consumer error encountered")
	}))
	if err != nil {
		cancelProcessing()
		return fmt.Errorf("failed to start consuming messages: %w", err)
	}
	ep.mu.Lock()
	ep.consumeCtx = consumeCtx
	ep.mu.Unlock()
	signalReady()

	ep.logger.Info("Event processor started successfully")

	// Block until context is cancelled
	<-ctx.Done()

	ep.logger.Info("Event processor context cancelled")
	return nil
}

// Stop stops the event processor gracefully
func (ep *EventProcessor) Stop() error {
	ep.stopOnce.Do(func() { ep.stopErr = ep.stop() })
	return ep.stopErr
}

func (ep *EventProcessor) stop() error {
	ep.logger.Info("Stopping event processor...")
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ep.mu.Lock()
	ep.stopRequested = true
	ready, cancelProcessing, cancelStartup := ep.ready, ep.processingCancel, ep.startupCancel
	ep.mu.Unlock()
	if cancelStartup != nil {
		cancelStartup()
	}
	if ready != nil {
		select {
		case <-ready:
		case <-stopCtx.Done():
			if cancelProcessing != nil {
				cancelProcessing()
			}
			<-ready
		}
	}
	ep.mu.Lock()
	consumeCtx := ep.consumeCtx
	if ep.callbacksDone == nil {
		ep.callbacksDone = make(chan struct{})
	}
	ep.mu.Unlock()
	var closed <-chan struct{}
	if consumeCtx != nil {
		closed = consumeCtx.Closed()
		consumeCtx.Stop()
	}
	go func() { ep.active.Wait(); close(ep.callbacksDone) }()
	var stopErr error
	for _, done := range []<-chan struct{}{closed, ep.callbacksDone} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-stopCtx.Done():
			stopErr = stopCtx.Err()
			if cancelProcessing != nil {
				cancelProcessing()
			}
			<-done
		}
	}
	if cancelProcessing != nil {
		cancelProcessing()
	}

	// Drain and close the NATS connection
	if ep.natsConn != nil {
		if err := ep.natsConn.Drain(); err != nil {
			ep.logger.With("error", err).Error("Error draining NATS connection")
		}
		ep.natsConn.Close()
		ep.logger.Info("NATS connection closed")
	}

	// Close the publisher
	if ep.publisher != nil {
		if err := ep.publisher.Close(); err != nil {
			ep.logger.With("error", err).Error("Error closing publisher")
		}
	}

	ep.logger.Info("Event processor stopped successfully")
	if ep.stateStore != nil {
		ep.stateStore.Close()
	}
	return stopErr
}
