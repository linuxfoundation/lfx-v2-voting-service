// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStore is an in-memory domain.MappingStore. It is the test adapter at the
// mapping-store seam; the production adapter is mappings.KVStore.
type memoryStore struct {
	mu     sync.Mutex
	states map[string]domain.MappingState
	// PutErr, when set, is returned by MarkSynced and MarkTombstoned.
	PutErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{states: map[string]domain.MappingState{}}
}

func memoryKey(kind domain.MappingKind, uid string) string {
	return fmt.Sprintf("%s.%s", kind, uid)
}

func (s *memoryStore) State(_ context.Context, kind domain.MappingKind, uid string) domain.MappingState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[memoryKey(kind, uid)]
}

func (s *memoryStore) MarkSynced(_ context.Context, kind domain.MappingKind, uid string) error {
	return s.set(kind, uid, domain.MappingSynced)
}

func (s *memoryStore) MarkTombstoned(_ context.Context, kind domain.MappingKind, uid string) error {
	return s.set(kind, uid, domain.MappingTombstoned)
}

// Seed sets a resource's state directly, for test setup.
func (s *memoryStore) Seed(kind domain.MappingKind, uid string, state domain.MappingState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[memoryKey(kind, uid)] = state
}

func (s *memoryStore) set(kind domain.MappingKind, uid string, state domain.MappingState) error {
	if s.PutErr != nil {
		return s.PutErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[memoryKey(kind, uid)] = state
	return nil
}

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	assert.Equal(t, domain.MappingAbsent, store.State(ctx, domain.MappingKindVote, "u1"))
	require.NoError(t, store.MarkSynced(ctx, domain.MappingKindVote, "u1"))
	assert.Equal(t, domain.MappingSynced, store.State(ctx, domain.MappingKindVote, "u1"))
	require.NoError(t, store.MarkTombstoned(ctx, domain.MappingKindVote, "u1"))
	assert.Equal(t, domain.MappingTombstoned, store.State(ctx, domain.MappingKindVote, "u1"))

	store.PutErr = errors.New("put failed")
	assert.Error(t, store.MarkSynced(ctx, domain.MappingKindVote, "u2"))
}
