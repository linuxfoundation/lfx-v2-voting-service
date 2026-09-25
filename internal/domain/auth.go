// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"
	"log/slog"
)

// Authenticator defines the interface for JWT authentication
type Authenticator interface {
	// ParsePrincipal validates the JWT and extracts the principal (user ID)
	ParsePrincipal(ctx context.Context, token string, logger *slog.Logger) (string, error)
}
