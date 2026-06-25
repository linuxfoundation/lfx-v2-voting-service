// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/mock"
)

// mockKeyValue is a testify mock for jetstream.KeyValue used in event handler tests.
type mockKeyValue struct {
	mock.Mock
}

func (m *mockKeyValue) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyValueEntry), args.Error(1)
}

func (m *mockKeyValue) GetRevision(ctx context.Context, key string, revision uint64) (jetstream.KeyValueEntry, error) {
	args := m.Called(ctx, key, revision)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyValueEntry), args.Error(1)
}

func (m *mockKeyValue) Put(ctx context.Context, key string, value []byte) (uint64, error) {
	args := m.Called(ctx, key, value)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockKeyValue) PutString(ctx context.Context, key string, value string) (uint64, error) {
	args := m.Called(ctx, key, value)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockKeyValue) Create(ctx context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	args := m.Called(ctx, key, value)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockKeyValue) Update(ctx context.Context, key string, value []byte, revision uint64) (uint64, error) {
	args := m.Called(ctx, key, value, revision)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *mockKeyValue) Delete(ctx context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *mockKeyValue) Purge(ctx context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *mockKeyValue) Watch(ctx context.Context, keys string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	args := m.Called(ctx, keys)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyWatcher), args.Error(1)
}

func (m *mockKeyValue) WatchAll(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyWatcher), args.Error(1)
}

func (m *mockKeyValue) WatchFiltered(ctx context.Context, keys []string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	args := m.Called(ctx, keys)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyWatcher), args.Error(1)
}

func (m *mockKeyValue) Keys(ctx context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockKeyValue) ListKeys(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyLister), args.Error(1)
}

func (m *mockKeyValue) ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error) {
	args := m.Called(ctx, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyLister), args.Error(1)
}

func (m *mockKeyValue) History(ctx context.Context, key string, _ ...jetstream.WatchOpt) ([]jetstream.KeyValueEntry, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]jetstream.KeyValueEntry), args.Error(1)
}

func (m *mockKeyValue) Bucket() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockKeyValue) Status(ctx context.Context) (jetstream.KeyValueStatus, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(jetstream.KeyValueStatus), args.Error(1)
}

func (m *mockKeyValue) PurgeDeletes(ctx context.Context, _ ...jetstream.KVPurgeOpt) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockKeyValueEntry struct {
	key   string
	value []byte
}

func (e mockKeyValueEntry) Key() string                     { return e.key }
func (e mockKeyValueEntry) Value() []byte                   { return e.value }
func (e mockKeyValueEntry) Revision() uint64                { return 1 }
func (e mockKeyValueEntry) Created() time.Time              { return time.Now() }
func (e mockKeyValueEntry) Delta() uint64                   { return 0 }
func (e mockKeyValueEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }
func (e mockKeyValueEntry) Bucket() string                  { return "test" }
