// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/logging"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
)

// RequestLoggerMiddleware creates a middleware that logs HTTP requests
func RequestLoggerMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Add request URL attributes to the context so that they can be used in all request handler logs
			ctx := r.Context()
			ctx = logging.AppendCtx(ctx, slog.String("method", r.Method))
			ctx = logging.AppendCtx(ctx, slog.String("path", r.URL.Path))
			ctx = logging.AppendCtx(ctx, slog.String("query", r.URL.RawQuery))
			ctx = logging.AppendCtx(ctx, slog.String("host", r.Host))
			ctx = logging.AppendCtx(ctx, slog.String("user_agent", r.UserAgent()))
			ctx = logging.AppendCtx(ctx, slog.String("remote_addr", r.RemoteAddr))

			if r.Header.Get(constants.EtagHeader) != "" {
				ctx = logging.AppendCtx(ctx, slog.String("req_header_etag", r.Header.Get(constants.EtagHeader)))
			}

			isHealthCheck := r.URL.Path == "/health" || r.URL.Path == "/livez" || r.URL.Path == "/readyz"

			// Create a new request with the updated context
			r = r.WithContext(ctx)

			// Create a response writer wrapper to capture status code
			ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			if isHealthCheck {
				slog.DebugContext(ctx, "HTTP request")
			} else {
				slog.InfoContext(ctx, "HTTP request")
			}

			// Call the next handler
			next.ServeHTTP(ww, r)

			// Calculate duration
			duration := time.Since(start)

			if isHealthCheck {
				slog.DebugContext(ctx, "HTTP response", "status", ww.statusCode, "duration", duration.String())
				return
			}
			// Log the response
			slog.InfoContext(ctx, "HTTP response", "status", ww.statusCode, "duration", duration.String())
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}
