// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventstate

import (
	"context"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestImport(t *testing.T) {
	for _, mode := range []string{"stable", "conflict", "source_changed", "extra_processed", "extra_guard", "extra_intent", "empty_source", "invalid_source_state"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
			require.NoError(t, err)
			go srv.Start()
			require.True(t, srv.ReadyForConnections(5*time.Second))
			t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
			nc, err := nats.Connect(srv.ClientURL())
			require.NoError(t, err)
			t.Cleanup(nc.Close)
			js, err := jetstream.New(nc)
			require.NoError(t, err)
			kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "v1-mappings", History: 10})
			require.NoError(t, err)
			if mode == "empty_source" || mode == "invalid_source_state" {
				if mode == "invalid_source_state" {
					_, err = kv.Put(ctx, "vote.invalid/key", []byte("1"))
					require.NoError(t, err)
				}
				_, err = s.Import(ctx, js, true)
				require.Equal(t, mode, MigrationFailureCategory(err))
				require.Error(t, s.Ready(ctx))
				return
			}
			want := map[string]string{"vote.test": "1", "vote_response.test": "1", "vote_result.test": "!del", "v1_vote_response_lfid_invite_sent.test": "pending"}
			for key, value := range want {
				_, err = kv.Put(ctx, "project.sfid.unrelated", []byte("project"))
				require.NoError(t, err)
				_, err = kv.Put(ctx, key, []byte("old"))
				require.NoError(t, err)
				_, err = kv.Put(ctx, key, []byte(value))
				require.NoError(t, err)
			}
			_, err = kv.Put(ctx, "vote.deleted", []byte("1"))
			require.NoError(t, err)
			require.NoError(t, kv.Delete(ctx, "vote.deleted"))
			_, err = kv.Put(ctx, "vote.purged", []byte("1"))
			require.NoError(t, err)
			require.NoError(t, kv.Purge(ctx, "vote.purged"))
			_, err = kv.Put(ctx, "vote.restored", []byte("old"))
			require.NoError(t, err)
			require.NoError(t, kv.Delete(ctx, "vote.restored"))
			_, err = kv.Put(ctx, "vote.restored", []byte("1"))
			require.NoError(t, err)
			if mode == "conflict" {
				_, err = s.write(ctx, "vote.test", []byte("!del"), "put", 0)
				require.NoError(t, err)
			}
			switch mode {
			case "extra_processed":
				require.NoError(t, s.MarkSynced(ctx, domain.MappingKindVote, "extra"))
			case "extra_guard":
				_, err = s.ClaimInvitation(ctx, "extra")
				require.NoError(t, err)
			case "extra_intent":
				require.NoError(t, s.PrepareInvitation(ctx, "extra"))
			case "source_changed":
				js = &changingJetStream{JetStream: js, mutate: func() { _, err := kv.Put(ctx, "vote.test", []byte("!del")); require.NoError(t, err) }}
			}
			n, err := s.Import(ctx, js, true)
			if mode == "conflict" {
				require.Error(t, err)
				require.Equal(t, "target_conflict", MigrationFailureCategory(err))
				require.Error(t, s.Ready(ctx))
				return
			}
			if mode == "source_changed" || strings.HasPrefix(mode, "extra_") {
				category := mode
				if strings.HasPrefix(mode, "extra_") {
					category = "target_extra_rows"
				}
				require.Equal(t, category, MigrationFailureCategory(err))
				require.Error(t, s.Ready(ctx))
				return
			}
			require.NoError(t, err)
			require.Equal(t, len(want)+1, n)
			for key, value := range want {
				e, err := s.Get(ctx, key)
				require.NoError(t, err)
				require.Equal(t, value, string(e.Value()))
			}
			for _, key := range []string{"vote.deleted", "vote.purged"} {
				_, err := s.Get(ctx, key)
				require.ErrorIs(t, err, jetstream.ErrKeyNotFound)
			}
			require.NoError(t, s.Ready(ctx))
			_, err = s.Import(ctx, js, true)
			require.Equal(t, "already_verified", MigrationFailureCategory(err))
		})
	}
}

// changingStream mutates synchronously before the final source fingerprint;
// production has no test hook and tests do not depend on scheduler timing.
type changingStream struct {
	jetstream.Stream
	calls  int
	mutate func()
}

func (s *changingStream) Info(ctx context.Context, opts ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	s.calls++
	if s.calls == 2 {
		s.mutate()
	}
	return s.Stream.Info(ctx, opts...)
}

type changingJetStream struct {
	jetstream.JetStream
	mutate func()
}

func (j *changingJetStream) Stream(ctx context.Context, name string) (jetstream.Stream, error) {
	s, err := j.JetStream.Stream(ctx, name)
	if err != nil {
		return nil, err
	}
	return &changingStream{Stream: s, mutate: j.mutate}, nil
}
