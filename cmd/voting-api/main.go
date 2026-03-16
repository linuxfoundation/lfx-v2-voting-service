// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apieventing "github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api/eventing"
	openapisvr "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/openapi/server"
	votesvr "github.com/linuxfoundation/lfx-v2-voting-service/gen/http/vote/server"
	votesvc "github.com/linuxfoundation/lfx-v2-voting-service/gen/vote"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/auth"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/idmapper"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/proxy"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/middleware"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service"
	goahttp "goa.design/goa/v3/http"
)

// Build-time variables set via ldflags
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Load configuration from environment
	cfg := loadConfig()

	// Initialize structured logging
	logging.InitStructureLogConfig()
	logger := slog.Default()

	logger.Info("Starting voting service",
		"version", Version,
		"build_time", BuildTime,
		"git_commit", GitCommit,
		"port", cfg.Port,
		"itx_base_url", cfg.ITXBaseURL,
	)

	// Initialize JWT authenticator
	jwtAuth, err := auth.NewJWTAuth(auth.Config{
		JWKSURL:            cfg.JWKSURL,
		Audience:           cfg.Audience,
		MockLocalPrincipal: cfg.MockLocalPrincipal,
	})
	if err != nil {
		logger.Error("Failed to initialize JWT auth", "error", err)
		return 1
	}

	// Initialize ITX proxy client with OAuth2 M2M authentication using private key
	proxyClient := proxy.NewClient(proxy.Config{
		BaseURL:     cfg.ITXBaseURL,
		Auth0Domain: cfg.ITXAuth0Domain,
		ClientID:    cfg.ITXClientID,
		PrivateKey:  cfg.ITXPrivateKey,
		Audience:    cfg.ITXAudience,
		Timeout:     cfg.ITXTimeout,
	})

	// Initialize ID mapper for v1/v2 ID conversions
	var idMapper domain.IDMapper
	if cfg.IDMappingDisabled {
		logger.Warn("ID mapping is DISABLED - using no-op mapper (IDs will pass through unchanged)")
		idMapper = idmapper.NewNoOpMapper()
	} else {
		natsMapper, err := idmapper.NewNATSMapper(idmapper.Config{
			URL:     cfg.NATSURL,
			Timeout: cfg.NATSTimeout,
		})
		if err != nil {
			logger.Error("Failed to initialize ID mapper", "error", err)
			return 1
		}
		defer natsMapper.Close()
		idMapper = natsMapper
	}

	// Create shutdown channel for coordinating graceful shutdown
	shutdown := make(chan struct{}, 1)

	// Initialize event processor (if enabled)
	var eventProcessor *apieventing.EventProcessor
	var eventProcessorCtx context.Context
	var eventProcessorCancel context.CancelFunc
	if cfg.EventProcessingEnabled {
		logger.Info("Event processing is ENABLED - initializing event processor")
		ep, err := apieventing.NewEventProcessor(eventing.Config{
			NATSURL:      cfg.NATSURL,
			ConsumerName: cfg.EventConsumerName,
			StreamName:   cfg.EventStreamName,
			FilterSubjects: []string{
				"$KV.v1-objects.itx-poll.>",
				"$KV.v1-objects.itx-poll-vote.>",
			},
			MaxDeliver:    3,
			AckWait:       30 * time.Second,
			MaxAckPending: 1000,
		}, idMapper, logger)
		if err != nil {
			logger.Error("Failed to initialize event processor", "error", err)
			return 1
		}
		eventProcessor = ep

		// Create context for event processor lifecycle
		eventProcessorCtx, eventProcessorCancel = context.WithCancel(context.Background())
		defer eventProcessorCancel()

		// Start event processor in goroutine
		go func() {
			if err := eventProcessor.Start(eventProcessorCtx); err != nil {
				logger.Error("Event processor error", "error", err)
				// Signal shutdown instead of calling os.Exit
				select {
				case shutdown <- struct{}{}:
				default:
				}
			}
		}()
		logger.Info("Event processor started in background")
	} else {
		logger.Info("Event processing is DISABLED - skipping event processor initialization")
	}

	// Initialize service layer
	voteService := service.NewVoteService(jwtAuth, proxyClient, idMapper, logger)
	voteResponseService := service.NewVoteResponseService(jwtAuth, proxyClient, idMapper, logger)

	// Initialize API layer
	votingAPI := NewVotingAPI(voteService, voteResponseService)

	// Create Goa endpoints
	votingEndpoints := votesvc.NewEndpoints(votingAPI)

	// Create HTTP muxer
	mux := goahttp.NewMuxer()

	// Resolve kodata path for serving OpenAPI spec files
	koDataPath := os.Getenv("KO_DATA_PATH")
	if koDataPath == "" {
		koDataPath = "../../gen/http"
	}
	koDataDir := http.Dir(koDataPath)

	// Mount HTTP handlers
	votingServer := votesvr.New(votingEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	votesvr.Mount(mux, votingServer)

	// Mount OpenAPI spec file handlers
	openapiServer := openapisvr.New(nil, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil, koDataDir, koDataDir, koDataDir, koDataDir)
	openapisvr.Mount(mux, openapiServer)

	// Add health check endpoints
	mux.Handle("GET", "/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.Handle("GET", "/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK\n"))
	})

	mux.Handle("GET", "/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK\n"))
	})

	// Wrap with middleware stack
	var handler http.Handler = mux
	handler = middleware.RequestLoggerMiddleware()(handler)
	handler = middleware.RequestIDMiddleware()(handler)
	handler = middleware.AuthorizationMiddleware()(handler)

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("HTTP server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
			// Signal shutdown instead of calling os.Exit
			select {
			case shutdown <- struct{}{}:
			default:
			}
		}
	}()

	// Wait for interrupt signal or shutdown event
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Info("Received interrupt signal")
	case <-shutdown:
		logger.Info("Received shutdown signal from background goroutine")
	}

	logger.Info("Shutting down server...")

	// Stop event processor first (if enabled)
	if eventProcessor != nil {
		logger.Info("Stopping event processor...")
		// Cancel the event processor context to stop the Start method
		if eventProcessorCancel != nil {
			eventProcessorCancel()
		}
		// Then stop the consumer and cleanup resources
		if err := eventProcessor.Stop(); err != nil {
			logger.Error("Error stopping event processor", "error", err)
		}
	}

	// Graceful shutdown of HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
		return 1
	}

	logger.Info("Server stopped gracefully")
	return 0
}

