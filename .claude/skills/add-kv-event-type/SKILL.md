# Skill: add-kv-event-type

## Purpose

Guide the complete workflow for adding support for a new v1 entity type to the NATS KV event processing pipeline — from domain model definition through KV prefix routing, publisher implementation, tombstone handling, and tests.

## When to use

When a new v1 entity type (identified by a new KV key prefix like `itx-committee.*`) needs to be consumed from the `v1-objects` bucket and synced to the indexer and/or FGA-sync services.

---

## Context you need first

Read these before writing any code:

- `docs/event-processing.md` — full pipeline: ACK/NAK semantics, tombstone pattern, exponential backoff, deduplication, `v1-mappings` bucket purpose.
- `docs/indexer-contract.md` — `IndexingConfig` field meanings (`AccessCheckObject`, `HistoryCheckObject`, `ParentRefs`, `Tags`). Wrong values here cause broken search or access checks.
- `docs/fga-contract.md` — FGA message shapes. Determine whether the new entity needs FGA tuples. (Example: `vote_result` does NOT emit FGA messages; `vote` and `vote_response` do.)
- Existing resource specs as the pattern: `internal/service/eventsync/vote.go`, `vote_response.go`, `vote_result.go`.

---

## Workflow

### Step 1 — Define the domain models

In `internal/domain/event_models.go`, add two structs:

**Raw v1 model** (`XxxDBRaw`) — mirrors the DynamoDB record. All numeric fields that arrive as strings must use a custom JSON type (see `FlexInt`, `FlexFloat` in the existing code) or implement `UnmarshalJSON` to coerce strings to their target type.

**Transformed v2 model** (`XxxData`) — the clean v2 representation. Use `string` UIDs for all identifiers. No ITX types here.

### Step 2 — Extend the EventPublisher interface

In `internal/domain/event_publisher.go`, add:

```go
PublishXxxEvent(ctx context.Context, action string, data *XxxData) error
```

### Step 3 — Implement the publisher method

In `internal/infrastructure/eventing/nats_publisher.go`:

**3a. Define the NATS subject constant:**
```go
IndexXxxSubject = "lfx.index.xxx"
```

**3b. Implement `PublishXxxEvent`:**
- Build `IndexingConfig` — this is the critical piece:
  - `ObjectID` — the entity's UID
  - `AccessCheckObject` / `AccessCheckRelation` — FGA object and relation to check for read access (e.g., `"vote:uid"` / `"viewer"`)
  - `HistoryCheckObject` / `HistoryCheckRelation` — for audit trail (e.g., `"xxx:uid"` / `"auditor"`)
  - `ParentRefs` — slice of `"type:uid"` strings for parent objects the indexer can inherit access from
  - `Tags` — slice of `"key:value"` strings for filtering (e.g., `"vote_uid:xxx"`)
- Decide on FGA: does the indexer need a matching access tuple? If yes, call `sendXxxAccessMessage` (build `fgatypes.GenericFGAMessage` with `ObjectType`, `Operation: "update_access"`, relations, references). If no (read-only child), skip FGA.
- For delete: call `sendIndexerDeleteMessage` and `sendDeleteAccessMessage` (if FGA is involved).

### Step 4 — Add a mapping kind

In `internal/domain/mapping_store.go`, add a `MappingKindXxx` constant. Its string value is the
prefix of the `v1-mappings` key (`"xxx.<uid>"`), which is an on-disk format: pick it once.

### Step 5 — Add a resource spec to the sync module

> **Architecture:** v1→v2 sync logic lives in `internal/service/eventsync`, behind one
> interface, `Syncer.Sync`. `cmd/voting-api/eventing/` is only the NATS adapter (consumer,
> ACK/NAK, backoff). Do not add sync logic to `cmd/`.

Create `internal/service/eventsync/xxx.go` with:

**`convertMapToXxxData`** — converts the decoded `map[string]interface{}` to `*domain.XxxData`.
Tightly coupled to the wire format, so it stays a thin adapter:
1. `json.Marshal` the map to bytes
2. `json.Unmarshal` into `XxxDBRaw`
3. Build `XxxData`, copying field names and coercing types (e.g. string → int)
4. Call `idMapper.MapProjectV1ToV2` / `MapCommitteeV1ToV2` for SFID → UUID translation

**`xxxResource()`** — returns a `*resource[domain.XxxData]`. The shared update and delete flows in
`resource.go` already handle conversion errors, the UID check, create-vs-update detection,
publishing, mapping writes and tombstone-before-delete. The spec only states what differs:
- `label`, `handlerName`, `kind`, `uidLogKey`, `convert`, `uid`, `missingUIDMessage`
- `retryOnConvertError` — retry transient conversion errors, or treat every one as permanent
- `prepare` — the resource's own rules before publishing: required fields, parent ordering
  (`s.deps.Mappings.State(ctx, parentKind, parentUID)`; return `outcome(Retry)` until the parent is
  synced). Return `nil` to continue
- `tombstoneIsCreate` — whether a tombstoned mapping counts as a create or an update
- `publish` — calls the `domain.EventPublisher` method for the entity
- `afterPublish` — optional best-effort follow-up
- `deleteItem` — the minimal `XxxData{UID: uid}` published for a delete

Then register the key prefix in `New` (`syncer.go`) with a `prefixXxx` constant:
```go
prefixXxx: xxxResource(),
```

### Step 6 — Write tests

Test through `Syncer.Sync` with the in-memory store helper in `internal/service/eventsync` (`memory_store_test.go`: seed and inspect state) and a mock publisher. Do not import `internal/infrastructure` from these tests:

- `TestConvertMapToXxxData` — full conversion, missing optional fields, invalid numeric string, invalid JSON
- Update — happy path (`ActionCreated` vs `ActionUpdated` from mapping state), conversion error, missing UID, parent not yet synced, transient publish error
- Delete — happy path, already-tombstoned (no publish), transient publish error, soft delete (`_sdc_deleted_at`)

### Step 7 — Update documentation

- `docs/indexer-contract.md` — add the new entity's payload schema (field names, types, required vs optional)
- `docs/fga-contract.md` — add the new entity's FGA access config (if FGA tuples are emitted)

### Step 8 — Validate

```bash
make ci
```

---

## Critical correctness checklist

Before marking the work done:

- [ ] The spec's `kind` is a new `MappingKind` whose value is the `v1-mappings` key prefix
- [ ] `tombstoneIsCreate` is set deliberately, and `prepare` waits on the parent where one exists
- [ ] Publish errors go through the shared flow (`retryIfTransient`); do not re-implement ACK/NAK
- [ ] `IndexingConfig.AccessCheckObject` uses the correct FGA object type string
- [ ] FGA message is emitted only when appropriate (not for read-only child entities like `vote_result`)
- [ ] New tests cover the tombstone path and transient error path
- [ ] `docs/indexer-contract.md` updated

---

## Further reading

- `docs/event-processing.md` — full pipeline documentation
- `docs/indexer-contract.md` — IndexingConfig reference
- `docs/fga-contract.md` — FGA message shapes
- `docs/architecture/data-flow.md` — NATS KV event sequence diagram
