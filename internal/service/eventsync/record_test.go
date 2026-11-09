// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventsync

import (
	"encoding/json"
	"testing"

	indexerConstants "github.com/linuxfoundation/lfx-v2-indexer-service/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

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

func TestShouldSendVoteResponseInvite(t *testing.T) {
	assert.True(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionUpdated, "", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "existing", "guest@example.com"))
	assert.False(t, shouldSendVoteResponseInvite(indexerConstants.ActionCreated, "", ""))
}
