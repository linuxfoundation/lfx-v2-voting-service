// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPollIsPseudoAnonymous(t *testing.T) {
	const pollID = "poll-1"
	const pollKey = "itx-poll." + pollID

	pollEntry := func(value string) func(kv *mockKeyValue) {
		return func(kv *mockKeyValue) {
			kv.On("Get", mock.Anything, pollKey).
				Return(mockKeyValueEntry{key: pollKey, value: []byte(value)}, nil)
		}
	}

	tests := []struct {
		name     string
		setup    func(kv *mockKeyValue)
		nilKV    bool
		want     bool
		wantErr  bool
		wantWarn bool
	}{
		{name: "pseudo-anonymous poll", setup: pollEntry(`{"poll_id":"poll-1","pseudo_anonymity":true}`), want: true},
		{name: "named poll", setup: pollEntry(`{"poll_id":"poll-1","pseudo_anonymity":false}`), want: false},
		{name: "missing flag fails closed", setup: pollEntry(`{"poll_id":"poll-1"}`), want: true},
		{
			name: "record not found fails closed",
			setup: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).Return(nil, jetstream.ErrKeyNotFound)
			},
			want: true,
		},
		{
			name: "transient read error is returned",
			setup: func(kv *mockKeyValue) {
				kv.On("Get", mock.Anything, pollKey).Return(nil, errors.New("nats timeout"))
			},
			wantErr: true,
		},
		{name: "undecodable record fails closed", setup: pollEntry("\xc1private-record-body"), want: true, wantWarn: true},
		{name: "nil bucket fails closed", nilKV: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			var err error
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("vote_response_id", "response-1")
			if tt.nilKV {
				got, err = pollIsPseudoAnonymous(context.Background(), nil, pollID, logger)
			} else {
				kv := &mockKeyValue{}
				tt.setup(kv)
				got, err = pollIsPseudoAnonymous(context.Background(), kv, pollID, logger)
				kv.AssertExpectations(t)
			}

			if tt.wantWarn {
				var record map[string]any
				require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
				assert.Equal(t, "WARN", record["level"])
				assert.Equal(t, "undecodable parent poll record, treating as pseudo-anonymous", record["msg"])
				assert.Equal(t, pollKey, record["poll_key"])
				assert.Equal(t, "response-1", record["vote_response_id"])
				assert.NotEmpty(t, record[errKey])
				assert.NotContains(t, logs.String(), "private-record-body")
			} else {
				assert.Empty(t, logs.String())
			}

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
