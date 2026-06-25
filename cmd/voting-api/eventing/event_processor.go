// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	infraNATS "github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	V1MappingsBucket = "v1-mappings"
)

// EventProcessor handles NATS KV bucket event processing
type EventProcessor struct {
	natsConn      *nats.Conn
	jsInstance    jetstream.JetStream
	consumer      jetstream.Consumer
	consumeCtx    jetstream.ConsumeContext
	publisher     domain.EventPublisher
	idMapper      domain.IDMapper
	mappingsKV    jetstream.KeyValue
	v1ObjectsKV   jetstream.KeyValue
	inviteHandler *VoteResponseInviteHandler
	logger        *slog.Logger
	config        eventing.Config
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	cfg eventing.Config,
	idMapper domain.IDMapper,
	logger *slog.Logger,
	inviteCfg InviteFeatureConfig,
) (*EventProcessor, error) {
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
	publisher := eventing.NewNATSPublisher(conn, logger)

	// Access the V1 mappings KV bucket
	mappingsKV, err := jsContext.KeyValue(context.Background(), V1MappingsBucket)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to access %s KV bucket: %w", V1MappingsBucket, err)
	}

	var v1ObjectsKV jetstream.KeyValue
	var inviteHandler *VoteResponseInviteHandler
	if inviteCfg.Enabled &&
		strings.TrimSpace(inviteCfg.SelfServeBaseURL) != "" {
		v1ObjectsKV, err = jsContext.KeyValue(context.Background(), V1ObjectsBucket)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to access %s KV bucket: %w", V1ObjectsBucket, err)
		}
		inviteHandler = &VoteResponseInviteHandler{
			inviteSender:     infraNATS.NewInviteSender(conn, logger),
			userReader:       infraNATS.NewUserReader(conn, logger),
			v1ObjectsKV:      v1ObjectsKV,
			v1MappingsKV:     mappingsKV,
			selfServeBaseURL: inviteCfg.SelfServeBaseURL,
		}
	}

	return &EventProcessor{
		natsConn:      conn,
		jsInstance:    jsContext,
		publisher:     publisher,
		idMapper:      idMapper,
		mappingsKV:    mappingsKV,
		v1ObjectsKV:   v1ObjectsKV,
		inviteHandler: inviteHandler,
		logger:        logger,
		config:        cfg,
	}, nil
}

// Start starts the event processor
func (ep *EventProcessor) Start(ctx context.Context) error {
	ep.logger.Info("Starting event processor", "consumer_name", ep.config.ConsumerName)

	// Create or update consumer
	consumer, err := ep.jsInstance.CreateOrUpdateConsumer(ctx, ep.config.StreamName, jetstream.ConsumerConfig{
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
		return fmt.Errorf("failed to create or update consumer: %w", err)
	}
	ep.consumer = consumer

	// Start consuming messages
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		kvMessageHandler(ctx, msg, ep.publisher, ep.idMapper, ep.mappingsKV, ep.inviteHandler, ep.logger)
	}, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		ep.logger.With("error", err).Error("KV consumer error encountered")
	}))
	if err != nil {
		return fmt.Errorf("failed to start consuming messages: %w", err)
	}
	ep.consumeCtx = consumeCtx

	ep.logger.Info("Event processor started successfully")

	// Block until context is cancelled
	<-ctx.Done()

	ep.logger.Info("Event processor context cancelled")
	return nil
}

// Stop stops the event processor gracefully
func (ep *EventProcessor) Stop() error {
	ep.logger.Info("Stopping event processor...")

	// Stop the consumer
	if ep.consumeCtx != nil {
		ep.consumeCtx.Stop()
		ep.logger.Info("Consumer stopped")
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
	return nil
}