// config holds the application configuration
type config struct {
	Port                   string
	JWKSURL                string
	Audience               string
	MockLocalPrincipal     string
	ITXBaseURL             string
	ITXAuth0Domain         string
	ITXClientID            string
	ITXPrivateKey          string
	ITXAudience            string
	ITXTimeout             time.Duration
	NATSURL                string
	NATSTimeout            time.Duration
	IDMappingDisabled      bool
	EventProcessingEnabled bool
	EventConsumerName      string
	EventStreamName        string
}

// loadConfig loads configuration from environment variables
func loadConfig() config {
	return config{
		Port:                   getEnv("PORT", "8080"),
		JWKSURL:                getEnv("JWKS_URL", "http://heimdall:4457/.well-known/jwks"),
		Audience:               getEnv("AUDIENCE", "lfx-v2-voting-service"),
		MockLocalPrincipal:     getEnv("JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL", ""),
		ITXBaseURL:             getEnv("ITX_BASE_URL", "https://api.dev.itx.linuxfoundation.org/"),
		ITXAuth0Domain:         getEnv("ITX_AUTH0_DOMAIN", "linuxfoundation-dev.auth0.com"),
		ITXClientID:            getEnv("ITX_CLIENT_ID", ""),
		ITXPrivateKey:          getEnv("ITX_CLIENT_PRIVATE_KEY", ""),
		ITXAudience:            getEnv("ITX_AUDIENCE", "https://api.dev.itx.linuxfoundation.org/"),
		ITXTimeout:             30 * time.Second,
		NATSURL:                getEnv("NATS_URL", "nats://nats:4222"),
		NATSTimeout:            5 * time.Second,
		IDMappingDisabled:      getEnv("ID_MAPPING_DISABLED", "") == "true",
		EventProcessingEnabled: getEnv("EVENT_PROCESSING_ENABLED", "true") == "true",
		EventConsumerName:      getEnv("EVENT_CONSUMER_NAME", "voting-service-kv-consumer"),
		EventStreamName:        getEnv("EVENT_STREAM_NAME", "KV_v1-objects"),
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
