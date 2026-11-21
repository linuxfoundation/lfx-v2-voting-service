// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package eventstate implements service-owned Postgres processing state. The
// domain interfaces remain independent of both Postgres and NATS.
package eventstate

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
	"strings"
	"time"
)

//go:embed schema.sql
var schema string

const opTimeout = 5 * time.Second

// Store owns typed processed state, invitation intents, and dispatch guards.
type Store struct{ pool *pgxpool.Pool }

type resourceConnectionKey struct{}
type stateQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Locked DML reuses its session so a fully locked pool cannot starve itself.
func (s *Store) db(ctx context.Context) stateQuerier {
	if conn, ok := ctx.Value(resourceConnectionKey{}).(*pgxpool.Conn); ok {
		return conn
	}
	return s.pool
}

var _ domain.MappingStore = (*Store)(nil)
var _ domain.InvitationGuards = (*Store)(nil)

// Open connects to the service-owned database; it never falls back to KV.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, domain.NewUnavailableError("VOTING_STATE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, domain.NewUnavailableError("invalid voting state database configuration")
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.ConnectTimeout = opTimeout
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, unavailable()
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, unavailable()
	}
	return &Store{pool: pool}, nil
}
func unavailable() error { return domain.NewUnavailableError("voting event state unavailable") }

// Close releases the pool after consumers have stopped.
func (s *Store) Close() { s.pool.Close() }

// Ready fails closed until an offline source-to-target import is verified.
func (s *Store) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var ready bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM voting_state_cutover)").Scan(&ready)
	if err != nil || !ready {
		return domain.NewUnavailableError("voting state cutover not verified")
	}
	return nil
}

// ApplySchema applies the owned schema under a bounded transaction and lock.
func (s *Store) ApplySchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1951001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type binding struct {
	table, payload string
	keys           []string
	args           []any
}

func bind(key string) (binding, error) {
	kind, id, ok := strings.Cut(key, ".")
	if !ok || id == "" || strings.ContainsAny(key, " \t\r\n*>/\\") {
		return binding{}, domain.NewValidationError("invalid voting state key")
	}
	switch kind {
	case "vote", "vote_response", "vote_result":
		return binding{"voting_processed_resources", "legacy_uid", []string{"resource_kind", "source_id"}, []any{kind, id}}, nil
	case "v1_vote_response_lfid_invite_sent":
		return binding{"voting_invitation_guards", "invite_id", []string{"response_id"}, []any{id}}, nil
	default:
		return binding{}, domain.NewValidationError("unowned voting state family")
	}
}
func (b binding) decode(value []byte) (string, string) {
	switch string(value) {
	case "":
		return "empty", ""
	case "!del":
		return "deleted", ""
	}
	if b.table == "voting_invitation_guards" {
		if string(value) == "pending" {
			return "pending", ""
		}
		return "sent", string(value)
	}
	if string(value) == "1" {
		return "processed", ""
	}
	return "legacy", string(value)
}
func where(b binding) string {
	parts := make([]string, len(b.keys))
	for i, col := range b.keys {
		parts[i] = fmt.Sprintf("%s=$%d", col, i+1)
	}
	return strings.Join(parts, " AND ")
}

type entry struct {
	key     string
	value   []byte
	version uint64
	updated time.Time
}

func (e entry) Bucket() string                  { return "voting-event-state" }
func (e entry) Key() string                     { return e.key }
func (e entry) Value() []byte                   { return e.value }
func (e entry) Revision() uint64                { return e.version }
func (e entry) Created() time.Time              { return e.updated }
func (e entry) Delta() uint64                   { return 0 }
func (e entry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

// Get is a scoped compatibility reader for reindex tooling and offline imports.
// It reconstructs legacy encoding from owned columns, without accessing KV.
func (s *Store) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	b, err := bind(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	e := entry{key: key}
	var state, payload string
	err = s.db(ctx).QueryRow(ctx, "SELECT state,"+b.payload+",version,updated_at FROM "+b.table+" WHERE "+where(b), b.args...).Scan(&state, &payload, &e.version, &e.updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, jetstream.ErrKeyNotFound
	}
	if err != nil {
		return nil, unavailable()
	}
	switch state {
	case "processed":
		e.value = []byte("1")
	case "pending":
		e.value = []byte("pending")
	case "deleted":
		e.value = []byte("!del")
	default:
		e.value = []byte(payload)
	}
	return e, nil
}

// State reports confirmed presence/absence/deletion, or MappingUnavailable.
func (s *Store) State(ctx context.Context, kind domain.MappingKind, id string) domain.MappingState {
	e, err := s.Get(ctx, string(kind)+"."+id)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.MappingAbsent
	}
	if err != nil {
		return domain.MappingUnavailable
	}
	if string(e.Value()) == "!del" {
		return domain.MappingTombstoned
	}
	return domain.MappingSynced
}

// MarkSynced retains the processed marker for the existing source ID.
func (s *Store) MarkSynced(ctx context.Context, kind domain.MappingKind, id string) error {
	_, err := s.write(ctx, string(kind)+"."+id, []byte("1"), "put", 0)
	return err
}

