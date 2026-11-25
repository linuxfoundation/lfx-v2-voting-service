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

func TestAuthorizationMiddleware(t *testing.T) {
	run := func(t *testing.T, header string, set bool) (value any, called bool) {
		t.Helper()
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			value = r.Context().Value(constants.AuthorizationContextID)
		})
		req := httptest.NewRequest(http.MethodGet, "/votes", nil)
		if set {
			req.Header.Set(constants.AuthorizationHeader, header)
		}
		AuthorizationMiddleware()(next).ServeHTTP(httptest.NewRecorder(), req)
		return value, called
	}

	t.Run("stores the authorization header in the context", func(t *testing.T) {
		value, called := run(t, "Bearer token-1", true)
		require.True(t, called)
		assert.Equal(t, "Bearer token-1", value)
	})

	t.Run("stores an empty string when the header is absent", func(t *testing.T) {
		value, called := run(t, "", false)
		require.True(t, called)
		assert.Equal(t, "", value)
	})

	t.Run("does not leak the header onto the response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/votes", nil)
		req.Header.Set(constants.AuthorizationHeader, "Bearer secret")
		AuthorizationMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
		assert.Empty(t, rec.Header().Get(constants.AuthorizationHeader))
	})
}
