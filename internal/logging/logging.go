// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package logging contains the logging functionality for the voting service.
package logging

import (
	"context"
	"log"
	"log/slog"
	"os"

	slogotel "github.com/remychantenay/slog-otel"
)

type ctxKey string

const (
	slogFields      ctxKey = "slog_fields"
	logLevelDefault        = slog.LevelDebug

	// Log levels
	debug = "debug"
	warn  = "warn"
	err   = "error"
	info  = "info"
)

type contextHandler struct {
	slog.Handler
}

// Handle adds contextual attributes to the Record before calling the underlying handler
func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs, ok := ctx.Value(slogFields).([]slog.Attr); ok {
		for _, v := range attrs {
			r.AddAttrs(v)
		}
	}

	return h.Handler.Handle(ctx, r)
}

// AppendCtx adds an slog attribute to the provided context so that it will be
// included in any Record created with such context
func AppendCtx(parent context.Context, attr slog.Attr) context.Context {
	if parent == nil {
		parent = context.Background()
	}

	if v, ok := parent.Value(slogFields).([]slog.Attr); ok {
		// Create a new slice to avoid race conditions when multiple goroutines
		// append to the same parent context
		newV := make([]slog.Attr, len(v), len(v)+1)
		copy(newV, v)
		newV = append(newV, attr)
		return context.WithValue(parent, slogFields, newV)
	}

	v := []slog.Attr{}
	v = append(v, attr)
	return context.WithValue(parent, slogFields, v)
}

// InitStructureLogConfig sets the structured log behavior
func InitStructureLogConfig() slog.Handler {
	logOptions := &slog.HandlerOptions{}
	var h slog.Handler

	// Configure log level
	logLevel := os.Getenv("LOG_LEVEL")
	switch logLevel {
	case debug:
		logOptions.Level = slog.LevelDebug
	case warn:
		logOptions.Level = slog.LevelWarn
	case err:
		logOptions.Level = slog.LevelError
	case info:
		logOptions.Level = slog.LevelInfo
	default:
		logOptions.Level = logLevelDefault
	}

	// Configure source information
	addSource := os.Getenv("LOG_ADD_SOURCE")
	logOptions.AddSource = addSource == "true" || addSource == "t" || addSource == "1"

	h = slog.NewJSONHandler(os.Stdout, logOptions)
	log.SetFlags(log.Llongfile)

	// Wrap with slog-otel handler to add trace_id and span_id from context
	otelHandler := slogotel.OtelHandler{Next: h}

	// Wrap with contextHandler to support context-based attributes
	logger := contextHandler{otelHandler}
	slog.SetDefault(slog.New(logger))

	slog.Info("log config",
		"logLevel", logOptions.Level,
		"addSource", logOptions.AddSource,
	)

	return h
}
