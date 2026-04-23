// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	nats "github.com/nats-io/nats.go"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

const authServiceUsernameToSubSubject = "lfx.auth-service.username_to_sub"

// NATSUserLookup implements the V1UserLookup interface using the auth service over NATS
type NATSUserLookup struct {
	nc     *nats.Conn
	logger *slog.Logger
}

// NewNATSUserLookup creates a new NATS-based v1 user lookup service
func NewNATSUserLookup(nc *nats.Conn, logger *slog.Logger) *NATSUserLookup {
	return &NATSUserLookup{
		nc:     nc,
		logger: logger,
	}
}

// MapUsernameToAuthSub converts a v1 username to the Auth0 "sub" format by calling the
// auth service over NATS on subject lfx.auth-service.username_to_sub.
func (l *NATSUserLookup) MapUsernameToAuthSub(ctx context.Context, username string) (string, error) {
	if username == "" {
		return "", nil
	}

	msg, err := l.nc.RequestWithContext(ctx, authServiceUsernameToSubSubject, []byte(username))
	if err != nil {
		return "", fmt.Errorf("auth service username lookup failed: %w", err)
	}

	sub := string(msg.Data)
	if sub == "" {
		return "", fmt.Errorf("auth service returned empty sub for username %q", username)
	}

	// The auth service returns a plain sub string on success, or a JSON error object on failure.
	// Detect the error case so we don't forward the raw JSON as an FGA user identifier.
	if sub[0] == '{' {
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if jsonErr := json.Unmarshal(msg.Data, &errResp); jsonErr == nil && !errResp.Success {
			return "", fmt.Errorf("auth service could not resolve username %q to auth sub: %s", username, errResp.Error)
		}
	}

	return sub, nil
}

// Ensure NATSUserLookup implements V1UserLookup
var _ domain.V1UserLookup = (*NATSUserLookup)(nil)
