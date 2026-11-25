// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	inviteapi "github.com/linuxfoundation/lfx-v2-invite-service/pkg/api"
	"github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAcceptanceClient records AcceptInvite calls.
type fakeAcceptanceClient struct {
	mu    sync.Mutex
	calls [][2]string // {email, username}
	err   error
	done  chan struct{}
}

func newFakeAcceptanceClient() *fakeAcceptanceClient {
	return &fakeAcceptanceClient{done: make(chan struct{}, 8)}
}

func (f *fakeAcceptanceClient) AcceptInvite(_ context.Context, email, username string) error {
	f.mu.Lock()
	f.calls = append(f.calls, [2]string{email, username})
	f.mu.Unlock()
	f.done <- struct{}{}
	return f.err
}

func (f *fakeAcceptanceClient) recorded() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.calls...)
}

func acceptedEvent(email, username string) inviteapi.InviteServiceAcceptedEvent {
	var evt inviteapi.InviteServiceAcceptedEvent
	evt.Recipient.Email = email
	evt.AcceptedBy = username
	evt.Resource.Type = "vote"
	return evt
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestProcessInviteAcceptedEvent(t *testing.T) {
	t.Run("calls ITX with the recipient email and the accepting username", func(t *testing.T) {
		client := newFakeAcceptanceClient()

		err := processInviteAcceptedEvent(context.Background(), acceptedEvent("alice@example.com", "alice"), client, discardLogger())

		require.NoError(t, err)
		assert.Equal(t, [][2]string{{"alice@example.com", "alice"}}, client.recorded())
	})

	for name, evt := range map[string]inviteapi.InviteServiceAcceptedEvent{
		"missing email":    acceptedEvent("", "alice"),
		"missing username": acceptedEvent("alice@example.com", ""),
		"both missing":     acceptedEvent("", ""),
	} {
		t.Run(name+" is discarded without calling ITX", func(t *testing.T) {
			client := newFakeAcceptanceClient()

			err := processInviteAcceptedEvent(context.Background(), evt, client, discardLogger())

			require.NoError(t, err)
			assert.Empty(t, client.recorded())
		})
	}

	t.Run("an ITX failure is returned", func(t *testing.T) {
		client := newFakeAcceptanceClient()
		client.err = errors.New("itx unavailable")

		err := processInviteAcceptedEvent(context.Background(), acceptedEvent("alice@example.com", "alice"), client, discardLogger())

		require.Error(t, err)
		assert.ErrorIs(t, err, client.err)
	})
}

// startSubscriber runs an embedded NATS server with a started InviteAcceptedSubscriber
// and returns a connection to publish events on.
func startSubscriber(t *testing.T, client *fakeAcceptanceClient) (*InviteAcceptedSubscriber, *natsgo.Conn) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Port: -1})
	require.NoError(t, err)
	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)

	conn, err := natsgo.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)

	sub := NewInviteAcceptedSubscriber(conn, client, discardLogger())
	require.NoError(t, sub.Start(context.Background()))
	require.NoError(t, conn.Flush())
	return sub, conn
}

func waitForCall(t *testing.T, client *fakeAcceptanceClient) {
	t.Helper()
	select {
	case <-client.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for AcceptInvite")
	}
}

func TestInviteAcceptedSubscriber(t *testing.T) {
	t.Run("a published acceptance event reaches ITX", func(t *testing.T) {
		client := newFakeAcceptanceClient()
		sub, conn := startSubscriber(t, client)
		defer sub.Stop()

		payload, err := json.Marshal(acceptedEvent("alice@example.com", "alice"))
		require.NoError(t, err)
		require.NoError(t, conn.Publish(inviteapi.InviteServiceAcceptedSubject, payload))

		waitForCall(t, client)
		assert.Equal(t, [][2]string{{"alice@example.com", "alice"}}, client.recorded())
	})

	t.Run("malformed events are discarded and do not stop the subscriber", func(t *testing.T) {
		client := newFakeAcceptanceClient()
		sub, conn := startSubscriber(t, client)
		defer sub.Stop()

		require.NoError(t, conn.Publish(inviteapi.InviteServiceAcceptedSubject, []byte("not json")))
		good, err := json.Marshal(acceptedEvent("bob@example.com", "bob"))
		require.NoError(t, err)
		require.NoError(t, conn.Publish(inviteapi.InviteServiceAcceptedSubject, good))

		waitForCall(t, client)
		assert.Equal(t, [][2]string{{"bob@example.com", "bob"}}, client.recorded())
	})

	t.Run("an ITX failure is not retried and the next event is still processed", func(t *testing.T) {
		client := newFakeAcceptanceClient()
		client.err = errors.New("itx unavailable")
		sub, conn := startSubscriber(t, client)
		defer sub.Stop()

		for _, who := range []string{"alice", "bob"} {
			payload, err := json.Marshal(acceptedEvent(who+"@example.com", who))
			require.NoError(t, err)
			require.NoError(t, conn.Publish(inviteapi.InviteServiceAcceptedSubject, payload))
		}

		waitForCall(t, client)
		waitForCall(t, client)
		assert.Len(t, client.recorded(), 2, "each event is attempted once")
	})

	t.Run("Stop ends delivery", func(t *testing.T) {
		client := newFakeAcceptanceClient()
		sub, conn := startSubscriber(t, client)

		sub.Stop()
		require.NoError(t, conn.Flush())
		payload, err := json.Marshal(acceptedEvent("alice@example.com", "alice"))
		require.NoError(t, err)
		require.NoError(t, conn.Publish(inviteapi.InviteServiceAcceptedSubject, payload))
		require.NoError(t, conn.Flush())

		select {
		case <-client.done:
			t.Fatal("event delivered after Stop")
		case <-time.After(300 * time.Millisecond):
		}
	})

	t.Run("Start on a closed connection fails and cancels its context", func(t *testing.T) {
		srv, err := server.NewServer(&server.Options{Port: -1})
		require.NoError(t, err)
		go srv.Start()
		require.True(t, srv.ReadyForConnections(5*time.Second))
		t.Cleanup(srv.Shutdown)
		conn, err := natsgo.Connect(srv.ClientURL())
		require.NoError(t, err)
		conn.Close()
		sub := NewInviteAcceptedSubscriber(conn, newFakeAcceptanceClient(), discardLogger())

		err = sub.Start(context.Background())

		require.Error(t, err)
		assert.Error(t, sub.ctx.Err(), "context must be cancelled when the subscription fails")
	})

	t.Run("Stop before Start is a no-op", func(t *testing.T) {
		sub := NewInviteAcceptedSubscriber(nil, newFakeAcceptanceClient(), discardLogger())

		assert.NotPanics(t, sub.Stop)
	})
}
