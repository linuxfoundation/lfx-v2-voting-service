// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"

	natsgo "github.com/nats-io/nats.go"
)

// Requester is the subset of nats.Conn used for NATS request/reply calls.
type Requester interface {
	RequestWithContext(ctx context.Context, subj string, data []byte) (*natsgo.Msg, error)
}
