# Skill: write-proxy-service-tests

## Purpose

Guide writing tests for the currently untested HTTP proxy path — the service layer (`internal/service/`), converter layer (`cmd/voting-api/service/`), and proxy client (`internal/infrastructure/proxy/`) — using the established mock patterns in this repository.

## When to use

- Adding tests for any new service method
- Closing the known testing gap (no tests exist yet for `internal/service/` or `internal/infrastructure/proxy/`)
- Writing tests for a new converter in `cmd/voting-api/service/`

---

## Context you need first

- `tmp/refactoring-suggestions.md` — identifies the exact gaps and the highest-value test targets
- `internal/service/vote_service_test.go` — the only existing service test; use as style reference
- `cmd/voting-api/eventing/*_test.go` — richer mock patterns (table-driven, mock interfaces, mock KV); the best style reference even though it tests a different layer
- `internal/infrastructure/idmapper/noop_mapper.go` — ready-made `IDMapper` no-op for happy-path tests

---

## What to mock

The service layer depends on three domain interfaces. Mock them inline for each test file:

```go
// internal/service/vote_service_test.go (or a new _test.go file)

type mockPollClient struct {
    createPollFn func(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error)
    // add other methods as needed; return nil/nil for methods not under test
}

func (m *mockPollClient) CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error) {
    if m.createPollFn != nil {
        return m.createPollFn(ctx, req)
    }
    return nil, nil
}
// implement remaining interface methods with zero-value returns
```

For `domain.IDMapper`, prefer `idmapper.NewNoOpMapper()` for happy-path tests. For error-path tests, implement a minimal failing mock:

```go
type failingIDMapper struct{ err error }
func (m *failingIDMapper) MapProjectV1ToV2(ctx context.Context, id string) (string, error) { return "", m.err }
// ... other methods
```

For `domain.Authenticator`, implement `ParsePrincipal` inline:
```go
type mockAuthenticator struct{ principal string; err error }
func (m *mockAuthenticator) ParsePrincipal(_ context.Context, _ string, _ *slog.Logger) (string, error) {
    return m.principal, m.err
}
```

---

## Workflow

### Service layer tests (`internal/service/`)

Write table-driven tests for each service method. Each test case should specify:
- Input request
- Mock behavior (what the poll client returns / what the ID mapper returns)
- Expected output or expected error type

Key scenarios to cover for every service method:
1. Happy path — verify the correct ITX request is built and the response is returned
2. Missing principal — call without a principal in context, expect `ErrorTypeValidation`
3. ID mapping failure — mock IDMapper returning an error, verify propagation
4. Proxy client failure — mock PollClient returning each possible error type, verify the correct `domain.ErrorType` is returned

```go
func TestVoteService_CreateVote(t *testing.T) {
    tests := []struct {
        name        string
        req         *CreateVoteRequest
        setupClient func(*mockPollClient)
        setupMapper func() domain.IDMapper
        wantErr     domain.ErrorType
    }{
        {
            name: "happy path",
            req:  &CreateVoteRequest{Name: "Test Vote", ProjectUID: "proj-uid"},
            setupClient: func(m *mockPollClient) {
                m.createPollFn = func(_ context.Context, _ *itx.CreatePollRequest) (*itx.PollResponse, error) {
                    return &itx.PollResponse{PollID: "poll-123"}, nil
                }
            },
            setupMapper: func() domain.IDMapper { return idmapper.NewNoOpMapper() },
        },
        // ...
    }
}
```

### Converter tests (`cmd/voting-api/service/`)

Test converters independently of the service. Focus on field renames — these are the most likely to regress:

```go
func TestConvertCreateVotePayloadToDomain(t *testing.T) {
    payload := &votesvc.CreateVotePayload{
        Name:         "Test",
        ProjectUID:   "proj-uid",
        CommitteeUID: "comm-uid",
        // ... set all fields
    }
    result := ConvertCreateVotePayloadToDomain(payload)
    assert.Equal(t, payload.Name, result.Name)
    assert.Equal(t, payload.ProjectUID, result.ProjectUID)
    // verify every field maps correctly
}
```

Cover both directions: payload → service request AND service response (ITX response) → Goa result. The ITX → Goa conversions are especially prone to field-name bugs (`PollID` → `UID`, `ProjectID` → `ProjectUID`).

### Proxy client tests (`internal/infrastructure/proxy/`)

These require an HTTP test server. Use `net/http/httptest`:

```go
func TestProxyClient_CreatePoll(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // assert request shape
        assert.Equal(t, "POST", r.Method)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(&itx.PollResponse{PollID: "poll-123"})
    }))
    defer ts.Close()
    // construct client pointing at ts.URL
    // call method, assert response
}
```

Cover the error mapping: return a 404 from the mock server and verify the client returns `domain.ErrorTypeNotFound`.

---

## Rules for all tests in this repository

1. **No live external connections** — mock all ITX, NATS, and Heimdall dependencies.
2. **Table-driven** — use `t.Run` with a `tests` slice for any method with more than two cases.
3. **Race-safe** — the test suite runs with `-race`; don't share state across goroutines without synchronization.
4. **License header** — new test files need the copyright header (CI enforces this).

---

## Validate

```bash
make test
# or for a specific package:
go test ./internal/service/... -v -race
go test ./cmd/voting-api/service/... -v -race
go test ./internal/infrastructure/proxy/... -v -race
```

`make ci` must exit 0 before marking the work done.

---

## Further reading

- `tmp/refactoring-suggestions.md` — prioritized list of test gaps
- `cmd/voting-api/eventing/vote_event_handler_test.go` — best mock pattern reference
- `internal/domain/errors.go` — error types to assert in error-path tests
