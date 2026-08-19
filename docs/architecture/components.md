# Components — LFX Voting Service

This document describes each significant architectural component. For file-level detail see the
source files linked in each section.

---

## 1. Goa API DSL (Source of Truth)

**Location:** `api/voting/v1/design/`  
**Files:** `voting.go`, `types.go`

### Responsibility

Defines the entire HTTP API contract: endpoints, request/response shapes, required fields, security
schemes, and HTTP status codes. This is the **single source of truth** for the API — all generated
server and client code derives from it.

### Entry point

There is no runtime entry point. `make apigen` (runs `goa gen`) reads these files and regenerates
`gen/`.

### Important dependencies

- Goa DSL (`goa.design/goa/v3/dsl`) — imported as a dot import per Goa convention
- `types.go` defines reusable Goa attribute types referenced by `voting.go`

### What it should NOT do

Contain any business logic, request processing, or runtime code. Only API shape declarations.

### Boundary note

**Never edit `gen/` directly.** Any change to the API contract must start here, followed by
`make apigen`. The `make verify` CI target enforces this by re-running generation and checking for
diffs.

---

## 2. Generated Goa Code

**Location:** `gen/`  
**Key subdirectories:** `gen/vote/`, `gen/http/vote/server/`, `gen/http/vote/client/`,
`gen/http/openapi/`

### Responsibility

Provides the Go service interface (`gen/vote/service.go`), endpoint wiring (`gen/vote/endpoints.go`),
HTTP encode/decode logic, server mounting, and client utilities. Also generates committed OpenAPI
specs (`gen/http/openapi.yaml`, `openapi3.yaml`, etc.).

### What it should NOT do

Be edited by hand. It is fully regenerated on every `make apigen` run.

### Important note for agents

Any diff inside `gen/` that was not produced by `make apigen` will be overwritten on the next
regeneration and will fail the `make verify` CI check.

---

## 3. HTTP API Layer

**Location:** `cmd/voting-api/`  
**Files:** `main.go`, `api.go`, `api_votes.go`, `api_vote_responses.go`  
**Converter sub-package:** `cmd/voting-api/service/`

### Responsibility

- Implements the Goa-generated `vote.Service` interface (`api.go`)
- Translates Goa-generated payload types → internal service request types (converters in
  `cmd/voting-api/service/vote_converters.go`, `vote_response_converters.go`)
- Maps domain errors → Goa error types (`api.go:handleError`)
- Wires all components together and manages the HTTP server lifecycle (`main.go`)
- Registers middleware: OTel tracing, request ID, authorization context, request logger

### Entry point

`main.go:run()` — reads environment config, constructs all dependencies, starts the HTTP server,
the event processor goroutine, and the invite-accepted subscriber.

### Important dependencies

- `internal/service` — all business logic delegated here
- `gen/vote` — Goa-generated service interface it must satisfy
- `internal/domain` — error types for response mapping
- `internal/middleware` — HTTP middleware chain
- `internal/logging`, `pkg/utils` — structured logging, OTel initialization

### What it should NOT do

Contain business logic. The API layer converts types and handles HTTP concerns; all decisions
belong in the service layer.

### Health endpoints

Three health routes are registered directly on `main.go`, all returning 200 unconditionally:
- `GET /health` — JSON `{"status":"ok"}`
- `GET /livez` — plain text `OK`
- `GET /readyz` — plain text `OK`

> **Note:** No actual readiness logic exists. NATS or ITX failures do not affect these responses.

---

## 4. Service Layer

**Location:** `internal/service/`  
**Files:** `vote_service.go`, `vote_response_service.go`

### Responsibility

All business logic for the synchronous HTTP proxy path:

- Extracts and validates the JWT principal from context
- Maps v2 project/committee UIDs → v1 SFIDs before calling ITX
- Maps v1 SFIDs → v2 UIDs in ITX responses
- Constructs ITX request structs from internal request types
- Delegates ITX calls to the `domain.PollClient` / `domain.VoteResponseClient` interfaces
- Returns domain errors

### Important dependencies

- `internal/domain.IDMapper` — ID translation (interface, injected at startup)
- `internal/domain.PollClient` / `VoteResponseClient` — ITX calls (interface, injected)
- `internal/domain.Authenticator` — JWT principal extraction (interface, injected)
- `pkg/constants.PrincipalContextID` — context key for the principal string
- `pkg/models/itx` — ITX wire-format types (direct coupling — see `boundaries.md`)

