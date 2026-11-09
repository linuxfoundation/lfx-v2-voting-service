// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package mappings implements domain.MappingStore adapters.
package mappings

import (
	"context"
	"fmt"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// syncedValue is written to a mapping key once a resource has been synced.
	syncedValue = "1"

	// TombstoneMarker is written to a mapping key after successful deletion,
	// preventing duplicate delete events if the same KV delete is redelivered.
	// It is part of the on-disk format of the v1-mappings bucket.
	TombstoneMarker = "!del"
)

// mappingKey builds the v1-mappings key for a resource, e.g. "vote.<uid>".
func mappingKey(kind domain.MappingKind, uid string) string {
	return fmt.Sprintf("%s.%s", kind, uid)
}

// KVStore is a domain.MappingStore backed by the NATS JetStream v1-mappings bucket.
type KVStore struct {
	kv jetstream.KeyValue
}

// NewKVStore creates a mapping store on top of the given v1-mappings bucket.
func NewKVStore(kv jetstream.KeyValue) *KVStore {
	return &KVStore{kv: kv}
}

// State implements domain.MappingStore.
func (s *KVStore) State(ctx context.Context, kind domain.MappingKind, uid string) domain.MappingState {
	entry, err := s.kv.Get(ctx, mappingKey(kind, uid))
	if err != nil {
		return domain.MappingAbsent
	}
	if string(entry.Value()) == TombstoneMarker {
		return domain.MappingTombstoned
	}
	return domain.MappingSynced
}

// MarkSynced implements domain.MappingStore.
func (s *KVStore) MarkSynced(ctx context.Context, kind domain.MappingKind, uid string) error {
	_, err := s.kv.Put(ctx, mappingKey(kind, uid), []byte(syncedValue))
	return err
}

// MarkTombstoned implements domain.MappingStore.
func (s *KVStore) MarkTombstoned(ctx context.Context, kind domain.MappingKind, uid string) error {
	_, err := s.kv.Put(ctx, mappingKey(kind, uid), []byte(TombstoneMarker))
	return err
}
