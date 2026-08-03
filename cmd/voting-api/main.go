// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	natsgo "github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

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
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/utils"
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

	// Set up OpenTelemetry SDK.
	// Environment variable OTEL_SERVICE_VERSION takes precedence over
	// the build-time Version variable.
	otelConfig := utils.OTelConfigFromEnv()
	if otelConfig.ServiceVersion == "" {
		otelConfig.ServiceVersion = Version
	}
	otelShutdown, err := utils.SetupOTelSDKWithConfig(context.Background(), otelConfig)
	if err != nil {
		logger.Error("error setting up OpenTelemetry SDK", "error", err)
		return 1
	}
	// Handle shutdown properly so nothing leaks.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if shutdownErr := otelShutdown(ctx); shutdownErr != nil {
			logger.Error("error shutting down OpenTelemetry SDK", "error", shutdownErr)
		}
	}()

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

	inviteCfg := parseInviteConfig(logger)

	// Start invite_accepted subscriber independently of KV event processing.
	var inviteAcceptedSub *apieventing.InviteAcceptedSubscriber
	var inviteNatsConn *natsgo.Conn
	if inviteCfg.Enabled {
		nc, err := natsgo.Connect(cfg.NATSURL)
		if err != nil {
			logger.Warn("failed to connect to NATS for invite_accepted subscriber; continuing without enrichment", "error", err)
		} else {
			sub := apieventing.NewInviteAcceptedSubscriber(nc, proxyClient, logger)
			if err := sub.Start(context.Background()); err != nil {
				nc.Close()
				logger.Warn("failed to start invite_accepted subscriber; continuing without enrichment", "error", err)
			} else {
				inviteNatsConn = nc
				inviteAcceptedSub = sub
			}
		}
	}

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
		}, idMapper, logger, inviteCfg)
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

	// Register route-tagging middleware inside chi's routing chain so that
	// http.route is set on the OTel span after chi has matched the route pattern.
	// The span name is also updated here to avoid high-cardinality names from
	// using raw URL paths (which contain actual path parameter values).
	// Must be registered before Mount calls per chi convention.
	// Reads RoutePattern after next.ServeHTTP because chi populates the pattern
	// during routing (inside ServeHTTP), not before.
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rctx := chi.RouteContext(r.Context())
				if rctx != nil {
					routePattern := rctx.RoutePattern()
					if routePattern != "" {
						if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
							labeler.Add(semconv.HTTPRoute(routePattern))
						}
						span := trace.SpanFromContext(r.Context())
						span.SetAttributes(semconv.HTTPRoute(routePattern))
						span.SetName(r.Method + " " + routePattern)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	})

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
	handler = otelhttp.NewHandler(handler, "voting-service",
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !constants.IsHealthCheckPath(r.URL.Path)
		}),
	)

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

	if inviteAcceptedSub != nil {
		logger.Info("Stopping invite_accepted subscriber...")
		inviteAcceptedSub.Stop()
	}
	if inviteNatsConn != nil {
		inviteNatsConn.Close()
	}

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

func parseInviteConfig(logger *slog.Logger) apieventing.InviteFeatureConfig {
	raw := os.Getenv("INVITES_ENABLED")
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		if strings.EqualFold(raw, "yes") {
			enabled = true
		} else if raw != "" {
			logger.Warn("unrecognised INVITES_ENABLED value; feature disabled", "value", raw)
		}
	}

	selfServeBaseURL := os.Getenv("LFX_SELF_SERVE_BASE_URL")
	if selfServeBaseURL == "" {
		switch getEnv("LFX_ENVIRONMENT", "prod") {
		case "prod":
			selfServeBaseURL = "https://app.lfx.dev"
		case "staging":
			selfServeBaseURL = "https://app.staging.lfx.dev"
		default:
			selfServeBaseURL = "https://app.dev.lfx.dev"
		}
	}

	if enabled {
		parsed, err := url.ParseRequestURI(selfServeBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			logger.Warn("LFX_SELF_SERVE_BASE_URL is missing or invalid; outbound invite sending disabled (invite_accepted subscriber remains active)",
				"url", selfServeBaseURL,
				"error", err,
			)
			selfServeBaseURL = ""
		}
	}

	return apieventing.InviteFeatureConfig{
		Enabled:          enabled,
		SelfServeBaseURL: selfServeBaseURL,
	}
}
