// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventstate

import (
	"context"
	"errors"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
	"strings"
	"time"
)

// MigrationFailure carries only a fixed category, never sensitive driver data.
type MigrationFailure struct{ Category string }

// Error implements error without exposing source values or connection details.
func (e MigrationFailure) Error() string { return e.Category }

// MigrationFailureCategory returns an operator-safe failure category.
func MigrationFailureCategory(err error) string {
	var failure MigrationFailure
	if errors.As(err, &failure) {
		return failure.Category
	}
	return "infrastructure_failure"
}

// Import performs explicit offline migration with all legacy writers fenced.
func (s *Store) Import(ctx context.Context, js jetstream.JetStream, apply bool) (int, error) {
	if apply {
		if err := s.ApplySchema(ctx); err != nil {
			return 0, err
		}
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			return 0, err
		}
		defer conn.Release()
		opCtx, cancel := context.WithTimeout(ctx, opTimeout)
		var locked bool
		err = conn.QueryRow(opCtx, "SELECT pg_try_advisory_lock(1951002)").Scan(&locked)
		cancel()
		if err != nil {
			return 0, err
		}
		if !locked {
			return 0, MigrationFailure{"import_busy"}
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), opTimeout)
			defer cancel()
			_, _ = conn.Exec(cleanup, "SELECT pg_advisory_unlock(1951002)")
		}()
		opCtx, cancel = context.WithTimeout(ctx, opTimeout)
		var ready bool
		err = conn.QueryRow(opCtx, "SELECT EXISTS(SELECT 1 FROM voting_state_cutover)").Scan(&ready)
		cancel()
		if err != nil {
			return 0, err
		}
		if ready {
			return 0, MigrationFailure{"already_verified"}
		}
	}
	stream, err := js.Stream(ctx, "KV_v1-mappings")
	if err != nil {
		return 0, err
	}
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	before, err := stream.Info(opCtx)
	cancel()
	if err != nil {
		return 0, err
	}
	latest := map[string][]byte{}
	for _, kind := range []string{"vote", "vote_response", "vote_result", "v1_vote_response_lfid_invite_sent"} {
		for seq := uint64(1); seq <= before.State.LastSeq; {
			opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			msg, err := ReadSourceMessage(opCtx, js, "KV_v1-mappings", seq, "$KV.v1-mappings."+kind+".>", "")
			cancel()
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				break
			}
			if err != nil {
				return 0, err
			}
			if msg.Sequence > before.State.LastSeq {
				break
			}
			seq = msg.Sequence + 1
			key := strings.TrimPrefix(msg.Subject, "$KV.v1-mappings.")
			if msg.Header.Get("KV-Operation") == "DEL" || msg.Header.Get("KV-Operation") == "PURGE" {
				delete(latest, key)
			} else {
				latest[key] = append([]byte{}, msg.Data...)
			}
		}
	}
	if len(latest) == 0 {
		return 0, MigrationFailure{"empty_source"}
	}
	counts := map[string]int{}
	for key := range latest {
		b, err := bind(key)
		if err != nil {
			return 0, MigrationFailure{"invalid_source_state"}
		}
		counts[b.table]++
	}
	if apply {
		for key, value := range latest {
			if _, err := s.write(ctx, key, value, "create", 0); err != nil && !errors.Is(err, domain.ErrEventStateExists) {
				return 0, err
			}
			e, err := s.Get(ctx, key)
			if err != nil {
				return 0, err
			}
			b, _ := bind(key)
			ws, wv := b.decode(value)
			gs, gv := b.decode(e.Value())
			if ws != gs || wv != gv {
				return 0, MigrationFailure{"target_conflict"}
			}
		}
		for _, table := range []string{"voting_processed_resources", "voting_invitation_guards", "voting_invitation_intents"} {
			opCtx, cancel := context.WithTimeout(ctx, opTimeout)
			var n int
			err := s.pool.QueryRow(opCtx, "SELECT count(*) FROM "+table).Scan(&n)
			cancel()
			if err != nil {
				return 0, err
			}
			if n != counts[table] {
				return 0, MigrationFailure{"target_extra_rows"}
			}
		}
	}
	opCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
	after, err := stream.Info(opCtx)
	cancel()
	if err != nil {
		return 0, err
	}
	if before.State.FirstSeq != after.State.FirstSeq || before.State.LastSeq != after.State.LastSeq || before.State.Msgs != after.State.Msgs {
		return 0, MigrationFailure{"source_changed"}
	}
	if apply {
		opCtx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		_, err := s.pool.Exec(opCtx, "INSERT INTO voting_state_cutover(singleton,source_last_sequence) VALUES(true,$1)", after.State.LastSeq)
		if err != nil {
			return 0, err
		}
	}
	return len(latest), nil
}
