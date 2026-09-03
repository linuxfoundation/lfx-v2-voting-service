# Dependencies — LFX Voting Service

This document covers the architecturally significant dependencies. It does not reproduce `go.mod`
or Helm chart values; it focuses on what each dependency means for the system design.

---

## External APIs

### ITX Voting API

| | |
|---|---|
| **Purpose** | Authoritative data store for all polls (votes) and vote responses (ballots) |
| **Used by** | `internal/infrastructure/proxy/client.go` |
| **Protocol** | HTTPS REST |
| **Authentication** | OAuth2 M2M — private-key JWT client assertion via Auth0, then Bearer token |
| **Base URL env var** | `ITX_BASE_URL` (default: `https://api.dev.itx.linuxfoundation.org/`) |
| **Scope header** | All requests include `x-scope: manage:voting` |
| **API version** | All paths use `/v2/voting/...` |
| **Architectural implication** | This service owns no persistent storage. All poll and vote-response data lives exclusively in ITX. If ITX is unavailable, all synchronous API calls fail. |

### Auth0

| | |
|---|---|
| **Purpose** | Issues the M2M access token used to authenticate against ITX |
| **Used by** | `internal/infrastructure/proxy/client.go` (via `auth0/go-auth0` SDK) |
| **Protocol** | HTTPS (OAuth2 client-credentials + private-key JWT assertion) |
| **Config env vars** | `ITX_AUTH0_DOMAIN`, `ITX_CLIENT_ID`, `ITX_CLIENT_PRIVATE_KEY` (RSA PEM), `ITX_AUDIENCE` |
| **Token caching** | `oauth2.ReuseTokenSource` with 60-second expiry leeway |
| **Architectural implication** | `ITX_CLIENT_PRIVATE_KEY` must be set at startup; `proxy.NewClient` panics otherwise. In prod this secret is provided by External Secrets Operator from AWS Secrets Manager. |

---

## Authentication / Identity

### Heimdall (JWKS endpoint)

| | |
|---|---|
| **Purpose** | Provides public keys for validating inbound JWTs issued to LFXv2 clients |
| **Used by** | `internal/infrastructure/auth/jwt.go` |
| **Protocol** | HTTPS (JWKS — `/.well-known/jwks`) |
| **Config env var** | `JWKS_URL` (default: `http://heimdall:4457/.well-known/jwks`) |
| **Local dev bypass** | `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL=<any string>` skips validation entirely |
| **Architectural implication** | Heimdall is the gateway for all inbound authentication. The service does not issue tokens — it only validates them. |

---

## Message Brokers

### NATS JetStream

NATS is used for four distinct purposes, each with different interaction patterns:

#### 4a. v1 KV event sync (inbound, JetStream consumer)

| | |
|---|---|
| **Purpose** | Receives change events from the legacy v1 DynamoDB (mirrored via Meltano) |
| **Used by** | `cmd/voting-api/eventing/event_processor.go` |
| **Stream** | `KV_v1-objects` (env var `EVENT_STREAM_NAME`) |
| **Consumer** | Durable, `DeliverLastPerSubjectPolicy`, filter on `$KV.v1-objects.itx-poll.>`, `itx-poll-vote.>`, `itx-poll-results.>` |
| **KV bucket** | `v1-objects` |
| **Config env vars** | `NATS_URL`, `EVENT_PROCESSING_ENABLED`, `EVENT_CONSUMER_NAME`, `EVENT_STREAM_NAME` |
| **Controlled by** | `EventProcessor` — starts when `EVENT_PROCESSING_ENABLED=true` |

#### 4b. ID mapping (request/reply)

| | |
|---|---|
| **Purpose** | Translates v2 UUIDs ↔ v1 SFIDs for projects and committees |
| **Used by** | `internal/infrastructure/idmapper/nats_mapper.go` |
| **Subject** | `lfx.lookup_v1_mapping` |
| **KV bucket** | `v1-mappings` (also used for deduplication tracking) |
| **Pattern** | Request/reply with 5-second timeout |
| **Config env vars** | `NATS_URL`, `ID_MAPPING_DISABLED` |
| **Architectural implication** | Used on synchronous HTTP requests that translate project or committee UIDs. When NATS mapping is unavailable (and not disabled), the following calls fail: `CreateVote`, `GetVote`, `UpdateVote`, `ExtendVote`, `CreateVoteResponse`, `GetVoteResponse`, `UpdateVoteResponse`. Operations that pass a vote ID directly to ITX without UID translation (`DeleteVote`, `EnableVote`, `BulkResendVote`, `GetVoteResults`) continue to work. |

#### 4c. Downstream event publishing (fire-and-forget pub/sub)

| | |
|---|---|
| **Purpose** | Forwards transformed v2 data to indexer-service and fga-sync |
| **Used by** | `internal/infrastructure/eventing/nats_publisher.go` |
| **Subjects** | `lfx.index.vote`, `lfx.index.vote_response`, `lfx.index.vote_result` (indexer); `lfx.fga-sync.update_access`, `lfx.fga-sync.delete_access` (FGA) |
| **Pattern** | Fire-and-forget; no response expected |
| **OTel** | Trace context is injected into NATS message headers via W3C TraceContext propagation |

#### 4d. Invite / user lookup (request/reply, optional)

| | |
|---|---|
| **Purpose** | Sends LFID invites; looks up usernames from email addresses |
| **Used by** | `internal/infrastructure/nats/invite_sender.go`, `user_reader.go` |
| **Subjects** | `lfx.invite-service.send_invite`, `lfx.auth-service.email_to_username`, `lfx.invite-service.invite_accepted` (subscribe) |
| **Config** | `INVITES_ENABLED`, `NATS_URL` |

