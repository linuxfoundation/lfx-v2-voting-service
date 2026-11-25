// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRequestID(t *testing.T) {
	first := generateRequestID()
	second := generateRequestID()

	_, err := uuid.Parse(first)
	require.NoError(t, err, "request ID should be a valid UUID")
	assert.NotEqual(t, first, second, "each call must produce a unique ID")
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("reuses the incoming request ID", func(t *testing.T) {
		var ctxID any
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			ctxID = r.Context().Value(constants.RequestIDContextID)
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/votes", nil)
		req.Header.Set(constants.RequestIDHeader, "req-123")

		RequestIDMiddleware()(next).ServeHTTP(rec, req)

		assert.Equal(t, "req-123", ctxID)
		assert.Equal(t, "req-123", rec.Header().Get(constants.RequestIDHeader))
	})

	t.Run("generates a request ID when none is supplied", func(t *testing.T) {
		var ctxID any
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			ctxID = r.Context().Value(constants.RequestIDContextID)
		})
		rec := httptest.NewRecorder()

		RequestIDMiddleware()(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/votes", nil))

		id, ok := ctxID.(string)
		require.True(t, ok, "context value should be a string")
		_, err := uuid.Parse(id)
		require.NoError(t, err)
		assert.Equal(t, id, rec.Header().Get(constants.RequestIDHeader))
	})

	t.Run("adds the request ID to the logging context", func(t *testing.T) {
		logs := captureLogs(t)
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			slog.InfoContext(r.Context(), "inside handler")
		})
		req := httptest.NewRequest(http.MethodGet, "/votes", nil)
		req.Header.Set(constants.RequestIDHeader, "req-456")

		RequestIDMiddleware()(next).ServeHTTP(httptest.NewRecorder(), req)

		entries := logs()
		require.Len(t, entries, 2) // "log config" + "inside handler"
		assert.Equal(t, "req-456", entries[1][constants.RequestIDHeader])
	})
}
