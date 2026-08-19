# Architectural Boundaries — LFX Voting Service

This document identifies the major architectural boundaries in the codebase, what crosses each
boundary, which component owns the responsibility, and what the evidence is.

It also documents known boundary violations so that future engineers and agents can make informed
decisions rather than treating inconsistencies as intentional rules.

---

## Boundary 1 — Generated Code ↔ Hand-Written Code

### What it is

All code under `gen/` is produced by `goa gen` from the DSL in `api/voting/v1/design/`. It is
regenerated on every `make apigen` run and must never be edited manually.

### What crosses the boundary

- The API layer (`cmd/voting-api/api*.go`) imports `gen/vote` service types and implements the
  `gen/vote.Service` interface.
- `cmd/voting-api/service/` converters import `gen/vote` Goa payload types.
- Generated OpenAPI specs (`gen/http/openapi*.yaml`) are committed and consumed by CI/CD and
  MegaLinter's Spectral linter.

### Which component owns the responsibility

`api/voting/v1/design/` owns the API contract. All downstream generated code is derived.

### What should NOT cross this boundary

No manual edits to `gen/`. No business logic or infrastructure imports inside `gen/`.

### Evidence

- `Makefile:apigen` — `goa gen github.com/linuxfoundation/lfx-v2-voting-service/api/voting/v1/design`
- `Makefile:verify` — regenerates and fails if `git status --porcelain gen/` is non-empty
- `//nolint:staticcheck` comment on the dot import in `design/voting.go` — acknowledges the DSL
  import convention

---

## Boundary 2 — API Layer ↔ Service Layer

### What it is

The API layer (`cmd/voting-api/`) handles HTTP concerns and Goa types. The service layer
(`internal/service/`) handles business logic. They communicate through internal request/response
types (`CreateVoteRequest`, etc.) defined in `internal/service/`.

### What crosses the boundary

- Goa-generated payload types → internal request types (via `cmd/voting-api/service/` converters)
- `*itx.PollResponse` returned from the service layer (see violation below)
- `domain.DomainError` passed from service → API layer for HTTP status mapping

### Which component owns the responsibility

- API layer owns: HTTP decoding, JWT auth delegation, response encoding, error-to-HTTP-status mapping
- Service layer owns: principal extraction, ID translation, ITX call orchestration

### What should NOT cross this boundary

Goa-generated payload types should not appear in the service layer or below. HTTP status code
decisions should not be made in the service layer.

### Known violation

The service layer returns `*itx.PollResponse` and `*itx.VoteResults` (from `pkg/models/itx`)
directly through `internal/domain/proxy.go`. This means the service layer is coupled to ITX wire
formats. The domain/service boundary would be cleaner if a separate v2 domain type were introduced
and the ITX types were confined to `internal/infrastructure/proxy/`.

**Evidence:** `internal/domain/proxy.go:PollClient` interface returning `*itx.PollResponse`;
`internal/service/vote_service.go:CreateVote()` returning `(*itx.PollResponse, error)`.
Noted in `tmp/refactoring-suggestions.md` §5.

---

## Boundary 3 — Service Layer ↔ Infrastructure

### What it is

The service layer depends only on interfaces defined in `internal/domain/`. Infrastructure
implementations in `internal/infrastructure/` satisfy those interfaces. The service layer never
imports `internal/infrastructure` directly.

### What crosses the boundary

- `domain.IDMapper` — injected at startup, used for ID translation
- `domain.PollClient` / `domain.VoteResponseClient` — injected at startup, used for ITX calls
- `domain.Authenticator` — injected at startup, used for JWT parsing

### Which component owns the responsibility

- Service layer owns: business logic (what to do)
- Infrastructure owns: mechanism (how to do it — HTTP, NATS, Auth0)

### Evidence this boundary is followed

- `internal/service/vote_service.go` imports only `internal/domain` and `pkg/...` — no
  `internal/infrastructure` imports
- All infrastructure implementations are injected in `cmd/voting-api/main.go`

---

## Boundary 4 — Synchronous HTTP Path ↔ Asynchronous Event Path

### What it is

The HTTP API path (Flows 1–3 in `data-flow.md`) and the NATS KV consumer path (Flow 4) are
entirely independent execution paths within the same binary. They do not share goroutines,
channels, or mutable state at runtime.

### What crosses the boundary

- The `domain.IDMapper` instance is **shared** between both paths (injected into both
  `VoteService` and `EventProcessor` in `main.go`)
- The `domain.InviteAcceptanceClient` (ITX proxy client) is shared between the HTTP path and the
  `InviteAcceptedSubscriber`

### Which component owns the responsibility

- HTTP path owns: synchronous client-facing operations (CRUD, auth, ID mapping on request/response)
- Event path owns: asynchronous v1→v2 sync, search indexing, FGA tuple management

### What should NOT cross this boundary

The event processor should not make synchronous HTTP calls on behalf of a client request. The HTTP
path should not write to NATS KV buckets or consume JetStream messages.

