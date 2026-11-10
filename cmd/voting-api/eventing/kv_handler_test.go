// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/service/eventsync"
)

// fakeMsg is a jetstream.Msg that records how the handler settled it.
type fakeMsg struct {
	subject string
	data    []byte
	headers nats.Header

	metadata    *jetstream.MsgMetadata
	metadataErr error
	ackErr      error
	nakErr      error

	acked    int
	nakDelay []time.Duration
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.metadata, m.metadataErr }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return m.headers }
func (m *fakeMsg) Subject() string                           { return m.subject }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { m.acked++; return m.ackErr }
func (m *fakeMsg) DoubleAck(context.Context) error           { return nil }
func (m *fakeMsg) Nak() error                                { return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error {
	m.nakDelay = append(m.nakDelay, d)
	return m.nakErr
}
func (m *fakeMsg) InProgress() error           { return nil }
func (m *fakeMsg) Term() error                 { return nil }
func (m *fakeMsg) TermWithReason(string) error { return nil }

// absentMappings reports every resource as not yet synced, so a vote response always
// waits for its parent vote and the syncer asks for a retry.
type absentMappings struct{}

func (absentMappings) State(context.Context, domain.MappingKind, string) domain.MappingState {
	return domain.MappingAbsent
}
func (absentMappings) MarkSynced(context.Context, domain.MappingKind, string) error     { return nil }
func (absentMappings) MarkTombstoned(context.Context, domain.MappingKind, string) error { return nil }

type echoIDMapper struct{}

func (echoIDMapper) MapProjectV2ToV1(_ context.Context, id string) (string, error)   { return id, nil }
func (echoIDMapper) MapProjectV1ToV2(_ context.Context, id string) (string, error)   { return id, nil }
func (echoIDMapper) MapCommitteeV2ToV1(_ context.Context, id string) (string, error) { return id, nil }
func (echoIDMapper) MapCommitteeV1ToV2(_ context.Context, id string) (string, error) { return id, nil }

func newTestSyncer() *eventsync.Syncer {
	return eventsync.New(eventsync.Deps{
		IDMapper: echoIDMapper{},
		Mappings: absentMappings{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

const (
	// A vote response whose parent vote is not yet synced: the syncer returns Retry.
	retrySubject = "$KV.v1-objects.itx-poll-vote.vote-1"
	retryBody    = `{"vote_id":"vote-1","poll_id":"poll-1"}`

	// A record of a type the syncer does not handle: the syncer returns Ack.
	ackSubject = "$KV.v1-objects.itx-unknown.thing-1"
	ackBody    = `{"id":"thing-1"}`
)

func handle(t *testing.T, msg *fakeMsg) {
	t.Helper()
	kvMessageHandler(context.Background(), msg, newTestSyncer(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestKVMessageHandler_Ack(t *testing.T) {
	t.Run("acknowledges a finished entry and does not NAK", func(t *testing.T) {
		msg := &fakeMsg{subject: ackSubject, data: []byte(ackBody), headers: nats.Header{}}

		handle(t, msg)

		assert.Equal(t, 1, msg.acked)
		assert.Empty(t, msg.nakDelay)
	})

	t.Run("a failed ack is logged, not fatal", func(t *testing.T) {
		msg := &fakeMsg{subject: ackSubject, data: []byte(ackBody), headers: nats.Header{}, ackErr: errors.New("ack timeout")}

		assert.NotPanics(t, func() { handle(t, msg) })
		assert.Equal(t, 1, msg.acked)
	})

	t.Run("a subject shorter than the bucket prefix yields an empty key and is acknowledged", func(t *testing.T) {
		msg := &fakeMsg{subject: "$KV.v1-objects.", data: []byte(ackBody), headers: nats.Header{}}

		handle(t, msg)

		assert.Equal(t, 1, msg.acked)
		assert.Empty(t, msg.nakDelay)
	})
}

func TestKVMessageHandler_RetryBackoff(t *testing.T) {
	tests := []struct {
		name      string
		delivered uint64
		want      time.Duration
	}{
		{name: "first delivery retries after 2s", delivered: 1, want: 2 * time.Second},
		{name: "second delivery retries after 10s", delivered: 2, want: 10 * time.Second},
		{name: "third delivery retries after 20s", delivered: 3, want: 20 * time.Second},
		{name: "later deliveries stay at 20s", delivered: 9, want: 20 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &fakeMsg{
				subject: retrySubject, data: []byte(retryBody), headers: nats.Header{},
				metadata: &jetstream.MsgMetadata{NumDelivered: tt.delivered},
			}

			handle(t, msg)

			assert.Equal(t, []time.Duration{tt.want}, msg.nakDelay)
			assert.Zero(t, msg.acked, "a retried message must not also be acknowledged")
		})
	}

	t.Run("unreadable metadata falls back to the first-retry delay", func(t *testing.T) {
		msg := &fakeMsg{
			subject: retrySubject, data: []byte(retryBody), headers: nats.Header{},
			metadataErr: errors.New("not a JetStream message"),
		}

		handle(t, msg)

		assert.Equal(t, []time.Duration{2 * time.Second}, msg.nakDelay)
		assert.Zero(t, msg.acked)
	})

	t.Run("a failed NAK is logged, not fatal", func(t *testing.T) {
		msg := &fakeMsg{
			subject: retrySubject, data: []byte(retryBody), headers: nats.Header{},
			metadata: &jetstream.MsgMetadata{NumDelivered: 1}, nakErr: errors.New("nak failed"),
		}

		assert.NotPanics(t, func() { handle(t, msg) })
		assert.Len(t, msg.nakDelay, 1)
	})
}

func TestKVMessageHandler_Operation(t *testing.T) {
	// A key with the vote-response prefix but no UID separates the two operations: a put
	// is routed to the vote-response handler (parent not synced, so retry), while a delete
	// cannot extract a UID and is acknowledged.
	const noUIDSubject = "$KV.v1-objects.itx-poll-vote"

	t.Run("no KV-Operation header is handled as a put", func(t *testing.T) {
		msg := &fakeMsg{
			subject: noUIDSubject, data: []byte(retryBody), headers: nats.Header{},
			metadata: &jetstream.MsgMetadata{NumDelivered: 1},
		}

		handle(t, msg)

		assert.Len(t, msg.nakDelay, 1)
		assert.Zero(t, msg.acked)
	})

	t.Run("an explicit PUT is handled as a put", func(t *testing.T) {
		msg := &fakeMsg{
			subject: noUIDSubject, data: []byte(retryBody),
			headers:  nats.Header{"KV-Operation": []string{"PUT"}},
			metadata: &jetstream.MsgMetadata{NumDelivered: 1},
		}

		handle(t, msg)

		assert.Len(t, msg.nakDelay, 1)
	})

	for _, op := range []string{"DEL", "PURGE"} {
		t.Run(op+" is handled as a delete", func(t *testing.T) {
			msg := &fakeMsg{
				subject: noUIDSubject, data: []byte(retryBody),
				headers:  nats.Header{"KV-Operation": []string{op}},
				metadata: &jetstream.MsgMetadata{NumDelivered: 1},
			}

			handle(t, msg)

			assert.Equal(t, 1, msg.acked)
			assert.Empty(t, msg.nakDelay)
		})
	}
}
