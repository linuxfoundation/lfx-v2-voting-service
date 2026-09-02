# Skill: add-api-endpoint

## Purpose

Guide the complete workflow for adding a new HTTP API endpoint to the voting service — from the Goa DSL definition through code generation, ITX proxy client, service logic, converters, and Goa handler implementation.

## When to use

When adding a new endpoint (new action, new resource variant, or new operation on an existing resource). Endpoints on this service proxy to the ITX API — every endpoint needs a Goa definition, an ITX proxy method, and a service method.

---

## Context you need first

Before writing any code, read:

- `docs/api-contracts.md` — LFXv2 ↔ ITX field name mappings. These rename throughout the stack (`PollID` → `UID`, `ProjectID` → `ProjectUID`). Getting them wrong causes silent data corruption.
- `docs/architecture/boundaries.md` — which layer owns what; where each piece of code belongs.
- `internal/domain/errors.go` — `ErrorType` enum values and how they map to HTTP status codes.
- An existing endpoint pair to understand the expected pattern: `cmd/voting-api/api_votes.go` (handler) + `internal/service/vote_service.go` (service method) + `internal/infrastructure/proxy/client.go` (proxy method).

---

## Workflow

### Step 1 — Define the endpoint in the Goa DSL

Edit `api/voting/v1/design/voting.go`. Add the new method to the appropriate service (`vote` or `vote_response`). Follow the existing method shape exactly:

```go
Method("method_name", func() {
    Description("...")
    Security(JWTAuth)
    Payload(func() { ... })
    Result(TypeName)
    HTTP(func() {
        POST("/path")
        Response(StatusOK)
        Response("NotFound", StatusNotFound)
        // add all error responses your method can return
        // NOTE: error names are PascalCase (e.g. "NotFound", "BadRequest") —
        // they must match the Error() declarations at the service level exactly.
        // Wrong casing causes a Goa generation error.
    })
})
```

Add types to `api/voting/v1/design/types.go` if the payload or result needs new fields.

### Step 2 — Regenerate API code

```bash
make generate
```

This regenerates `gen/`. **Commit `gen/` changes alongside the design file changes** — they must stay in sync.

Verify: `gen/http/openapi.yaml` now contains the new endpoint.

### Step 3 — Add ITX model types (if needed)

If ITX has a corresponding new request or response shape that doesn't exist yet, add it to `pkg/models/itx/`. Follow the naming and JSON tag conventions already present there.

### Step 4 — Extend the domain interface

Add the method signature to the appropriate interface in `internal/domain/proxy.go` (`PollClient` or `VoteResponseClient`).

**Rule:** The domain interface must only use types from `internal/domain/` or `pkg/models/itx/`. Never import from `internal/infrastructure/` or `gen/`.

### Step 5 — Implement the proxy method

Add the implementation to `internal/infrastructure/proxy/client.go`. Follow the existing method structure exactly:

1. Build the ITX request struct
2. Call the appropriate HTTP helper (`doJSON[T]` pattern or the current method-per-verb pattern if the refactor hasn't landed)
3. Map ITX error responses to `domain.DomainError` using `mapHTTPError`

Consult `docs/api-contracts.md` for every field that renames between LFXv2 and ITX.

### Step 6 — Add the service method

Add the method to the appropriate file in `internal/service/` (`vote_service.go` or `vote_response_service.go`).

Service method responsibilities:
- Extract and validate the principal from context using the shared `requirePrincipal(ctx)` helper
  (defined in `internal/service/vote_service.go`). Do **not** call `ctx.Value(constants.PrincipalContextID)`
  directly — that pattern has been replaced to avoid duplication.
- Call `idMapper.MapXxxV2ToV1` / `MapXxxV1ToV2` for any UID ↔ SFID translation
- Call the domain interface method (proxy client)
- Return `domain.DomainError` for all error paths

**Never** call infrastructure packages directly. Call only domain interfaces.

### Step 7 — Add payload → service request converter

Add a `ConvertXxxPayloadToDomain` function to `cmd/voting-api/service/`. It converts the Goa-generated payload type (from `gen/vote`) to the service-layer request type (from `internal/service/`).

Keep all field renames explicit and documented with a comment if the naming is non-obvious.

### Step 8 — Add service response → Goa result converter

Add a `ConvertXxxToResult` function (same file or a new `xxx_converters.go`). It converts the response from the proxy/service into the Goa result type (from `gen/vote`).

Field renames between ITX and LFXv2 happen here. Cross-check with `docs/api-contracts.md`.

### Step 9 — Implement the Goa handler

Add the method implementation in `cmd/voting-api/api.go` (or `api_votes.go` / `api_vote_responses.go` for the longer files). The handler must:

1. Call the converter to build the service request
2. Call the service method
3. Map errors using the shared `handleError(err)` helper (defined in `cmd/voting-api/api.go`)
4. Call the response converter and return

Error mapping pattern — use `handleError`, which already covers every `ErrorType`:
```go
result, err := s.voteService.MyMethod(ctx, req)
if err != nil {
    return nil, handleError(err)
}
return svc.ConvertMyMethodToResult(result), nil
```

Do **not** call `vote.MakeNotFound`, `vote.MakeBadRequest`, `handleVoteError`, or
`handleVoteResponseError` — those functions do not exist. `handleError` is the single entry point
for domain → Goa error conversion in this service.

### Step 10 — Validate

```bash
make ci
```

All of the following must pass: `make verify` (gen/ is current), `make check` (format + lint), `make build`, `make test`.

---

## Adding a new HTTP error status (e.g. 429 Rate Limited)

If you need an error type that currently doesn't exist in the domain (see `internal/domain/errors.go`), you must wire it through **four places**:

1. **`internal/domain/errors.go`** — add `ErrorTypeXxx ErrorType = iota` and `NewXxxError(...)` constructor.
2. **`internal/infrastructure/proxy/client.go`** — update `mapHTTPError` to return `domain.NewXxxError` for the new status code(s).
3. **`api/voting/v1/design/voting.go`** — add `Error("xxx_name", XxxError)` to every method that can return this error, and define the Goa error type in `types.go`.
4. **`cmd/voting-api/api.go`** — add `case domain.ErrorTypeXxx: code = http.StatusXxx` in `handleError`.

Then run `make deps && make generate` after step 3 (Goa must generate the new `*votesvc.XxxError` struct before the handler can use it).

**Prerequisite:** `make deps` must be run first to ensure the local `goa` CLI matches `go.mod`. A version mismatch causes `make generate` to fail silently with a compile error.

## Things that commonly go wrong

- **Skipping `make generate`** — the build compiles against stale generated types, producing confusing errors or silent mismatches.
- **Putting converters in the wrong package** — Goa payload → service request converters belong in `cmd/voting-api/service/`, not in `internal/service/`.
- **Missing error responses in the DSL** — if a Goa method doesn't declare an error response (e.g., `Response("not_found", StatusNotFound)`), Goa won't generate the helper function for it and the handler won't compile.
- **Wrong field names in converters** — always compare against `docs/api-contracts.md`, not the ITX or Goa source alone.
- **Importing `gen/` from `internal/`** — generated types must not be imported below `cmd/voting-api/`. If you need a type in both layers, define it in `internal/service/` or `internal/domain/` and convert at the `cmd/` boundary.

---

## Further reading

- `docs/api-contracts.md` — field mapping reference
- `docs/architecture/boundaries.md` — layer rules and known violations
- `docs/itx-proxy-implementation.md` — proxy pattern deep-dive
- `AGENTS.md` — layer table and quick reference
