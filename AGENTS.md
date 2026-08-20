# AGENTS.md — LFX V2 Voting Service

## Repository Structure

| Path | Purpose |
|---|---|
| `api/voting/v1/design/` | Goa DSL — API contract source of truth |
| `gen/` | **Generated** — never edit directly |
| `cmd/voting-api/` | Application entry point, handlers, wiring, converters |
| `cmd/voting-api/eventing/` | NATS KV event processing (asynchronous path) |
| `internal/domain/` | Interfaces and models — no external imports allowed |
| `internal/service/` | Business logic |
| `internal/infrastructure/` | ITX proxy, NATS publisher, ID mapper, auth |
| `docs/` | Architecture and design documentation |
| `charts/` | Helm chart for Kubernetes deployment |

## Development Commands

```bash
make setup      # first-time: copy .env.example → .env
make deps       # install goa CLI, golangci-lint, download modules
make generate   # regenerate API code from Goa design (run after editing api/)
make build      # build binary to bin/voting-api
make run        # build and run (requires: source .env first)
make test       # go test ./... -race -timeout 5m
make check      # format check + lint (no file modifications)
make ci         # full pre-submit: verify + check + build + test
make tidy       # go mod tidy
make clean-bin  # remove bin/ only (preserves gen/)
```

Run `make ci` before opening a pull request. Run `make help` for all targets.

## Architecture Layers

Dependencies flow **inward only**. Never import infrastructure from service or domain.

| Layer | Path | Rule |
|---|---|---|
| API design | `api/voting/v1/design/` | Shape only — no logic |
| Generated | `gen/` | Never edit directly |
| Domain | `internal/domain/` | Interfaces + models — zero external imports |
| Service | `internal/service/` | Business logic — call domain interfaces only |
| Infrastructure | `internal/infrastructure/` | External systems — never imported by service/domain |
| App wiring | `cmd/voting-api/` | Handlers, converters, dependency injection |
| Event processing | `cmd/voting-api/eventing/` | Known tech debt: contains business logic that belongs in service layer — do not add more here |

See `docs/architecture/boundaries.md` for the full boundary map including known violations.

## Coding Conventions

**License header** — every new source file must begin with:
```
// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT
```
Use `#` prefix for YAML, Makefile, and shell files. CI enforces this on all files except `gen/` and `cmd/voting-api/kodata/*`.

**Logging** — use `slog`; never `fmt.Println` or `log.Printf`.

**Errors** — use the `ErrorType` enum in `internal/domain/errors.go`. Return domain errors from service/infrastructure; convert to Goa errors only in `cmd/voting-api/api.go`.

> **Why ITX 401/403 maps to `ErrorTypeInternal`**: When the proxy receives a 401 or 403 from ITX it means the service's own M2M token is invalid or expired — an infrastructure problem, not a client-auth failure. There is deliberately no `ErrorTypeForbidden`. See the comment in `internal/domain/errors.go` for the full rationale.

**Doc comments** — all exported functions and types must have Go doc comments.

**Terminology** — "vote" = a poll/election (what ITX calls a "poll"); "vote response" = a ballot submission. See `docs/glossary.md`.

## Generated Code

`gen/` is produced by Goa from the DSL in `api/voting/v1/design/`.

- Never edit `gen/` directly — changes are overwritten on next `make generate`.
- Run `make deps` first if you have not set up this repo — it installs the `goa` CLI at the exact version pinned in `go.mod`. A version mismatch causes `make generate` to fail with a compile error.
- Run `make generate` (or `go generate ./api/...`) after editing design files and commit the result.
- `make ci` calls `make verify`, which regenerates and fails if `gen/` is stale.

## Validation After Changes

1. `make build` — confirms the change compiles
2. `make test` — full suite with race detector
3. `make check` — formatting + lint

Before opening a PR: `make ci` (covers all of the above plus generated code check).

To test without a live ITX connection, mock the domain interfaces in `internal/domain/`. The service layer depends only on those interfaces.

## Common Workflow: Adding an API Endpoint

1. Define endpoint in `api/voting/v1/design/voting.go`
2. Add types in `api/voting/v1/design/types.go` if needed
3. `make generate` — commit the `gen/` changes
4. Add proxy method in `internal/infrastructure/proxy/client.go`
5. Add service method in `internal/service/`
6. Add converters in `cmd/voting-api/service/`
7. Implement Goa handler in `cmd/voting-api/api.go`

## Dangerous Operations

- **Auth middleware** (`internal/middleware/`) — changes affect every request; must be its own PR
- **NATS event schemas** (indexer and FGA events in `cmd/voting-api/eventing/`) — downstream services depend on the exact format; see `docs/indexer-contract.md` and `docs/fga-contract.md`
- **ITX API field mappings** — mismatches cause silent data corruption; see `docs/api-contracts.md`
- **`internal/domain/` interface changes** — break all infrastructure implementations that implement them
- **`make clean`** removes `gen/`; run `make generate` before the next build

## Further Documentation

| Document | Purpose |
|---|---|
| `docs/architecture/overview.md` | High-level architecture and Mermaid component diagram |
| `docs/architecture/boundaries.md` | Layer boundaries and known violations |
| `docs/architecture/data-flow.md` | Request and event flow sequence diagrams |
| `docs/architecture/components.md` | Per-component responsibilities |
| `docs/event-processing.md` | NATS KV event flow and operational guide |
| `docs/api-contracts.md` | LFXv2 ↔ ITX field mappings |
| `docs/itx-proxy-implementation.md` | Proxy layer deep-dive |
| `docs/glossary.md` | Service-specific terminology (ITX, SFID, v1/v2) |
| `README.md` | Environment variables, running locally, Helm deployment |
| `CONTRIBUTING.md` | PR process, branch naming, commit conventions |
