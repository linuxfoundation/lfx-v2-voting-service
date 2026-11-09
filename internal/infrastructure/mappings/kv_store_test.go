// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package mappings

import (
	"context"
	"errors"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBucket is a minimal in-memory jetstream.KeyValue; unimplemented methods panic.
type fakeBucket struct {
	jetstream.KeyValue
	data   map[string][]byte
	getErr error
}

type fakeEntry struct {
	jetstream.KeyValueEntry
	value []byte
}

func (e fakeEntry) Value() []byte { return e.value }

func (b *fakeBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	v, ok := b.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return fakeEntry{value: v}, nil
}

func (b *fakeBucket) Put(_ context.Context, key string, value []byte) (uint64, error) {
	b.data[key] = value
	return 1, nil
}

// TestKVStore_OnDiskFormat pins the key and value format of the v1-mappings bucket,
// which already-written data and external tools depend on.
func TestKVStore_OnDiskFormat(t *testing.T) {
	ctx := context.Background()
	bucket := &fakeBucket{data: map[string][]byte{}}
	store := NewKVStore(bucket)

	assert.Equal(t, domain.MappingAbsent, store.State(ctx, domain.MappingKindVote, "u1"))

	require.NoError(t, store.MarkSynced(ctx, domain.MappingKindVote, "u1"))
	assert.Equal(t, []byte("1"), bucket.data["vote.u1"])
	assert.Equal(t, domain.MappingSynced, store.State(ctx, domain.MappingKindVote, "u1"))

	require.NoError(t, store.MarkTombstoned(ctx, domain.MappingKindVoteResponse, "r1"))
	assert.Equal(t, []byte("!del"), bucket.data["vote_response.r1"])
	assert.Equal(t, domain.MappingTombstoned, store.State(ctx, domain.MappingKindVoteResponse, "r1"))

	require.NoError(t, store.MarkSynced(ctx, domain.MappingKindVoteResult, "p1"))
	assert.Contains(t, bucket.data, "vote_result.p1")
}

func TestKVStore_ReadErrorReportsAbsent(t *testing.T) {
	bucket := &fakeBucket{data: map[string][]byte{"vote.u1": []byte("1")}, getErr: errors.New("boom")}
	store := NewKVStore(bucket)
	assert.Equal(t, domain.MappingAbsent, store.State(context.Background(), domain.MappingKindVote, "u1"))
}
