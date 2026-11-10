// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package idmapper

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
)

// lookupResponder answers lfx.lookup_v1_mapping requests from a key -> reply table and
// records every key it was asked for. A key missing from the table gets an empty reply
// (what the v1-sync-helper sends for "not found"). A reply of "<hang>" never answers.
type lookupResponder struct {
	mu   sync.Mutex
	keys []string
}

func (r *lookupResponder) asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.keys...)
}

// newMapperWithResponder starts an embedded NATS server, a fake mapping service and a
// NATSMapper connected to it.
func newMapperWithResponder(t *testing.T, replies map[string]string) (*NATSMapper, *lookupResponder) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{Port: -1})
	require.NoError(t, err)
	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)

	svcConn, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(svcConn.Close)

	responder := &lookupResponder{}
	_, err = svcConn.Subscribe(lookupSubject, func(m *nats.Msg) {
		key := string(m.Data)
		responder.mu.Lock()
		responder.keys = append(responder.keys, key)
		responder.mu.Unlock()
		if replies[key] == "<hang>" {
			return
		}
		_ = m.Respond([]byte(replies[key]))
	})
	require.NoError(t, err)
	require.NoError(t, svcConn.Flush())

	mapper, err := NewNATSMapper(Config{URL: srv.ClientURL()})
	require.NoError(t, err)
	t.Cleanup(mapper.Close)
	return mapper, responder
}

func TestNewNATSMapper(t *testing.T) {
	t.Run("URL is required", func(t *testing.T) {
		m, err := NewNATSMapper(Config{})

		require.Error(t, err)
		assert.Nil(t, m)
		assert.Contains(t, err.Error(), "NATS URL is required")
	})

	t.Run("connection failure is returned", func(t *testing.T) {
		m, err := NewNATSMapper(Config{URL: "nats://127.0.0.1:1"})

		require.Error(t, err)
		assert.Nil(t, m)
		assert.Contains(t, err.Error(), "failed to connect to NATS")
	})

	t.Run("default timeout applies when none is configured", func(t *testing.T) {
		m, _ := newMapperWithResponder(t, nil)
		assert.Equal(t, defaultTimeout, m.timeout)
	})

	t.Run("configured timeout is kept", func(t *testing.T) {
		srv, err := server.NewServer(&server.Options{Port: -1})
		require.NoError(t, err)
		go srv.Start()
		require.True(t, srv.ReadyForConnections(5*time.Second))
		t.Cleanup(srv.Shutdown)

		m, err := NewNATSMapper(Config{URL: srv.ClientURL(), Timeout: 2 * time.Second})

		require.NoError(t, err)
		t.Cleanup(m.Close)
		assert.Equal(t, 2*time.Second, m.timeout)
	})
}

func TestNATSMapper_Close(t *testing.T) {
	t.Run("closes the connection", func(t *testing.T) {
		m, _ := newMapperWithResponder(t, nil)

		m.Close()

		assert.True(t, m.conn.IsClosed())
	})

	t.Run("nil connection is tolerated", func(t *testing.T) {
		assert.NotPanics(t, func() { (&NATSMapper{}).Close() })
	})
}

func TestNATSMapper_Mappings(t *testing.T) {
	ctx := context.Background()
	replies := map[string]string{
		"project.uid.00000000-0000-0000-0000-000000000001": "a0A000000000001",
		"project.sfid.a0A000000000001":                     "00000000-0000-0000-0000-000000000001",
		"committee.sfid.a0B000000000001":                   "00000000-0000-0000-0000-000000000002",
	}

	t.Run("MapProjectV2ToV1 uses the project.uid key", func(t *testing.T) {
		mapper, responder := newMapperWithResponder(t, replies)

		got, err := mapper.MapProjectV2ToV1(ctx, "00000000-0000-0000-0000-000000000001")

		require.NoError(t, err)
		assert.Equal(t, "a0A000000000001", got)
		assert.Equal(t, []string{"project.uid.00000000-0000-0000-0000-000000000001"}, responder.asked())
	})

	t.Run("MapProjectV1ToV2 uses the project.sfid key", func(t *testing.T) {
		mapper, responder := newMapperWithResponder(t, replies)

		got, err := mapper.MapProjectV1ToV2(ctx, "a0A000000000001")

		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-0000-0000-000000000001", got)
		assert.Equal(t, []string{"project.sfid.a0A000000000001"}, responder.asked())
	})

	t.Run("MapCommitteeV1ToV2 uses the committee.sfid key", func(t *testing.T) {
		mapper, responder := newMapperWithResponder(t, replies)

		got, err := mapper.MapCommitteeV1ToV2(ctx, "a0B000000000001")

		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-0000-0000-000000000002", got)
		assert.Equal(t, []string{"committee.sfid.a0B000000000001"}, responder.asked())
	})
}

