# ITX Proxy Implementation Architecture

This document describes how the ITX proxy endpoints are implemented in the codebase and the architectural patterns used.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Code Organization](#code-organization)
- [Implementation Patterns](#implementation-patterns)
- [Data Flow](#data-flow)
- [Key Components](#key-components)
- [Field Mapping](#field-mapping)
- [Error Handling](#error-handling)
- [Configuration](#configuration)

---

## Overview

The LFX Voting Service acts as a lightweight proxy to the ITX Voting API service, providing:

1. **Authentication Translation** - JWT (Heimdall) → OAuth2 M2M (Auth0)
2. **Authorization** - OpenFGA fine-grained access control
3. **ID Mapping** - V2 UUIDs → V1 Salesforce IDs (via NATS)
4. **Field Mapping** - LFX v2 conventions → ITX conventions
5. **Stateless Proxy** - No local persistence, all data managed by ITX

---

## Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    LFX Voting Service                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              Vote Endpoints                              │   │
│  │              /votes/*  /vote_responses/*                 │   │
│  └────────────────────┬─────────────────────────────────────┘   │
│                       │                                         │
│                       ▼                                         │
│  ┌────────────────────────────────────────────────────────┐     │
│  │           Service Layer (Proxy Logic)                  │     │
│  │  - JWT Authentication via Heimdall                     │     │
│  │  - ID mapping (V2 UIDs ↔ V1 SFIDs via NATS)          │     │
│  │  - Field mapping (committee_uid → committee_id)        │     │
│  │  - Request/response transformation                     │     │
│  └────────────────────┬───────────────────────────────────┘     │
│                       │                                         │
│                       ▼                                         │
│  ┌────────────────────────────────────────────────────────┐     │
│  │         ITX Proxy Client (HTTP Client)                 │     │
│  │  - OAuth2 M2M authentication with Auth0               │     │
│  │  - HTTP requests to ITX service                       │     │
│  │  - Error mapping                                       │     │
│  └────────────────────┬───────────────────────────────────┘     │
│                       │                                         │
└───────────────────────┼─────────────────────────────────────────┘
                        ▼
              ┌──────────────────┐
              │   ITX Service    │
              │  (OAuth2 M2M)    │
              └──────────────────┘
```

---

## Code Organization

### Directory Structure

```
cmd/voting-api/
├── main.go                      # Service entry point
├── api.go                       # Goa handler implementations (votes)
├── api_votes.go                 # Additional vote handler helpers
└── api_vote_responses.go        # Vote response handler implementations

api/voting/v1/design/
└── ...                          # Goa API design (DSL)

internal/
├── domain/
│   ├── auth.go                  # Authentication interface
│   ├── id_mapper.go             # ID mapping interface (v1 ↔ v2)
│   ├── proxy.go                 # ITX proxy client interface
│   └── errors.go                # Domain error types
├── service/
│   ├── vote_service.go          # Vote business logic
│   └── vote_response_service.go # Vote response business logic
└── infrastructure/
    ├── auth/
    │   └── jwt_auth.go          # JWT authentication implementation
    ├── idmapper/
    │   └── nats_mapper.go       # NATS-based ID mapping
    └── proxy/
        └── client.go            # ITX HTTP proxy client

pkg/
├── constants/                   # Shared constants
└── models/itx/
    └── models.go                # ITX request/response models

gen/
└── ...                          # Generated Goa code
```

---

## Implementation Patterns

### API Handler Pattern

**File**: [cmd/voting-api/api.go](../cmd/voting-api/api.go)

```go
// VotingAPI implements the vote service interface
type VotingAPI struct {
    voteService *service.VoteService
}

// CreateVote handles POST /votes
func (api *VotingAPI) CreateVote(ctx context.Context, p *voting.CreateVotePayload) (*voting.VoteResult, error) {
    // Delegate to service layer
    return api.voteService.CreateVote(ctx, p)
}
```

**Pattern**: Thin handler that delegates to service layer

### Service Layer Pattern

**File**: [internal/service/vote_service.go](../internal/service/vote_service.go)

```go
func (s *VoteService) CreateVote(ctx context.Context, req *CreateVoteRequest) (*itx.PollResponse, error) {
    // 1. Extract principal from context (set by JWTAuth middleware)
    principal, ok := ctx.Value(constants.PrincipalContextID).(string)

    // 2. Map v2 UIDs to v1 SFIDs (project, committee, committeeIDs array)
    projectSFID, committeeSFID, committeeIDs, err := s.mapRequestIDsV2ToV1(ctx, req.ProjectUID, req.CommitteeUID, req.CommitteeUIDs)

    // 3. Build ITX request (field mapping: project_uid → project_id)
    proxyReq := &itx.CreatePollRequest{
        ProjectID:   projectSFID,
        CommitteeID: committeeSFID,
        CommitteeIDs: committeeIDs,
        // ... other fields identical
    }

    // 4. Call ITX proxy client
    proxyResp, err := s.proxyClient.CreatePoll(ctx, proxyReq)

    // 5. Map response V1 IDs back to V2 UIDs
    s.mapPollResponseV1ToV2(ctx, proxyResp)

    return proxyResp, nil
}
```

**Pattern**: Service layer handles authentication, ID mapping, field transformation, and error mapping

### Proxy Client Pattern

**File**: [internal/infrastructure/proxy/client.go](../internal/infrastructure/proxy/client.go)

```go
func (c *Client) CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error) {
    // 1. Marshal request to JSON
    body, err := json.Marshal(req)

    // 2. Create HTTP request
    url := fmt.Sprintf("%sv2/voting/poll", c.config.BaseURL)
    httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))

    // 3. Add headers (OAuth2 token added automatically by transport)
    httpReq.Header.Set("Content-Type", "application/json")

    // 4. Execute request
    resp, err := c.httpClient.Do(httpReq)

    // 5. Map HTTP errors to domain errors
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, c.mapHTTPError(resp)
    }

    // 6. Parse response
    var result itx.PollResponse
    json.NewDecoder(resp.Body).Decode(&result)

    return &result, nil
}
```

**Pattern**: HTTP client with automatic OAuth2 authentication and error mapping

---

## Data Flow

### Vote Creation Flow

```
1. Client Request
   POST /votes
   Authorization: Bearer <jwt_token>
   {
     "project_uid": "v2-project-uuid",
     "committee_uid": "v2-committee-uuid",
     "name": "Board Election 2024",
     ...
   }
   ↓
2. Heimdall Authorization
   - Validates JWT
   - Checks OpenFGA: user has "writer" permission on project
   - Adds JWT to context
   ↓
3. API Handler (api.go)
   CreateVote()
   ↓
4. Service Layer (vote_service.go)
   CreateVote()
   ├─→ Extract principal from context
   ├─→ Map v2 project UID to v1 project SFID (via NATS)
   ├─→ Map v2 committee UID to v1 committee identifier (via NATS)
   ├─→ Build ITX request (field mapping)
   └─→ Call proxy client
   ↓
5. Proxy Client (infrastructure/proxy/client.go)
   CreatePoll()
   ├─→ Marshal request to JSON
   ├─→ HTTP POST to ITX service
   ├─→ Add OAuth2 M2M token (automatic via transport)
   └─→ Parse response
   ↓
6. ITX Service
   POST /v2/voting/poll
   Authorization: Bearer <oauth2_m2m_token>
   {
     "project_id": "v1-project-sfid",
     "committee_id": "v1-committee-sfid",
     "name": "Board Election 2024",
     ...
   }
   ↓
7. Response flows back
   ↓
8. Service Layer
   - Maps V1 project/committee SFIDs back to V2 UIDs
   ↓
9. API Response
   201 Created
   {
     "poll_id": "poll-123",
     "project_id": "v2-project-uuid",
     "committee_id": "v2-committee-uuid",
     "name": "Board Election 2024",
     ...
   }
```

---

## Key Components

### 1. Authentication Layer

**Interface**: [internal/domain/auth.go](../internal/domain/auth.go)

```go
type Authenticator interface {
    // ParsePrincipal validates JWT and extracts user info
    ParsePrincipal(ctx context.Context, token string, logger *slog.Logger) (string, error)
}
```

**Implementation**: [internal/infrastructure/auth/jwt_auth.go](../internal/infrastructure/auth/jwt_auth.go)

- Validates JWT using JWKS from Heimdall
- Extracts principal (username) from token
- Supports mock authentication for local development

### 2. ID Mapper Layer

**Interface**: [internal/domain/id_mapper.go](../internal/domain/id_mapper.go)

```go
type IDMapper interface {
    // MapProjectV2ToV1 maps LFX v2 project UID to v1 Salesforce ID
    MapProjectV2ToV1(ctx context.Context, v2UID string) (string, error)

    // MapProjectV1ToV2 maps v1 project SFID to LFX v2 UID
    MapProjectV1ToV2(ctx context.Context, v1SFID string) (string, error)

    // MapCommitteeV2ToV1 maps LFX v2 committee UID to v1 committee identifiers
    MapCommitteeV2ToV1(ctx context.Context, v2UID string) (string, error)

    // MapCommitteeV1ToV2 maps v1 committee SFID to LFX v2 UID
    MapCommitteeV1ToV2(ctx context.Context, v1SFID string) (string, error)
}
```

**Implementation**: [internal/infrastructure/idmapper/nats_mapper.go](../internal/infrastructure/idmapper/nats_mapper.go)

- Uses NATS request/reply pattern
- Can be disabled for local development

### 3. Proxy Client Layer

**Interface**: [internal/domain/proxy.go](../internal/domain/proxy.go)

```go
type PollClient interface {
    CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error)
    GetPoll(ctx context.Context, pollID string) (*itx.PollResponse, error)
    UpdatePoll(ctx context.Context, pollID string, req *itx.UpdatePollRequest) (*itx.PollResponse, error)
    DeletePoll(ctx context.Context, pollID string) error
    ExtendPoll(ctx context.Context, pollID string, req *itx.ExtendPollRequest) (*itx.PollResponse, error)
    EnablePoll(ctx context.Context, pollID string) error
    BulkResendPoll(ctx context.Context, pollID string, req *itx.BulkResendRequest) error
    GetPollResults(ctx context.Context, pollID string) (*itx.VoteResults, error)
}

type VoteResponseClient interface {
    CreateVote(ctx context.Context, req *itx.CreateVoteRequest) error
    GetVote(ctx context.Context, voteID string) (*itx.VoteResponse, error)
    UpdateVote(ctx context.Context, voteID string, req *itx.UpdateVoteRequest) error
    ResendVote(ctx context.Context, voteID string) error
}

type ITXProxyClient interface {
    PollClient
    VoteResponseClient
}
```

**Implementation**: [internal/infrastructure/proxy/client.go](../internal/infrastructure/proxy/client.go)

- HTTP client with OAuth2 M2M authentication
- Automatic token refresh
- Error mapping from HTTP status codes to domain errors

---

## Field Mapping

### Request Field Mapping (Proxy → ITX)

Field differences between Proxy API and ITX API:

| Proxy API (LFX) | ITX API | Notes |
|-----------------|---------|-------|
| `project_uid` | `project_id` | V2 UUID → V1 Salesforce ID (mapped via NATS) |
| `committee_uid` | `committee_id` | V2 UUID → V1 Salesforce ID (mapped via NATS) |
| `committee_uids` (array) | `committee_ids` (array) | Each UID mapped to V1 SFID via NATS |
| All other fields | Same | Identical field names |

**Example**:

```go
// Proxy API request
{
  "project_uid":   "6ba7b810-9dad-11d1-80b4-00c04fd430c8",  // V2 UUID
  "committee_uid": "550e8400-e29b-41d4-a716-446655440000",  // V2 UUID
  "name": "Board Election 2024"
}

// After ID mapping
// ITX API request
{
  "project_id":   "a094V00000A1XyzQAF",  // V1 Salesforce ID
  "committee_id": "a094V00000A1BcdQAF",  // V1 Salesforce ID
  "name": "Board Election 2024"
}
```

### Response Field Mapping (ITX → Proxy)

Response IDs are mapped from V1 to V2:

| ITX API Response | Proxy API Response | Notes |
|-----------------|-------------------|-------|
| `project_id` | `project_id` | V1 Salesforce ID → V2 UUID (mapped via NATS) |
| `committee_id` | `committee_id` | V1 Salesforce ID → V2 UUID (mapped via NATS) |
| All other fields | Same | Identical field names |

**Fallback Strategy**: If V1→V2 mapping fails, the service returns an error rather than returning mismatched IDs, since project and committee references are critical.

### Path Mapping

| Proxy API Endpoint | ITX API Endpoint |
|-------------------|------------------|
| `POST /votes` | `POST /v2/voting/poll` |
| `GET /votes/{id}` | `GET /v2/voting/poll/{id}` |
| `PUT /votes/{id}` | `PUT /v2/voting/poll/{id}` |
| `DELETE /votes/{id}` | `DELETE /v2/voting/poll/{id}` |
| `POST /votes/{id}/extend` | `POST /v2/voting/poll/{id}/extend` |
| `PUT /votes/{id}/enable` | `PUT /v2/voting/poll/{id}/enable` |
| `POST /votes/{id}/bulk_resend` | `POST /v2/voting/poll/{id}/bulk_resend` |
| `GET /votes/{id}/results` | `GET /v2/voting/poll/{id}/results` |
| `POST /vote_responses` | `POST /v2/voting/vote` |
| `GET /vote_responses/{id}` | `GET /v2/voting/vote/{id}` |
| `PUT /vote_responses/{id}` | `PUT /v2/voting/vote/{id}` |
| `POST /vote_responses/{id}/resend` | `POST /v2/voting/vote/{id}/resend` |

**Pattern**: Proxy uses semantic LFX naming (`votes`, `vote_responses`); ITX uses its own naming (`poll`, `vote`)

---

## Error Handling

### Domain Error Types

**File**: [internal/domain/errors.go](../internal/domain/errors.go)

```go
type DomainError struct {
    Type    ErrorType
    Message string
    Err     error
}

// Error constructors
func NewValidationError(message string, err ...error) *DomainError   // 400
func NewNotFoundError(message string, err ...error) *DomainError     // 404
func NewConflictError(message string, err ...error) *DomainError     // 409
func NewInternalError(message string, err ...error) *DomainError     // 500
func NewUnavailableError(message string, err ...error) *DomainError  // 503
```

### HTTP to Domain Error Mapping

**File**: [internal/infrastructure/proxy/client.go](../internal/infrastructure/proxy/client.go)

```go
func (c *Client) mapHTTPError(resp *http.Response) error {
    switch resp.StatusCode {
    case http.StatusBadRequest:
        return domain.NewValidationError(message)
    case http.StatusNotFound:
        return domain.NewNotFoundError(message)
    case http.StatusConflict:
        return domain.NewConflictError(message)
    case http.StatusServiceUnavailable:
        return domain.NewUnavailableError(message)
    default:
        return domain.NewInternalError(message)
    }
}
```

> **Note**: Unauthorized (401) and Forbidden (403) responses from ITX are unexpected since the service uses M2M authentication, and are treated as internal errors.

---

## Configuration

### Environment Variables

**Server Configuration**:

```bash
PORT=8080
LOG_LEVEL=info        # debug, info, warn, error (default: info)
LOG_ADD_SOURCE=false  # Include source file/line in log output (default: false)
```

**Authentication**:

```bash
JWKS_URL=http://heimdall:4457/.well-known/jwks
AUDIENCE=lfx-v2-voting-service
# For local dev only:
JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL=test-user
```

**ITX Integration** (OAuth2 M2M with Auth0):

```bash
ITX_BASE_URL=https://api.dev.itx.linuxfoundation.org/
ITX_AUTH0_DOMAIN=linuxfoundation-dev.auth0.com
ITX_CLIENT_ID=<client-id>
ITX_CLIENT_PRIVATE_KEY=<rsa-private-key-pem>
ITX_AUDIENCE=https://api.dev.itx.linuxfoundation.org/
```

**ID Mapping** (NATS):

```bash
NATS_URL=nats://nats:4222
# For local dev only:
ID_MAPPING_DISABLED=true
```

### Helm Configuration

**File**: [charts/lfx-v2-voting-service/values.yaml](../charts/lfx-v2-voting-service/values.yaml)

```yaml
app:
  environment:
    PORT:
      value: "8080"
    LOG_LEVEL:
      value: info
    ITX_BASE_URL:
      value: https://api.dev.itx.linuxfoundation.org/
    ITX_AUTH0_DOMAIN:
      value: linuxfoundation-dev.auth0.com
    ITX_AUDIENCE:
      value: https://api.dev.itx.linuxfoundation.org/
    NATS_URL:
      value: nats://lfx-platform-nats.lfx.svc.cluster.local:4222
    JWKS_URL:
      value: https://heimdall.dev.lfx.linuxfoundation.org/.well-known/jwks.json
    AUDIENCE:
      value: lfx-v2-voting-service

  # Secrets loaded from AWS Secrets Manager via External Secrets Operator
  secrets:
    - name: ITX_CLIENT_ID
      path: /cloudops/managed-secrets/auth0/LFX_V2_Voting_Service
      key: client_id
    - name: ITX_CLIENT_PRIVATE_KEY
      path: /cloudops/managed-secrets/auth0/LFX_V2_Voting_Service
      key: client_private_key
```

---

## Authorization

### Heimdall RuleSet

**File**: [charts/lfx-v2-voting-service/templates/ruleset.yaml](../charts/lfx-v2-voting-service/templates/ruleset.yaml)

Authorization is handled by Heimdall with OpenFGA checks:

```yaml
- id: "rule:lfx:lfx-v2-voting-service:votes:create"
  match:
    methods: [POST]
    routes:
      - path: /votes
  execute:
    - authenticator: oidc
    - authorizer: openfga_check
      config:
        values:
          relation: writer
          object: "project:{{- .Request.Body.project_uid -}}"
    - finalizer: create_jwt
```

**Permission Model**:

- `writer` - Can create, update, delete, extend, enable, and bulk-resend votes
- `viewer` - Can read vote details
- `results_viewer` - Can view aggregated vote results
- `auditor` - Can view vote response details
- `owner` - Can submit and update their own vote responses

---

## Testing Strategy

### Unit Tests

1. **Service Layer Tests** (with mock proxy client)
   - Test JWT principal extraction
   - Test ID mapping logic (V2 → V1 and V1 → V2)
   - Test error propagation
   - Test field mapping

2. **Proxy Client Tests** (with mock HTTP server)
   - Test HTTP request construction
   - Test OAuth2 token addition
   - Test error mapping from HTTP status codes
   - Test response parsing

### Example Test

```go
func TestCreateVote(t *testing.T) {
    // Setup mocks
    mockProxy := &MockPollClient{}
    mockIDMapper := &MockIDMapper{}
    mockAuth := &MockAuthenticator{}

    svc := NewVoteService(mockAuth, mockProxy, mockIDMapper, logger)

    // Mock ID mapping: v2 UUID → v1 SFID
    mockIDMapper.On("MapProjectV2ToV1", mock.Anything, "v2-project-uuid").
        Return("v1-project-sfid", nil)
    mockIDMapper.On("MapCommitteeV2ToV1", mock.Anything, "v2-committee-uuid").
        Return("v1-committee-sfid", nil)

    // Mock ITX response
    mockProxy.On("CreatePoll", mock.Anything, mock.MatchedBy(func(req *itx.CreatePollRequest) bool {
        return req.ProjectID == "v1-project-sfid" && req.Name == "Board Election 2024"
    })).Return(&itx.PollResponse{
        PollID:      "poll-123",
        ProjectID:   "v1-project-sfid",
        CommitteeID: "v1-committee-sfid",
        Name:        "Board Election 2024",
    }, nil)

    // Mock reverse mapping: v1 SFID → v2 UUID
    mockIDMapper.On("MapProjectV1ToV2", mock.Anything, "v1-project-sfid").
        Return("v2-project-uuid", nil)
    mockIDMapper.On("MapCommitteeV1ToV2", mock.Anything, "v1-committee-sfid").
        Return("v2-committee-uuid", nil)

    // Execute
    result, err := svc.CreateVote(ctx, &CreateVoteRequest{
        ProjectUID:   "v2-project-uuid",
        CommitteeUID: "v2-committee-uuid",
        Name:         "Board Election 2024",
    })

    // Verify
    assert.NoError(t, err)
    assert.Equal(t, "poll-123", result.PollID)
    assert.Equal(t, "v2-project-uuid", result.ProjectID)
    mockProxy.AssertExpectations(t)
    mockIDMapper.AssertExpectations(t)
}
```

---

## Summary

### Architecture Characteristics

| Characteristic | Implementation |
|---------------|----------------|
| **Type** | Stateless HTTP proxy |
| **Storage** | None (all data in ITX) |
| **Authentication** | JWT (Heimdall) → OAuth2 M2M (Auth0) |
| **Authorization** | OpenFGA via Heimdall |
| **Field Mapping** | project_uid/committee_uid ↔ project_id/committee_id |
| **ID Mapping** | V2 UUID ↔ V1 Salesforce ID (via NATS) |
| **Business Logic** | Thin proxy layer |
| **Code Size** | ~5000 LOC (including event processing) |

### Key Design Decisions

1. **Stateless Proxy**: No local persistence simplifies deployment and scaling
2. **ID Mapping on Both Sides**: Project and committee IDs mapped on request and response
3. **Automatic OAuth2**: Transport layer handles token acquisition and refresh
4. **Domain Error Pattern**: Consistent error handling across all layers
5. **Clean Architecture**: Clear separation between API, service, and infrastructure layers
6. **Goa Framework**: Type-safe API definitions with generated code

### Benefits

- **Simple Integration**: Thin proxy reduces complexity
- **No State Management**: ITX handles all vote lifecycle
- **Centralized Voting Access**: ITX manages credentials and voting API complexity
- **Fast Implementation**: Minimal business logic required
- **Easy Testing**: Mock proxy client for unit tests
- **Scalable**: Stateless design allows horizontal scaling
