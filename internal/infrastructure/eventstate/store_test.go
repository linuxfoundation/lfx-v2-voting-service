// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventstate

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("VOTING_STATE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VOTING_STATE_TEST_DATABASE_URL to a disposable local Postgres database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("voting_state_test_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+name)
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	s, err := Open(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		s.Close()
		_, err := admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	require.NoError(t, s.ApplySchema(ctx))
	return s
}
func TestStore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	require.Error(t, s.Ready(ctx))
	require.NoError(t, s.ApplySchema(ctx))
	for _, kind := range []domain.MappingKind{domain.MappingKindVote, domain.MappingKindVoteResponse, domain.MappingKindVoteResult} {
		t.Run(string(kind), func(t *testing.T) {
			require.Equal(t, domain.MappingAbsent, s.State(ctx, kind, "test"))
			require.NoError(t, s.MarkSynced(ctx, kind, "test"))
			require.Equal(t, domain.MappingSynced, s.State(ctx, kind, "test"))
			require.NoError(t, s.MarkTombstoned(ctx, kind, "test"))
			require.Equal(t, domain.MappingTombstoned, s.State(ctx, kind, "test"))
			require.NoError(t, s.MarkSynced(ctx, kind, "test"))
			require.Equal(t, domain.MappingSynced, s.State(ctx, kind, "test"))
		})
	}
	t.Run("guard CAS and delete/recreate safety", func(t *testing.T) {
		g, err := s.LookupInvitation(ctx, "response")
		require.NoError(t, err)
		require.False(t, g.Exists)
		rev, err := s.ClaimInvitation(ctx, "response")
		require.NoError(t, err)
		_, err = s.ClaimInvitation(ctx, "response")
		require.ErrorIs(t, err, domain.ErrEventStateExists)
		require.ErrorIs(t, s.CompleteInvitation(ctx, "response", rev+1, "invite"), domain.ErrEventStateRevisionMismatch)
		require.NoError(t, s.CompleteInvitation(ctx, "response", rev, "invite"))
		g, err = s.LookupInvitation(ctx, "response")
		require.NoError(t, err)
		require.False(t, g.Pending)
		require.Equal(t, "invite", g.InviteID)
		require.ErrorIs(t, s.CompleteInvitation(ctx, "response", rev, "other"), domain.ErrEventStateRevisionMismatch)
		_, err = s.pool.Exec(ctx, "DELETE FROM voting_invitation_guards WHERE response_id='response'")
		require.NoError(t, err)
		fresh, err := s.ClaimInvitation(ctx, "response")
		require.NoError(t, err)
		require.Greater(t, fresh, g.Revision)
		require.ErrorIs(t, s.CompleteInvitation(ctx, "response", g.Revision, "stale"), domain.ErrEventStateRevisionMismatch)
	})
	t.Run("one concurrent dispatcher", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 20)
		for range 20 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := s.ClaimInvitation(ctx, "concurrent"); results <- err }()
		}
		wg.Wait()
		close(results)
		wins := 0
		for err := range results {
			if err == nil {
				wins++
			} else {
				require.ErrorIs(t, err, domain.ErrEventStateExists)
			}
		}
		require.Equal(t, 1, wins)
	})
	t.Run("intent is explicit", func(t *testing.T) {
		prepared, err := s.InvitationPrepared(ctx, "new-response")
		require.NoError(t, err)
		require.False(t, prepared)
		require.NoError(t, s.PrepareInvitation(ctx, "new-response"))
		prepared, err = s.InvitationPrepared(ctx, "new-response")
		require.NoError(t, err)
		require.True(t, prepared)
		require.NoError(t, s.ResolveInvitation(ctx, "new-response"))
		require.NoError(t, s.PrepareInvitation(ctx, "new-response"))
		prepared, err = s.InvitationPrepared(ctx, "new-response")
		require.NoError(t, err)
		require.False(t, prepared)
	})
	t.Run("resource serialization and locked pool capacity", func(t *testing.T) {
		locked, release, err := s.LockResource(ctx, "one")
		require.NoError(t, err)
		_, _, err = s.LockResource(ctx, "one")
		require.ErrorIs(t, err, domain.ErrResourceBusy)
		require.NoError(t, s.MarkSynced(locked, domain.MappingKindVote, "locked"))
		release()
		releases := []func(){}
		defer func() {
			for _, release := range releases {
				release()
			}
		}()
		contexts := []context.Context{}
		for i := range 10 {
			locked, release, err := s.LockResource(ctx, fmt.Sprintf("pool.%d", i))
			require.NoError(t, err)
			releases = append(releases, release)
			contexts = append(contexts, locked)
		}
		for i, locked := range contexts {
			require.NoError(t, s.MarkSynced(locked, domain.MappingKindVote, fmt.Sprintf("pool-%d", i)))
		}
	})
	t.Run("superseded source cannot write state", func(t *testing.T) {
		fenced := domain.WithSourceFence(ctx, staleSourceFence{})
		require.ErrorIs(t, s.MarkSynced(fenced, domain.MappingKindVote, "stale"), domain.ErrSourceSuperseded)
		require.ErrorIs(t, s.MarkTombstoned(fenced, domain.MappingKindVote, "stale"), domain.ErrSourceSuperseded)
		require.ErrorIs(t, s.PrepareInvitation(fenced, "stale"), domain.ErrSourceSuperseded)
		require.Equal(t, domain.MappingAbsent, s.State(ctx, domain.MappingKindVote, "stale"))
	})
	t.Run("unavailable is not absence", func(t *testing.T) {
		s.Close()
		require.Equal(t, domain.MappingUnavailable, s.State(ctx, domain.MappingKindVote, "test"))
	})
}

type staleSourceFence struct{}

func (staleSourceFence) Current(context.Context) (bool, error) { return false, nil }
