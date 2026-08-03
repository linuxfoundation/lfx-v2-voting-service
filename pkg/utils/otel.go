// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// OTelConfig holds OpenTelemetry resource configuration. All exporter,
// protocol, endpoint, and propagator settings are read directly from standard
// OTEL_* environment variables by the autoexport/autoprop packages.
type OTelConfig struct {
	// ServiceName is the name of the service for resource identification.
	// Env: OTEL_SERVICE_NAME (default: "lfx-v2-voting-service")
	ServiceName string
	// ServiceVersion is the version of the service.
	// Env: OTEL_SERVICE_VERSION (no default; caller may set from build-time ldflags)
	ServiceVersion string
}

// OTelConfigFromEnv creates an OTelConfig from environment variables.
// See OTelConfig struct fields for supported environment variables.
func OTelConfigFromEnv() OTelConfig {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "lfx-v2-voting-service"
	}

	cfg := OTelConfig{
		ServiceName:    serviceName,
		ServiceVersion: os.Getenv("OTEL_SERVICE_VERSION"),
	}

	slog.With(
		"service-name", cfg.ServiceName,
		"version", cfg.ServiceVersion,
	).Debug("OTelConfig")

	return cfg
}

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context) (shutdown func(context.Context) error, err error) {
	return SetupOTelSDKWithConfig(ctx, OTelConfigFromEnv())
}

// SetupOTelSDKWithConfig bootstraps the OpenTelemetry pipeline with the provided configuration.
// If it does not return an error, make sure to call shutdown for proper cleanup.
//
// Exporter type, protocol, and endpoint are configured via standard OTEL_* env vars:
//   - OTEL_TRACES_EXPORTER, OTEL_METRICS_EXPORTER, OTEL_LOGS_EXPORTER (as supported by autoexport, e.g. "otlp", "none", "stdout")
//   - OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_PROTOCOL ("grpc" or "http/protobuf")
//   - OTEL_PROPAGATORS ("tracecontext,baggage,jaeger" etc.)
//   - OTEL_TRACES_SAMPLER ("always_on", "always_off", "traceidratio", "parentbased_*")
//   - OTEL_TRACES_SAMPLER_ARG (ratio for traceidratio, e.g. "0.5")
func SetupOTelSDKWithConfig(ctx context.Context, cfg OTelConfig) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Create resource with service information.
	res, err := newResource(cfg)
	if err != nil {
		handleErr(err)
		return
	}

	// Set up propagator from OTEL_PROPAGATORS env var.
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	// Set up trace provider.
	tracerProvider, err := newTraceProvider(ctx, res)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up metrics provider.
	metricsProvider, err := newMetricsProvider(ctx, res)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, metricsProvider.Shutdown)
	otel.SetMeterProvider(metricsProvider)

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(ctx, res)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return
}

// newResource creates an OpenTelemetry resource with service name and version attributes.
func newResource(cfg OTelConfig) (*resource.Resource, error) {
	var attrs []attribute.KeyValue
	if cfg.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceName(cfg.ServiceName))
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, attrs...),
	)
}

// newSampler creates a trace.Sampler from standard OTEL_TRACES_SAMPLER env vars.
// Supported values: "always_on", "always_off", "traceidratio",
// "parentbased_always_on", "parentbased_always_off", "parentbased_traceidratio".
// The ratio for traceidratio samplers is read from OTEL_TRACES_SAMPLER_ARG.
// Default (unset): "parentbased_always_on" per the OpenTelemetry specification.
func newSampler() trace.Sampler {
	sampler := os.Getenv("OTEL_TRACES_SAMPLER")
	arg := os.Getenv("OTEL_TRACES_SAMPLER_ARG")

	parseRatio := func() float64 {
		if arg == "" {
			return 1.0
		}
		r, err := strconv.ParseFloat(arg, 64)
		if err != nil || r < 0.0 || r > 1.0 {
			slog.Warn("invalid OTEL_TRACES_SAMPLER_ARG, using 1.0", "value", arg)
			return 1.0
		}
		return r
	}

	switch sampler {
	case "always_on":
		return trace.AlwaysSample()
	case "always_off":
		return trace.NeverSample()
	case "traceidratio":
		return trace.TraceIDRatioBased(parseRatio())
	case "parentbased_always_on":
		return trace.ParentBased(trace.AlwaysSample())
	case "parentbased_always_off":
		return trace.ParentBased(trace.NeverSample())
	case "parentbased_traceidratio":
		return trace.ParentBased(trace.TraceIDRatioBased(parseRatio()))
	default: // unset or unrecognized — use OTel spec default
		return trace.ParentBased(trace.AlwaysSample())
	}
}

// newTraceProvider creates a TracerProvider. The exporter type, protocol, and
// endpoint are configured via OTEL_TRACES_EXPORTER and OTEL_EXPORTER_OTLP_*
// environment variables (handled by autoexport).
func newTraceProvider(ctx context.Context, res *resource.Resource) (*trace.TracerProvider, error) {
	exporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, err
	}
	return trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSampler(newSampler()),
		trace.WithBatcher(exporter),
	), nil
}

// newMetricsProvider creates a MeterProvider. The exporter type, protocol, and
// endpoint are configured via OTEL_METRICS_EXPORTER and OTEL_EXPORTER_OTLP_*
// environment variables (handled by autoexport).
func newMetricsProvider(ctx context.Context, res *resource.Resource) (*metric.MeterProvider, error) {
	reader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return nil, err
	}
	return metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader),
	), nil
}

// newLoggerProvider creates a LoggerProvider. The exporter type, protocol, and
// endpoint are configured via OTEL_LOGS_EXPORTER and OTEL_EXPORTER_OTLP_*
// environment variables (handled by autoexport).
func newLoggerProvider(ctx context.Context, res *resource.Resource) (*log.LoggerProvider, error) {
	exporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return nil, err
	}
	return log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(exporter)),
	), nil
}
