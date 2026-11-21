# Voting-owned Postgres event state

ITX remains the business-data source of truth and vote/response/result IDs stay
unchanged. Canonical project/committee translations still use the helper RPC.
Only owned event bookkeeping leaves the shared `v1-mappings` bucket:

- `voting_processed_resources`: source kind/ID with empty, processed, deleted or
  legacy-ID state. Application deletion guards are retained.
- `voting_invitation_guards`: response ID, dispatch status, returned invite ID,
  and a store-local revision for create-only claims and CAS completion.
- `voting_invitation_intents`: new responses eligible for invitation, recorded
  before processed state; historical imported responses acquire no intent.
  Terminal decisions set `resolved=true` without deleting/recreating eligibility.
- `voting_state_cutover`: offline semantic-verification checkpoint.

These are service-owned tables, not the helper's `v1_mappings` table. No ballot
answers, usernames, recipient emails, names or invite payloads are persisted.
The domain interfaces have no external imports. The narrow compatibility reader
for reindex tooling reconstructs old entries from named PG columns, not KV.
Source `v1-objects` replication and business entities are unchanged.

## Configuration and offline cutover

Set `VOTING_STATE_DATABASE_URL` from an approved Secret, using verified TLS in
deployed environments. Helm `eventStateDatabase.secretName`/`secretKey` wire an
existing single-key DSN Secret. Alternatively use
`app.environment.VOTING_STATE_DATABASE_URL.valueFrom`, never both. Operations
have five-second caps and pools have at most ten connections per process.
Runtime and reindex tooling require a verified checkpoint; they do not create
empty state or fall back to legacy KV. Proxy-only local development uses the
existing `EVENT_PROCESSING_ENABLED=false` switch and needs no state database.

1. Provision a voting-owned database/schema and Secret. A migration role owns
   DDL/checkpoint writes; runtime receives table DML, sequence usage and checkpoint
   read access only. Size capacity for all replicas.
2. Stop/drain all legacy mappings writers and voting consumers/repair jobs. Keep
   the approved writer fence through import and deployment. Leave source
   replication and the durable stream intact; verify retention headroom.
3. Run `go run ./scripts/migrate_event_state` with `NATS_URL` and the Secret-loaded
   DSN. Dry-run validates owned source state without creating schema or writing.
    Scans use leader `STREAM.MSG.GET` next-by-subject requests, never direct/follower
    reads or a bulk ephemeral consumer; memory holds
   only owned current entries. Size the job from measured state volume.
4. Reconcile source/history gaps and deliberate differences from historical
   wrapper rows copied into the helper's Postgres table. Those copies are not
   implicitly authoritative and are not overwritten or deleted by this tool.
5. Run the same command with `-apply`. Create-only imports preserve agreeing
   partial rows, compare every entry semantically, and reject conflicts/unmatched
   target rows. Native DEL/PURGE removes source entries; application `!del` stays.
   Source first/last sequence and message count must remain unchanged. Empty or
   unusable source snapshots abort; incomplete imports cannot enable runtime.
6. Deploy with the verified PG Secret while fenced, then restore other still-
   legacy writers. Verify dev first, then production: vote/response/result events,
   parent dependencies, pseudo-anonymous ballot redaction, participant grants,
   deletion/replay/restore and invitations. These are operational rollout steps;
   the PR does not execute them or change the participant-grants flag.

The importer changes no NATS data or deployment settings. Fixed failure categories
identify `already_verified`, `import_busy`, `empty_source`, `invalid_source_state`,
`target_conflict`, `target_extra_rows`, `source_changed` or `infrastructure_failure`;
raw driver/source data is not logged. Verify checkpoint state before retrying
after an ambiguous result. A completed checkpoint refuses reimport because PG
may contain newer state; never erase it to force a retry.

## Concurrency and rollback

State read failures return `MappingUnavailable` and retry before publishing,
never a guessed absence/create. Processed/deletion persistence failures retry.
New-response invitation intents allow an updated redelivery to recover from
pre-dispatch guard read/claim failures without inviting historical responses.
Only one create-only claim wins; completion uses that claim's PG revision.
Pending/sent guards suppress another dispatch. Ambiguous send/completion outcomes
retain pending state for manual reconciliation, never automatic expiry/resend.
PG revisions stay monotonic across delete/recreate cycles.

Recoverable deliveries have no finite attempt ceiling. Backoff is capped at
2s/10s/20s, warnings explain state/intent failures, and active callbacks extend
their acknowledgment deadline. Permanent invalid payloads still ACK. Monitor
backlog and retained-source headroom; expired/purged source messages cannot be
recovered by retry configuration.

Same-source handlers use PG session advisory locks across replicas, reusing the
locked connection for state operations to avoid starving a fully locked pool.
Runtime carries the source stream revision and checks current source through the
leader API before publication, dispatch and PG writes. Busy locks retry. NATS
publications are flushed before releasing the lock. Runtime needs leader
`STREAM.MSG.GET` permissions on the configured source stream as well as its
existing consumer/KV permissions.

This is not a distributed transaction or strict downstream version enforcement:
a source change or lost lock connection can race an already-started external
write, and independent index/FGA subjects/consumers can reorder effects. Strict
end-to-end ordering requires companion downstream version/outbox work. Subsequent
current-source deliveries reconcile handler state; this PR does not claim atomic
index/FGA effects.

Pending claims are retained even if a source change prevents dispatch after claim
creation. Inventory without recipient data:

```sql
SELECT response_id, version, updated_at
FROM voting_invitation_guards WHERE state = 'pending'
ORDER BY updated_at, response_id;
```

Check invite-service evidence and current source for each claim. Retain ambiguous
or confirmed-dispatched guards; complete a confirmed result using the observed
revision and returned invite ID. Reset only under an explicit operator approval,
with proof no dispatch occurred and consumers fenced. Never bulk purge or use
elapsed age as proof that resending is safe.

Before upgraded PG writes, rollback may resume the old image against unchanged
KV. After PG writes, KV is stale: fence/drain processing and explicitly reconcile
an approved rollback target before using the old image. No dual-write or stale-
read fallback is provided. Remove obsolete shared KV/PG copies only after verified
owner cutovers and explicit cleanup approval. Bucket retirement remains gated by
other services and read-only tooling in ops #1649.

`scripts/reindex-vote-responses` now reads PG processing state and re-puts only
source `v1-objects` records. Historical imported response markers have no invite
intent; replay alone does not generate an invitation. `migrate_event_state` is
the sole explicit offline legacy mappings reader in production tooling.

## Tests

Set `VOTING_STATE_TEST_DATABASE_URL` to a disposable local PG database and run
`make ci`. Tests create/drop only unique test schemas. Without the variable,
database tests skip. Real PG tests cover state transitions, failure-vs-absence,
concurrent invitation claims, CAS, and delete/recreate revision safety. Migration
tests reject changed source, extra target rows, empty/invalid source, conflicts
and repeat imports without enabling runtime. They use an in-process NATS server for all four owned families, interleaved
unrelated keys, multiple revisions and native delete/purge/recreation. Domain
sync tests cover pre-dispatch retry recovery, parent ordering and privacy. CI
provides disposable PG so database tests do not silently skip there.
Production-wiring tests use public `NewEventProcessor`, a local verified PG
fixture and NATS without the legacy mappings bucket; they cover persistence,
invitation guards and graceful in-flight dispatch completion.
