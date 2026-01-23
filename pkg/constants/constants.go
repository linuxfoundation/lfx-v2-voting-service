// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

type contextKey string

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