---

## Internal Platform Services

### indexer-service

| | |
|---|---|
| **Purpose** | Indexes vote and vote-response data into OpenSearch for platform search |
| **Used by** | `internal/infrastructure/eventing/nats_publisher.go` |
| **Protocol** | NATS pub/sub |
| **Message format** | `indexerTypes.IndexerMessageEnvelope` (from `lfx-v2-indexer-service` Go module) |
| **Architectural implication** | This service publishes a contract defined in an external Go module (`github.com/linuxfoundation/lfx-v2-indexer-service`). Changes to that module's types require a `go get` update here. |

### fga-sync

| | |
|---|---|
| **Purpose** | Writes and removes OpenFGA relationship tuples that enforce access control |
| **Used by** | `internal/infrastructure/eventing/nats_publisher.go` |
| **Protocol** | NATS pub/sub |
| **Message format** | `fgatypes.GenericFGAMessage` (from `lfx-v2-fga-sync` Go module) |
| **Contract doc** | [`../fga-contract.md`](../fga-contract.md) |
| **Architectural implication** | This service does not perform access checks — it only publishes FGA messages. The actual authorization check at request time is done by Heimdall/the API gateway layer before the request reaches this service. |

### invite-service

| | |
|---|---|
| **Purpose** | Sends email invites to vote participants who do not yet have an LFX account |
| **Used by** | `internal/infrastructure/nats/invite_sender.go` |
| **Protocol** | NATS request/reply |
| **Conditional** | Only active when `INVITES_ENABLED=true` and `LFX_SELF_SERVE_BASE_URL` is valid |

### auth-service

| | |
|---|---|
| **Purpose** | Resolves LFX usernames from email addresses |
| **Used by** | `internal/infrastructure/nats/user_reader.go` |
| **Protocol** | NATS request/reply (`lfx.auth-service.email_to_username`) |
| **Conditional** | Only called during invite flow when `INVITES_ENABLED=true` |

---

## Infrastructure / Platform

### Kubernetes + Helm

The service runs in the `lfx` Kubernetes namespace. Deployment manifests live in
`charts/lfx-v2-voting-service/`. Key infrastructure resources:

- **HTTPRoute** — Gateway API route exposing the service via the cluster's HTTP gateway
- **Heimdall RuleSet** — declares which routes require JWT validation
- **PodDisruptionBudget** — prevents all replicas being unavailable during rollouts
- **ServiceAccount** with IRSA annotations — allows the pod to access AWS Secrets Manager via ESO

### External Secrets Operator (ESO) + AWS Secrets Manager

| | |
|---|---|
| **Purpose** | Provisions `ITX_CLIENT_ID` and `ITX_CLIENT_PRIVATE_KEY` as Kubernetes secrets |
| **Used by** | Deployment (`deployment.yaml` reads them as env vars via `secretKeyRef`) |
| **Config** | `charts/.../externalsecret.yaml`, `secretstore.yaml` |
| **Architectural implication** | These secrets do not exist in the Helm chart values. They are pulled at deploy time. Without ESO running and the secrets present in AWS, the pod will not start. |

### ko (container image builder)

| | |
|---|---|
| **Purpose** | Builds and publishes OCI images to GHCR without a Dockerfile |
| **Used by** | CI workflows `ko-build-branch.yaml`, `ko-build-main.yaml`, `ko-build-tag.yaml` |
| **Note** | A `Dockerfile` exists for local `make docker-build` but is not used in CI |

### OpenTelemetry

| | |
|---|---|
| **Purpose** | Distributed tracing, metrics, and log correlation |
| **Used by** | `pkg/utils/otel.go` (SDK setup), `otelhttp` (HTTP middleware + outbound transport), `slog-otel` (log→trace bridge), `nats_publisher.go` (trace context propagation in NATS headers) |
| **Config** | Standard OpenTelemetry env vars (`OTEL_*`) plus `OTEL_SERVICE_VERSION` |

---

## Go Module Dependencies (Architecturally Significant)

| Module | Role |
|---|---|
| `goa.design/goa/v3` | API framework — DSL, code generation, HTTP server/client |
| `github.com/go-chi/chi/v5` | HTTP router (used as Goa muxer) |
| `github.com/auth0/go-auth0` | Auth0 SDK for M2M client-credential token acquisition |
| `github.com/auth0/go-jwt-middleware/v2` | JWT validation against JWKS endpoint |
| `github.com/nats-io/nats.go` | NATS client (core pub/sub, JetStream, KV) |
| `github.com/nats-io/nats-server/v2` | Embedded NATS server used in tests only |
| `github.com/linuxfoundation/lfx-v2-fga-sync` | FGA message types + subject constants |
| `github.com/linuxfoundation/lfx-v2-indexer-service` | Indexer message types + subject constants |
| `github.com/linuxfoundation/lfx-v2-invite-service` | Invite message types |
| `github.com/vmihailenco/msgpack/v5` | msgpack decoding for KV events from Meltano batch path |

---

## Documentation Gaps

- The exact ownership and schema of the `v1-mappings` NATS KV bucket (which services write to it
  besides this one, and what the full key namespace is) is not documented in this repo.
- The Meltano pipeline that mirrors v1 DynamoDB data into the `v1-objects` KV bucket is an
  external system not documented here. Its behavior explains why numeric fields may arrive as
  strings (DynamoDB Decimal → Meltano string serialization).
- The `OTEL_*` environment variable contract (exporter endpoint, service name, etc.) is not
  documented in `.env.example` or `README.md`.
