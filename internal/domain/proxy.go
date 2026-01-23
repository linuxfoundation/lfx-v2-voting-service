// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package domain

import (
	"context"

	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
)

// ITXProxyClient defines the interface for calling ITX voting service
type ITXProxyClient interface {
	// CreatePoll creates a new poll in ITX (maps to our "create vote")
	CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error)

	// GetPoll retrieves poll details from ITX
	GetPoll(ctx context.Context, pollID string) (*itx.PollResponse, error)

	// UpdatePoll updates a poll in ITX (only when status is "disabled")
	UpdatePoll(ctx context.Context, pollID string, req *itx.UpdatePollRequest) (*itx.PollResponse, error)

	// DeletePoll deletes a poll in ITX (only when status is "disabled")
	DeletePoll(ctx context.Context, pollID string) error
}