// MarkTombstoned retains deletion state to suppress redeliveries.
func (s *Store) MarkTombstoned(ctx context.Context, kind domain.MappingKind, id string) error {
	_, err := s.write(ctx, string(kind)+"."+id, []byte("!del"), "put", 0)
	return err
}
func (s *Store) write(ctx context.Context, key string, value []byte, op string, rev uint64) (uint64, error) {
	if err := domain.CheckSourceFence(ctx); err != nil {
		return 0, err
	}
	b, err := bind(key)
	if err != nil {
		return 0, err
	}
	state, payload := b.decode(value)
	args := append(append([]any{}, b.args...), state, payload)
	var query string
	if op == "update" {
		args = append(args, rev)
		query = "UPDATE " + b.table + fmt.Sprintf(" SET state=$%d,%s=$%d,version=nextval('voting_state_revision'),updated_at=now() WHERE ", len(b.args)+1, b.payload, len(b.args)+2) + where(b) + fmt.Sprintf(" AND version=$%d RETURNING version", len(args))
	} else {
		cols := append(append([]string{}, b.keys...), "state", b.payload)
		params := make([]string, len(args))
		for i := range params {
			params[i] = fmt.Sprintf("$%d", i+1)
		}
		query = "INSERT INTO " + b.table + " (" + strings.Join(cols, ",") + ") VALUES (" + strings.Join(params, ",") + ") ON CONFLICT (" + strings.Join(b.keys, ",") + ") "
		if op == "create" {
			query += "DO NOTHING"
		} else {
			query += "DO UPDATE SET state=EXCLUDED.state," + b.payload + "=EXCLUDED." + b.payload + ",version=nextval('voting_state_revision'),updated_at=now()"
		}
		query += " RETURNING version"
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var version uint64
	err = s.db(ctx).QueryRow(ctx, query, args...).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		if op == "create" {
			return 0, domain.ErrEventStateExists
		}
		return 0, domain.ErrEventStateRevisionMismatch
	}
	if err != nil {
		return 0, unavailable()
	}
	return version, nil
}

// LookupInvitation distinguishes a missing guard from storage failure.
func (s *Store) LookupInvitation(ctx context.Context, id string) (domain.InvitationGuard, error) {
	e, err := s.Get(ctx, "v1_vote_response_lfid_invite_sent."+id)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return domain.InvitationGuard{}, nil
	}
	if err != nil {
		return domain.InvitationGuard{}, err
	}
	value := string(e.Value())
	inviteID := value
	if value == "pending" || value == "!del" {
		inviteID = ""
	}
	return domain.InvitationGuard{Exists: true, Pending: value == "pending", InviteID: inviteID, Revision: e.Revision()}, nil
}

// ClaimInvitation atomically selects one dispatcher before a remote send.
func (s *Store) ClaimInvitation(ctx context.Context, id string) (uint64, error) {
	return s.write(ctx, "v1_vote_response_lfid_invite_sent."+id, []byte("pending"), "create", 0)
}

// CompleteInvitation stores the remote result only under the original claim.
func (s *Store) CompleteInvitation(ctx context.Context, id string, rev uint64, inviteID string) error {
	if inviteID == "" {
		return domain.NewValidationError("invite ID is required")
	}
	_, err := s.write(ctx, "v1_vote_response_lfid_invite_sent."+id, []byte(inviteID), "update", rev)
	return err
}

// PrepareInvitation records new-response eligibility, never historical imports.
func (s *Store) PrepareInvitation(ctx context.Context, id string) error {
	if err := domain.CheckSourceFence(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.db(ctx).Exec(ctx, "INSERT INTO voting_invitation_intents(response_id) VALUES($1) ON CONFLICT DO NOTHING", id)
	if err != nil {
		return unavailable()
	}
	return nil
}

// InvitationPrepared recovers eligible unclaimed invitations on redelivery.
func (s *Store) InvitationPrepared(ctx context.Context, id string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var prepared bool
	err := s.db(ctx).QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM voting_invitation_intents WHERE response_id=$1 AND NOT resolved)", id).Scan(&prepared)
	if err != nil {
		return false, unavailable()
	}
	return prepared, nil
}

// ResolveInvitation retains a terminal row that cannot regain eligibility.
func (s *Store) ResolveInvitation(ctx context.Context, id string) error {
	if err := domain.CheckSourceFence(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.db(ctx).Exec(ctx, "INSERT INTO voting_invitation_intents(response_id,resolved) VALUES($1,true) ON CONFLICT(response_id) DO UPDATE SET resolved=true", id)
	if err != nil {
		return unavailable()
	}
	return nil
}

// LockResource owns a session advisory lock and discards uncertain sessions.
func (s *Store) LockResource(ctx context.Context, key string) (context.Context, func(), error) {
	acquireCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	conn, err := s.pool.Acquire(acquireCtx)
	if err != nil {
		return ctx, nil, unavailable()
	}
	var locked bool
	err = conn.QueryRow(acquireCtx, "SELECT pg_try_advisory_lock(hashtextextended($1,1951094))", key).Scan(&locked)
	if err != nil {
		raw := conn.Hijack()
		cleanup, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		_ = raw.Close(cleanup)
		return ctx, nil, unavailable()
	}
	if !locked {
		conn.Release()
		return ctx, nil, domain.ErrResourceBusy
	}
	return context.WithValue(ctx, resourceConnectionKey{}, conn), func() {
		cleanup, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		var unlocked bool
		err := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,1951094))", key).Scan(&unlocked)
		if err != nil || !unlocked {
			raw := conn.Hijack()
			_ = raw.Close(cleanup)
			return
		}
		conn.Release()
	}, nil
}
