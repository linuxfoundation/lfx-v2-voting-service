// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
		name    string
		setup   func(kv *mockKeyValue)
		nilKV   bool
		want    bool
		wantErr bool
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
		{name: "undecodable record fails closed", setup: pollEntry("\xc1not-json"), want: true},
		{name: "nil bucket fails closed", nilKV: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			var err error
			if tt.nilKV {
				got, err = pollIsPseudoAnonymous(context.Background(), nil, pollID)
			} else {
				kv := &mockKeyValue{}
				tt.setup(kv)
				got, err = pollIsPseudoAnonymous(context.Background(), kv, pollID)
				kv.AssertExpectations(t)
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