### Evidence

- `cmd/voting-api/main.go`: `EventProcessor` started in a separate goroutine
- `cmd/voting-api/main.go`: Graceful shutdown stops the event processor before the HTTP server

---

## Boundary 5 — Application Code ↔ Domain Interfaces

### What it is

`internal/domain/` defines all interfaces and shared models. No package outside `internal/domain`
defines the contracts between layers.

### What crosses the boundary

All domain interface types, error types, and model types (`VoteData`, `VoteResponseData`,
`PollResultData`, `DomainError`, etc.) are defined here and used everywhere.

### What should NOT cross this boundary

Infrastructure-specific types (NATS connection objects, `http.Client`, Auth0 structs) should not
appear in `internal/domain/`. Domain interfaces should not import from `internal/infrastructure/`.

### Evidence this boundary is followed

`internal/domain/` imports: only standard library and `pkg/models/itx` (the latter is the
coupling noted in Boundary 2's violation).

---

## Boundary 6 — Business Logic ↔ Event Processing

### Intended boundary

Per standard LFX service architecture, business logic belongs in `internal/service/` or
`internal/domain/`, with `cmd/` containing only wiring/entry-point code.

### Actual state — **VIOLATION**

`cmd/voting-api/eventing/` contains substantial business logic:

- ID mapping and v1→v2 data transformation
- Deduplication logic (checking `v1-mappings` KV)
- Orphan prevention logic (checking parent vote existence)
- Invite eligibility rules
- Error classification (transient vs. permanent)

This logic is not in `internal/` and is not independently testable without the `cmd` package.

### Impact

- The event handlers cannot be imported or tested from outside `cmd/voting-api/eventing/`
  without going through the command package
- Adding a new event type requires work inside `cmd/` rather than `internal/`

### Evidence

`cmd/voting-api/eventing/vote_event_handler.go`, `vote_response_event_handler.go`,
`vote_result_event_handler.go`, `vote_response_invite.go` — all contain non-trivial logic.
Noted in `tmp/refactoring-suggestions.md` §6 as a candidate for relocation to `internal/`.

---

## Boundary 7 — v2 IDs ↔ v1 IDs

### What it is

All v2 UUID ↔ v1 SFID translation is supposed to happen at a single seam. On the HTTP path, this
is the service layer. On the event path, this is inside the entity handlers.

### What crosses the boundary

- Inbound API requests arrive with v2 UUIDs; ITX expects v1 SFIDs
- ITX responses contain v1 SFIDs; LFXv2 clients expect v2 UUIDs
- KV events arrive with v1 SFIDs; downstream services expect v2 UUIDs

### Which component owns the responsibility

- Service layer owns translation for the HTTP path
- Event handlers own translation for the async event path

### Evidence the boundary is followed on the HTTP path

`internal/service/vote_service.go:mapRequestIDsV2ToV1()` and `mapPollResponseV1ToV2()` are the
dedicated translation helpers.

### Partial violation

`internal/domain/proxy.go` interfaces accept and return ITX types (which contain v1 SFIDs) rather
than translated v2 types. This means the ITX proxy client receives and returns v1 IDs directly,
and translation happens around the client call rather than at a clear seam.

---

## Boundary 8 — Configuration ↔ Application

### What it is

All configuration enters the system exclusively through environment variables, loaded in
`cmd/voting-api/main.go:loadConfig()` at startup into a `config` struct. No config files are
read at runtime.

### What crosses the boundary

The populated `config` struct fields are used to construct all infrastructure instances.
No environment variables are read outside `main.go` (except `LFX_ENVIRONMENT` and
`INVITES_ENABLED` in `parseInviteConfig()`).

### What should NOT cross this boundary

Infrastructure packages and service packages should not call `os.Getenv()` directly. Config must
be passed in explicitly.

### Evidence this boundary is followed

`internal/infrastructure/proxy/client.go` accepts a `Config` struct. `internal/infrastructure/
idmapper/nats_mapper.go` accepts a `Config` struct. No `os.Getenv` calls outside `main.go` and
`pkg/utils/otel.go` (OTel reads its own standard env vars independently).

### Exception

`pkg/utils/otel.go:OTelConfigFromEnv()` reads `OTEL_*` environment variables directly. This is
acceptable because OpenTelemetry defines its own standard configuration env var contract.

---

## Summary Table

| Boundary | Status | Location of violation (if any) |
|---|---|---|
| Generated ↔ hand-written | Enforced by CI | — |
| API ↔ service | Followed with one known coupling | `domain/proxy.go` returns ITX types |
| Service ↔ infrastructure | Followed | — |
| HTTP path ↔ event path | Followed | — |
| Application ↔ domain interfaces | Followed | — |
| Business logic in `internal/` | **Violated** | `cmd/voting-api/eventing/` contains logic |
| v2 IDs ↔ v1 IDs seam | Partially followed | Proxy interface accepts/returns ITX types |
| Configuration entry point | Followed | `main.go:loadConfig()` |
