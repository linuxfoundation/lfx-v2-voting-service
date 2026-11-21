// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventing"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestDeliveryFence(t *testing.T) {
	for _, tt := range []struct {
		name      string
		latest    uint64
		err       error
		current   bool
		wantError bool
	}{
		{"current", 3, nil, true, false}, {"superseded", 4, nil, false, false}, {"lagging", 2, nil, false, true}, {"unavailable", 0, errors.New("private endpoint"), false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &deliveryFence{revision: 3, latest: func(context.Context) (uint64, error) { return tt.latest, tt.err }}
			current, err := f.Current(context.Background())
			require.Equal(t, tt.current, current)
			require.Equal(t, tt.wantError, err != nil)
			if err != nil {
				require.NotContains(t, err.Error(), "private endpoint")
			}
		})
	}
}

func TestProcessMessage_SupersededSource(t *testing.T) {
	_, state := newPostgresState(t)
	ns, url := startTestNATSServer(t)
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	objects, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: V1ObjectsBucket})
	require.NoError(t, err)
	_, err = objects.Put(context.Background(), "itx-poll.one", []byte(`{}`))
	require.NoError(t, err)
	_, err = objects.Put(context.Background(), "itx-poll.one", []byte(`{}`))
	require.NoError(t, err)
	ep := &EventProcessor{state: state, jsInstance: js, config: eventing.Config{NATSURL: url, StreamName: "KV_v1-objects"}, logger: slog.Default()}
	msg := &fakeMsg{subject: "$KV.v1-objects.itx-poll.one", headers: nats.Header{"KV-Operation": []string{"DEL"}}, metadata: &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}, NumDelivered: 7}}
	ep.processMessage(context.Background(), msg)
	require.Equal(t, 1, msg.acked)
	require.Empty(t, msg.nakDelay)
}
