// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package middleware

import (
	"context"
	"net/http"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
)

// AuthorizationMiddleware creates a middleware that adds authorization to the context
func AuthorizationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try to get authorization from the header
			authorization := r.Header.Get(constants.AuthorizationHeader)

			// Add authorization to context
			ctx := context.WithValue(r.Context(), constants.AuthorizationContextID, authorization)

			// Create a new request with the updated context
			r = r.WithContext(ctx)

			// Call the next handler
			next.ServeHTTP(w, r)
		})
	}
}
