// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/infrastructure/eventstate"
	"github.com/stretchr/testify/require"
)

// newPostgresState creates only a disposable, uniquely named test schema.
func newPostgresState(t *testing.T) (string, *eventstate.Store) {
	t.Helper()
	dsn := os.Getenv("VOTING_STATE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VOTING_STATE_TEST_DATABASE_URL for local PG integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	name := fmt.Sprintf("voting_processor_test_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+name)
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	state, err := eventstate.Open(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		state.Close()
		_, err := admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE")
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	require.NoError(t, state.ApplySchema(ctx))
	return u.String(), state
}
