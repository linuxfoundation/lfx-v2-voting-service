// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseWriter(t *testing.T) {
	t.Run("WriteHeader records the status and forwards it", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

		rw.WriteHeader(http.StatusTeapot)

		assert.Equal(t, http.StatusTeapot, rw.statusCode)
		assert.Equal(t, http.StatusTeapot, rec.Code)
	})

	t.Run("Write forwards the body and keeps the default status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

		n, err := rw.Write([]byte("hello"))

		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", rec.Body.String())
		assert.Equal(t, http.StatusOK, rw.statusCode)
	})
}

func TestRequestLoggerMiddleware(t *testing.T) {
	t.Run("logs request and response with the captured status", func(t *testing.T) {
		logs := captureLogs(t)
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		req := httptest.NewRequest(http.MethodGet, "/votes/abc?limit=5", nil)
		req.Header.Set("User-Agent", "test-agent")
		req.Header.Set(constants.EtagHeader, "etag-1")

		RequestLoggerMiddleware()(next).ServeHTTP(httptest.NewRecorder(), req)

		entries := logs()
		require.Len(t, entries, 3) // "log config", request, response
		reqEntry, respEntry := entries[1], entries[2]

		assert.Equal(t, "HTTP request", reqEntry["msg"])
		assert.Equal(t, "INFO", reqEntry["level"])
		assert.Equal(t, http.MethodGet, reqEntry["method"])
		assert.Equal(t, "/votes/abc", reqEntry["path"])
		assert.Equal(t, "limit=5", reqEntry["query"])
		assert.Equal(t, "test-agent", reqEntry["user_agent"])
		assert.Equal(t, "etag-1", reqEntry["req_header_etag"])

		assert.Equal(t, "HTTP response", respEntry["msg"])
		assert.Equal(t, "INFO", respEntry["level"])
		assert.EqualValues(t, http.StatusNotFound, respEntry["status"])
		assert.NotEmpty(t, respEntry["duration"])
	})

	t.Run("defaults the status to 200 when the handler never sets one", func(t *testing.T) {
		logs := captureLogs(t)
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})

		RequestLoggerMiddleware()(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/votes", nil))

		entries := logs()
		require.Len(t, entries, 3)
		assert.EqualValues(t, http.StatusOK, entries[2]["status"])
		_, hasEtag := entries[1]["req_header_etag"]
		assert.False(t, hasEtag, "etag attribute only added when the header is present")
	})

	t.Run("health checks are logged at debug level", func(t *testing.T) {
		for _, path := range constants.HealthCheckPaths {
			t.Run(path, func(t *testing.T) {
				logs := captureLogs(t)
				next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

				RequestLoggerMiddleware()(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

				entries := logs()
				require.Len(t, entries, 3)
				assert.Equal(t, "DEBUG", entries[1]["level"])
				assert.Equal(t, "DEBUG", entries[2]["level"])
			})
		}
	})

	t.Run("passes the request through to the next handler", func(t *testing.T) {
		captureLogs(t)
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusCreated)
		})
		rec := httptest.NewRecorder()

		RequestLoggerMiddleware()(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/votes", nil))

		assert.True(t, called)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})
}
