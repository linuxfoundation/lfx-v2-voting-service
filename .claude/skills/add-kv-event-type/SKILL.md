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
- Existing handler trio as the pattern: `vote_event_handler.go`, `vote_response_event_handler.go`, `vote_result_event_handler.go`.

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

### Step 4 — Add KV prefix routing

In `cmd/voting-api/eventing/kv_handler.go`:

In `handleKVPut` switch statement:
```go
case "itx-xxx":
    return handleXxxUpdate(ctx, key, v1Data, publisher, idMapper, mappingsKV, logger)
```

In `handleResourceDelete` switch statement:
```go
case "itx-xxx":
    return handleXxxDelete(ctx, uid, publisher, mappingsKV, logger)
```

### Step 5 — Create the event handler file

> **Architecture boundary:** `cmd/voting-api/eventing/` is known tech debt — business logic
> has accumulated here instead of in `internal/service/`. **Do not add new business logic here.**
> Keep the handler thin: decode the payload, call an `internal/service/` method for any
> substantive transformation or decision-making, then publish the result. Only the KV routing,
> ACK/NAK semantics, and tombstone tracking belong in this package.
> See `AGENTS.md` and `.cursor/rules/eventing-boundary.mdc` for the full boundary rule.

Create `cmd/voting-api/eventing/xxx_event_handler.go` with:

**`convertMapToXxxData`** — converts `map[string]interface{}` to `*domain.XxxData`:
1. `json.Marshal` the map to bytes
2. `json.Unmarshal` into `XxxDBRaw`
3. Build `XxxData`, mapping field names and types
4. Call `idMapper.MapProjectV1ToV2` / `MapCommitteeV1ToV2` for any SFID → UUID conversions; return a wrapped error so the caller can apply `isTransientError`

**`handleXxxUpdate`** — full update handler:
1. `convertMapToXxxData` — on error, check `isTransientError`, return accordingly
2. Validate required fields (UID must be non-empty)
3. Check `mappingsKV.Get(ctx, "xxx."+uid)` to determine `ActionCreated` vs `ActionUpdated`
4. `publisher.PublishXxxEvent(ctx, action, data)` — on transient error return `true` (NAK), on permanent error return `false` (ACK)
5. `mappingsKV.Put(ctx, "xxx."+uid, []byte("1"))` to record the mapping (warn on failure, don't retry)
6. Return `false` (success ACK)

**`handleXxxDelete`** — tombstone-aware delete handler:
1. Check tombstone: `mappingsKV.Get(ctx, "xxx."+uid)` — if value is `"!del"`, return `false` (already processed)
2. Build minimal `XxxData{UID: uid}` for the delete event
3. `publisher.PublishXxxEvent(ctx, string(ActionDeleted), data)` — on transient error return `true`
4. `mappingsKV.Put(ctx, "xxx."+uid, []byte(tombstoneMarker))` to prevent duplicate deletes

**Why the tombstone matters:** The JetStream consumer can redeliver delete operations. Without the tombstone check, a redelivered delete publishes a second delete event to the indexer, which it handles incorrectly.

### Step 6 — Write tests

Create `cmd/voting-api/eventing/xxx_event_handler_test.go` with table-driven tests:

- `TestConvertMapToXxxData` — cover: full conversion, missing optional fields, invalid numeric string, invalid JSON
- `TestHandleXxxUpdate` — cover: happy path (verify `ActionCreated` vs `ActionUpdated` from mapping state), conversion error, missing UID, transient publish error
- `TestHandleXxxDelete` — cover: happy path, already-tombstoned (no publish), transient publish error

Use the mock pattern from `mock_kv_test.go` and the inline `eventPublisher` mock in existing test files.

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

- [ ] `handleXxxDelete` checks the tombstone **before** publishing
- [ ] `handleXxxUpdate` uses `mappingsKV.Get` to distinguish `created` vs `updated`
- [ ] `isTransientError` is called on all publish/ID-mapping errors
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