func TestNATSMapper_MapCommitteeV2ToV1(t *testing.T) {
	ctx := context.Background()
	replies := map[string]string{
		"committee.uid.c-pair":      "a0A000000000001:a0B000000000001",
		"committee.uid.c-bare":      "a0B000000000002",
		"committee.uid.c-3-parts":   "a:b:c",
		"committee.uid.c-empty-sfd": "a0A000000000001:",
		"committee.uid.c-error":     "error: committee lookup failed",
	}
	mapper, _ := newMapperWithResponder(t, replies)

	tests := []struct {
		name     string
		uid      string
		want     string
		wantType domain.ErrorType
		wantErr  string
	}{
		{name: "project:committee reply keeps only the committee SFID", uid: "c-pair", want: "a0B000000000001"},
		{name: "reply without a colon is returned as-is", uid: "c-bare", want: "a0B000000000002"},
		{name: "more than two parts is rejected", uid: "c-3-parts", wantType: domain.ErrorTypeUnavailable, wantErr: "unexpected committee mapping format"},
		{name: "empty committee SFID is rejected", uid: "c-empty-sfd", wantType: domain.ErrorTypeUnavailable, wantErr: "committee SFID is empty"},
		{name: "service error reply is unavailable", uid: "c-error", wantType: domain.ErrorTypeUnavailable, wantErr: "committee lookup failed"},
		{name: "unknown UID is a validation error", uid: "c-unknown", wantType: domain.ErrorTypeValidation, wantErr: "mapping not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapper.MapCommitteeV2ToV1(ctx, tt.uid)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.Equal(t, tt.wantType, domain.GetErrorType(err))
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNATSMapper_RequiresIDs(t *testing.T) {
	ctx := context.Background()
	mapper, responder := newMapperWithResponder(t, nil)

	cases := map[string]func(string) (string, error){
		"v2 project UID":    func(id string) (string, error) { return mapper.MapProjectV2ToV1(ctx, id) },
		"v1 project SFID":   func(id string) (string, error) { return mapper.MapProjectV1ToV2(ctx, id) },
		"v2 committee UID":  func(id string) (string, error) { return mapper.MapCommitteeV2ToV1(ctx, id) },
		"v1 committee SFID": func(id string) (string, error) { return mapper.MapCommitteeV1ToV2(ctx, id) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := call("")

			require.Error(t, err)
			assert.Empty(t, got)
			assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
			assert.Contains(t, err.Error(), name+" is required")
		})
	}

	t.Run("no lookup is sent for an empty ID", func(t *testing.T) {
		assert.Empty(t, responder.asked())
	})
}

func TestNATSMapper_LookupFailures(t *testing.T) {
	t.Run("not found (empty reply) is a validation error that names the key", func(t *testing.T) {
		mapper, _ := newMapperWithResponder(t, nil)

		got, err := mapper.MapProjectV2ToV1(context.Background(), "missing")

		require.Error(t, err)
		assert.Empty(t, got)
		assert.Equal(t, domain.ErrorTypeValidation, domain.GetErrorType(err))
		assert.Contains(t, err.Error(), "project.uid.missing")
	})

	t.Run("error-prefixed reply is unavailable with the service message", func(t *testing.T) {
		mapper, _ := newMapperWithResponder(t, map[string]string{"project.sfid.boom": "error: database offline"})

		_, err := mapper.MapProjectV1ToV2(context.Background(), "boom")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
		assert.Contains(t, err.Error(), "database offline")
	})

	t.Run("deadline exceeded is unavailable and says it timed out", func(t *testing.T) {
		mapper, _ := newMapperWithResponder(t, map[string]string{"project.sfid.slow": "<hang>"})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err := mapper.MapProjectV1ToV2(ctx, "slow")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
		assert.Contains(t, err.Error(), "timed out")
	})

	t.Run("no responder subscribed is unavailable", func(t *testing.T) {
		srv, err := server.NewServer(&server.Options{Port: -1})
		require.NoError(t, err)
		go srv.Start()
		require.True(t, srv.ReadyForConnections(5*time.Second))
		t.Cleanup(srv.Shutdown)
		mapper, err := NewNATSMapper(Config{URL: srv.ClientURL()})
		require.NoError(t, err)
		t.Cleanup(mapper.Close)

		_, err = mapper.MapCommitteeV1ToV2(context.Background(), "a0B000000000001")

		require.Error(t, err)
		assert.Equal(t, domain.ErrorTypeUnavailable, domain.GetErrorType(err))
		assert.Contains(t, err.Error(), "failed to lookup ID mapping")
	})
}
