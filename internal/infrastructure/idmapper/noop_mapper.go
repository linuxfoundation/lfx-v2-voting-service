// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
)

// NoOpMapper is a no-op ID mapper that returns the input ID unchanged.
// This is useful for local development when the NATS mapping service is not available.
type NoOpMapper struct{}

// NewNoOpMapper creates a new no-op ID mapper
func NewNoOpMapper() *NoOpMapper {
	return &NoOpMapper{}
}

// Close is a no-op for the NoOpMapper
func (m *NoOpMapper) Close() {}

// MapProjectV2ToV1 returns the input ID unchanged
func (m *NoOpMapper) MapProjectV2ToV1(ctx context.Context, v2UID string) (string, error) {
	return v2UID, nil
}

// MapProjectV1ToV2 returns the input ID unchanged
func (m *NoOpMapper) MapProjectV1ToV2(ctx context.Context, v1SFID string) (string, error) {
	return v1SFID, nil
}

// MapCommitteeV2ToV1 returns the input ID unchanged
func (m *NoOpMapper) MapCommitteeV2ToV1(ctx context.Context, v2UID string) (string, error) {
	return v2UID, nil
}

// MapCommitteeV1ToV2 returns the input ID unchanged
func (m *NoOpMapper) MapCommitteeV1ToV2(ctx context.Context, v1SFID string) (string, error) {
	return v1SFID, nil
}
