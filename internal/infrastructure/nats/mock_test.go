// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/mock"
)

// MockRequester is a mock implementation of [Requester].
type MockRequester struct {
	mock.Mock
}

// RequestWithContext mocks a NATS request/reply call.
func (m *MockRequester) RequestWithContext(ctx context.Context, subj string, data []byte) (*natsgo.Msg, error) {
	args := m.Called(ctx, subj, data)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*natsgo.Msg), args.Error(1)
}
