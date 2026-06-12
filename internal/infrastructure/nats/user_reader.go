// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	votingconstants "github.com/linuxfoundation/lfx-v2-voting-service/pkg/constants"
)

const userReaderTimeout = 10 * time.Second

// NATSUserReader implements domain.UserReader using NATS request/reply to the auth service.
type NATSUserReader struct {
	nc     Requester
	logger *slog.Logger
}

// NewUserReader creates a new NATS-based user reader.
func NewUserReader(nc Requester, logger *slog.Logger) *NATSUserReader {
	logger.Info("user reader initialized", "subject", votingconstants.AuthEmailToUsernameSubject)
	return &NATSUserReader{nc: nc, logger: logger}
}

// UsernameByEmail returns the LFX username for the LFID account that owns the given email address.
func (r *NATSUserReader) UsernameByEmail(ctx context.Context, email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", domain.ErrUserNotFound
	}

	reqCtx, cancel := context.WithTimeout(ctx, userReaderTimeout)
	defer cancel()

	msg, err := r.nc.RequestWithContext(reqCtx, votingconstants.AuthEmailToUsernameSubject, []byte(email))
	if err != nil {
		return "", fmt.Errorf("email_to_username request failed: %w", err)
	}

	body := strings.TrimSpace(string(msg.Data))
	if body == "" {
		return "", domain.ErrUserNotFound
	}

	if body[0] == '{' {
		var envelope struct {
			Success *bool  `json:"success"`
			Error   string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			return "", fmt.Errorf("failed to parse email_to_username response: %w", err)
		}
		if envelope.Success == nil {
			return "", fmt.Errorf("email_to_username response missing success field")
		}
		if !*envelope.Success {
			return "", domain.ErrUserNotFound
		}
		return "", fmt.Errorf("unexpected email_to_username success envelope")
	}

	return body, nil
}

var _ domain.UserReader = (*NATSUserReader)(nil)
