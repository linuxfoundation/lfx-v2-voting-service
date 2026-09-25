// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

import "slices"

type contextKey string

// HealthCheckPaths contains the paths considered health check endpoints.
var HealthCheckPaths = []string{"/health", "/livez", "/readyz"}

// IsHealthCheckPath reports whether path is a health check endpoint.
func IsHealthCheckPath(path string) bool {
	return slices.Contains(HealthCheckPaths, path)
}

const (
	// HTTP Headers
	AuthorizationHeader = "authorization"
	RequestIDHeader     = "X-REQUEST-ID"
	EtagHeader          = "ETag"

	// Context Keys
	AuthorizationContextID contextKey = "authorization"
	RequestIDContextID     contextKey = "X-REQUEST-ID"
	PrincipalContextID     contextKey = "principal"
)
