// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestIsTransientError(t *testing.T) {
	t.Run("returns false for nil error", func(t *testing.T) {
		result := isTransientError(nil)
		assert.False(t, result)
	})

	t.Run("returns true for timeout error", func(t *testing.T) {
		err := errors.New("request timeout occurred")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for connection error", func(t *testing.T) {
		err := errors.New("connection refused")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for unavailable error", func(t *testing.T) {
		err := errors.New("service unavailable")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns true for deadline error", func(t *testing.T) {
		err := errors.New("deadline exceeded")
		result := isTransientError(err)
		assert.True(t, result)
	})

	t.Run("returns false for permanent error", func(t *testing.T) {
		err := errors.New("invalid data format")
		result := isTransientError(err)
		assert.False(t, result)
	})

	t.Run("returns true for domain ErrorTypeUnavailable", func(t *testing.T) {
		err := domain.NewUnavailableError("id mapper temporarily overloaded")
		result := isTransientError(err)
		assert.True(t, result)
	})
}

type mutableSourceFence struct{ current bool }

func (f *mutableSourceFence) Current(context.Context) (bool, error) { return f.current, nil }

func TestResourceStaleRetries(t *testing.T) {
	for _, oldDelete := range []bool{true, false} {
		t.Run(fmt.Sprintf("oldDelete=%t", oldDelete), func(t *testing.T) {
			h := newHarness(t, withNoInviter())
			fence := &mutableSourceFence{current: true}
			ctx := domain.WithSourceFence(context.Background(), fence)
			put := putJSON(t, "itx-poll.one", votePayload("one"))
			del := hardDelete("itx-poll.one")
			old, newer := put, del
			if oldDelete {
				old, newer = del, put
			}
			h.store.PutErr = domain.NewUnavailableError("state unavailable")
			require.Equal(t, Retry, h.syncer.Sync(ctx, old))
			h.store.PutErr = nil
			require.Equal(t, Ack, h.syncer.Sync(context.Background(), newer))
			fence.current = false
			require.Equal(t, Ack, h.syncer.Sync(ctx, old))
			require.Len(t, h.pub.publishedVotes, 2, "old retry cannot republish")
			want := domain.MappingTombstoned
			if oldDelete {
				want = domain.MappingSynced
			}
			require.Equal(t, want, voteState(h, "one"))
		})
	}
}

func TestResourceRecoveryBeyondFormerBudget(t *testing.T) {
	h := newHarness(t, withNoInviter())
	entry := putJSON(t, "itx-poll.one", votePayload("one"))
	h.store.PutErr = domain.NewUnavailableError("state unavailable")
	for range 4 {
		require.Equal(t, Retry, h.sync(entry))
	}
	h.store.PutErr = nil
	require.Equal(t, Ack, h.sync(entry))
	require.Equal(t, domain.MappingSynced, voteState(h, "one"))
}

func TestDecodeRecord(t *testing.T) {
	t.Run("decodes JSON", func(t *testing.T) {
		payload := map[string]any{"name": "Board Election"}
		raw, err := json.Marshal(payload)
		require.NoError(t, err)

		got, err := DecodeRecord(raw)
		require.NoError(t, err)
		assert.Equal(t, "Board Election", got["name"])
	})

	t.Run("decodes msgpack", func(t *testing.T) {
		payload := map[string]any{"name": "Board Election"}
		raw, err := msgpack.Marshal(payload)
		require.NoError(t, err)

		got, err := DecodeRecord(raw)
		require.NoError(t, err)
		assert.Equal(t, "Board Election", got["name"])
	})

	t.Run("returns combined error when both decoders fail", func(t *testing.T) {
		_, err := DecodeRecord([]byte("not-json-or-msgpack"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json:")
		assert.Contains(t, err.Error(), "msgpack:")
	})
}
