# Skill: add-domain-interface

## Purpose

Guide the workflow for adding a new external dependency as a domain interface — defining the abstraction in `internal/domain/`, implementing it in `internal/infrastructure/`, providing a no-op for local development, and wiring it in `main.go`.

## When to use

When a new external dependency needs to be integrated (new NATS subject pattern, new external HTTP service, new read/write abstraction) and the service/domain layers must remain free of external imports.

---

## Context you need first

- `docs/architecture/boundaries.md` — the zero-external-imports rule for `internal/domain/`.
- `internal/domain/id_mapper.go` and `internal/domain/proxy.go` — existing interface patterns.
- `internal/infrastructure/idmapper/noop_mapper.go` — the no-op pattern for local dev.
- `cmd/voting-api/main.go` — env flag → conditional wiring pattern (look for `ID_MAPPING_DISABLED` and `EVENT_PROCESSING_ENABLED` for examples).
- `internal/domain/errors.go` — use `ErrorTypeUnavailable` when the dependency is temporarily unreachable; callers use this to distinguish transient from permanent errors.

---

## Workflow

### Step 1 — Define the interface in `internal/domain/`

Create a new file `internal/domain/xxx.go` (or add to an existing relevant file).

Rules for domain interfaces:
- **Zero external imports** — only `context`, stdlib, and other `internal/domain/` or `pkg/` types allowed.
- Document every method with a Go doc comment.
- Use `ErrorTypeUnavailable` (from `internal/domain/errors.go`) in the error return contract comment when the method can fail transiently.

```go
// XxxClient defines the interface for ... operations.
type XxxClient interface {
    // DoSomething does X. Returns ErrorTypeUnavailable if the service is temporarily unreachable.
    DoSomething(ctx context.Context, id string) (*XxxResult, error)
}
```

If the interface depends on new domain types, add them in `internal/domain/` (not in `pkg/models/itx/`).

### Step 2 — Implement in `internal/infrastructure/`

Create `internal/infrastructure/xxx/client.go` (new package) or add to an existing infrastructure package if appropriate.

The implementation may import external packages (NATS, HTTP clients, etc.) freely.

Return `domain.NewInternalError(...)` or `domain.ErrorTypeUnavailable` errors — not raw external errors. This keeps callers independent of the infrastructure package's error types.

### Step 3 — Create a no-op implementation

Create `internal/infrastructure/xxx/noop_xxx.go`:

```go
// NoOpXxxClient is a no-op implementation used when xxx is disabled for local development.
type NoOpXxxClient struct{}

func NewNoOpXxxClient() *NoOpXxxClient { return &NoOpXxxClient{} }

func (c *NoOpXxxClient) DoSomething(ctx context.Context, id string) (*domain.XxxResult, error) {
    return nil, domain.NewNotFoundError("xxx disabled (ID_XXX_DISABLED=true)")
}
```

Decide the no-op behavior: return a sensible default (for optional enrichment) or a `NotFound` / `Unavailable` error (for required functionality). Match what callers expect when the service isn't running locally.

### Step 4 — Add an enable/disable env flag

In `cmd/voting-api/main.go`, follow the `ID_MAPPING_DISABLED` pattern:

```go
var xxxClient domain.XxxClient
if os.Getenv("XXX_DISABLED") == "true" {
    xxxClient = xxx.NewNoOpXxxClient()
    logger.Info("xxx client disabled (XXX_DISABLED=true), using no-op")
} else {
    var err error
    xxxClient, err = xxx.NewClient(cfg.XxxURL, logger)
    if err != nil {
        logger.Error("failed to initialize xxx client", "error", err)
        os.Exit(1)
    }
}
```

### Step 5 — Wire into services that need it

Pass `xxxClient` to the service or event processor that needs it. Services must accept it as the domain interface type, not the concrete infrastructure type.

### Step 6 — Document the env var

Add the new variable to:

1. `.env.example` — with a safe local-dev default (typically `XXX_DISABLED=true`) and a descriptive comment
2. `README.md` — add a row to the configuration table

### Step 7 — Validate

```bash
make ci
```

Verify:
- `internal/domain/xxx.go` has no external imports (`go vet` / `make lint`)
- Service compiles when the no-op is selected
- Service compiles when the real implementation is selected
- Local dev works with the disable flag set (`make run` with `XXX_DISABLED=true`)

---

## Things that commonly go wrong

- **Importing the infrastructure type in a service** — the service constructor should accept `domain.XxxClient`, not `*xxx.Client`. If you find yourself importing `internal/infrastructure/xxx` from `internal/service/`, stop and restructure.
- **No-op that panics or logs at error level** — the no-op should behave gracefully; it's the expected local-dev path. Log at `Debug` or `Info`, not `Error`.
- **Missing the env flag** — without a disable flag, local dev without the external service fails at startup. Always provide the escape hatch.
- **Mutable state shared between methods** — domain interfaces should be stateless or thread-safe. If the implementation holds a connection, it must be safe for concurrent use.

---

## Further reading

- `docs/architecture/boundaries.md` — boundary definitions and why they exist
- `internal/domain/errors.go` — error types and their semantics
- `internal/infrastructure/idmapper/` — complete example of interface + real + no-op