### What it should NOT do

Import `internal/infrastructure` directly. All external system access must go through domain
interfaces. HTTP concerns (headers, status codes, Goa types) belong in the API layer.

### Internal request types

`CreateVoteRequest`, `UpdateVoteRequest`, and related types are defined at the bottom of
`vote_service.go`. These are the internal types the API layer converts Goa payloads into.

---

## 5. ITX Proxy Client

**Location:** `internal/infrastructure/proxy/client.go`  
**Size:** ~712 lines

### Responsibility

All outbound HTTP calls to the ITX Voting API:

- `CreatePoll`, `GetPoll`, `UpdatePoll`, `DeletePoll`, `ExtendPoll`, `EnablePoll`,
  `BulkResendPoll`, `GetPollResults` — implements `domain.PollClient`
- `CreateVote`, `GetVote`, `UpdateVote`, `ResendVote` — implements `domain.VoteResponseClient`
- `AcceptInvite` — implements `domain.InviteAcceptanceClient`
- Manages OAuth2 M2M token lifecycle (Auth0, private-key JWT assertion) via
  `oauth2.ReuseTokenSource`
- Maps ITX HTTP status codes → `domain.DomainError` types

### Important dependencies

- `auth0/go-auth0` — Auth0 SDK for private-key JWT client assertion
- `golang.org/x/oauth2` — token caching and renewal
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` — outbound trace propagation
- `pkg/models/itx` — ITX request/response types
- `internal/domain` — error constructors

### Startup constraint

`proxy.NewClient` **panics** if `ITX_CLIENT_PRIVATE_KEY` is empty. There is no graceful fallback.

### Known technical debt

Every method follows the same pattern (build request → set headers → execute → read body → map
errors → unmarshal) with no shared helper. This produces ~667 lines of repetition.
See `tmp/refactoring-suggestions.md` §2 for a proposed `doJSON[T]` / `doNoContent` refactor.

---

## 6. ID Mapper

**Location:** `internal/infrastructure/idmapper/`  
**Files:** `nats_mapper.go` (NATS implementation), `noop_mapper.go` (local dev stub)

### Responsibility

Translates between v2 UUIDs and v1 SFIDs for projects and committees via NATS request/reply.
Implements `domain.IDMapper`.

Four operations:
- `MapProjectV2ToV1` / `MapProjectV1ToV2`
- `MapCommitteeV2ToV1` / `MapCommitteeV1ToV2`

The NATS subject is `lfx.lookup_v1_mapping` (defined as `lookupSubject` in the package). The
lookup request and response are JSON-encoded structs whose exact schema is owned by the upstream
v1-sync-helper service.

### Local dev

`idmapper.NewNoOpMapper()` passes all IDs through unchanged. This means calls to ITX will include
v2 UUIDs when `ID_MAPPING_DISABLED=true` — they will be rejected by ITX. This is intentional for
local dev without NATS.

### Important dependency

NATS connection (`NATS_URL`). A 5-second request timeout is hardcoded.

---

## 7. Event Processor

**Location:** `cmd/voting-api/eventing/`  
**Files:** `event_processor.go`, `kv_handler.go`, `vote_event_handler.go`,
`vote_response_event_handler.go`, `vote_result_event_handler.go`,
`invite_accepted_subscriber.go`, `vote_response_invite.go`

### Responsibility

Processes NATS JetStream KV change events from the `v1-objects` bucket:

- Creates and manages a durable JetStream consumer on stream `KV_v1-objects`
- Routes KV messages by key prefix to per-entity handlers
- Transforms v1 data (string numerics, SFIDs) to v2 format (typed numerics, UUIDs)
- Uses the ID mapper to translate project/committee IDs
- Calls `domain.EventPublisher` to forward events to indexer-service and fga-sync
- Tracks processed records in the `v1-mappings` KV bucket for create/update/delete deduplication

**Three KV key prefixes handled:**
- `itx-poll.*` → vote (poll) events
- `itx-poll-vote.*` → vote response (ballot) events
- `itx-poll-results.*` → vote result snapshot events

### Runs independently of the HTTP path

The event processor starts in a separate goroutine (`main.go`) and has no connection to the HTTP
request path. It shares only the ID mapper instance.

### Invite path

When `INVITES_ENABLED=true`:
- The `VoteResponseInviteHandler` (in `vote_response_invite.go`) checks whether to send an LFID
  invite for each vote-response event that lacks a username
- `InviteAcceptedSubscriber` (independent of the event processor) subscribes to
  `lfx.invite-service.invite_accepted` and calls ITX's `POST /v2/voting/vote/invite_accepted`

### Architectural note

Substantial processing logic lives in `cmd/` rather than `internal/`. This is inconsistent with
the stated architecture (see `boundaries.md`).

---

## 8. NATS Publisher

**Location:** `internal/infrastructure/eventing/nats_publisher.go`  
**Supporting file:** `internal/infrastructure/eventing/tracing.go`

### Responsibility

Publishes processed event data to two downstream services:

- **indexer-service** via `lfx.index.vote`, `lfx.index.vote_response`, `lfx.index.vote_result`
- **fga-sync** via `lfx.fga-sync.update_access` / `lfx.fga-sync.delete_access`

Constructs `IndexerMessageEnvelope` and `GenericFGAMessage` structs (from external Go module
contracts), injects OTel trace context into NATS message headers, and publishes fire-and-forget.

### Important dependencies

- `github.com/linuxfoundation/lfx-v2-fga-sync/pkg/...` — FGA message types and subjects
- `github.com/linuxfoundation/lfx-v2-indexer-service/pkg/...` — indexer message types and
  subjects
- `internal/domain` — `VoteData`, `VoteResponseData`, `PollResultData` model types

---

## 9. JWT Authenticator

**Location:** `internal/infrastructure/auth/jwt.go`

### Responsibility

Validates inbound JWTs against Heimdall's JWKS endpoint. Extracts the principal claim (LFX
username) and returns it as a string. Supports a local dev bypass via
`JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL` — when set to any non-empty string, JWT validation is
skipped and that string is used as the principal.

Implements `domain.Authenticator`.

---

## 10. Domain Interfaces

**Location:** `internal/domain/`

### Responsibility

Defines the contracts between the service layer and infrastructure:

| Interface | File | Implemented by |
|---|---|---|
| `Authenticator` | `auth.go` | `infrastructure/auth.JWTAuth` |
| `PollClient` | `proxy.go` | `infrastructure/proxy.Client` |
| `VoteResponseClient` | `proxy.go` | `infrastructure/proxy.Client` |
| `InviteAcceptanceClient` | `invite_acceptance_client.go` | `infrastructure/proxy.Client` |
| `IDMapper` | `id_mapper.go` | `infrastructure/idmapper.NATSMapper` / `NoOpMapper` |
| `EventPublisher` | `event_publisher.go` | `infrastructure/eventing.NATSPublisher` |
| `InviteSender` | `invite_sender.go` | `infrastructure/nats.InviteSender` |
| `UserReader` | `user_reader.go` | `infrastructure/nats.UserReader` |

Also defines:
- `DomainError` + `ErrorType` enum (`errors.go`) — the error pattern for the whole service
- `VoteData`, `VoteResponseData`, `PollResultData` — v2 transformed models (`event_models.go`)
- `PollDBRaw`, `VoteDBRaw`, `PollResultDBRaw` — v1 raw models with custom `UnmarshalJSON`
  (`event_models.go`)

### What it should NOT do

Import any infrastructure packages. Domain is the innermost layer.

---

## 11. Middleware

**Location:** `internal/middleware/`  
**Files:** `authorization.go`, `request_id.go`, `request_logger.go`

### Responsibility

Three HTTP middleware functions applied to all routes in `main.go`:

- `AuthorizationMiddleware` — extracts the raw `Authorization` header and stores it in context
  (for forwarding in NATS event headers)
- `RequestIDMiddleware` — generates and attaches a request ID
- `RequestLoggerMiddleware` — logs structured HTTP request/response data

Applied in reverse registration order (outermost is registered last):
`otelhttp → RequestLogger → RequestID → Authorization → mux`

---

## Documentation Gaps

- The `cmd/voting-api/service/` converter package has no doc comment or README explaining its role
  relative to the service layer and the Goa-generated types.
- The exact schema of `lfx.lookup_v1_mapping` NATS request/response messages is owned by the
  v1-sync-helper service and not documented here.
- No documentation on how `v1-mappings` KV bucket keys are structured or who else writes to them.
