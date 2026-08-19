# Data Flow — LFX Voting Service

This document describes the major flows through the system. Only flows supported by the
implementation are documented here.

---

## Flow 1 — Synchronous HTTP API (Vote CRUD)

This flow covers all vote (poll) management operations: create, get, update, delete, extend,
enable, bulk-resend, and get-results.

```mermaid
sequenceDiagram
    participant Client as LFXv2 Client
    participant Heimdall as Heimdall (JWKS)
    participant API as HTTP API (Goa/chi)
    participant Conv as Type Converters<br/>(cmd/voting-api/service/)
    participant Svc as VoteService
    participant IDMap as ID Mapper (NATS)
    participant Proxy as ITX Proxy Client
    participant Auth0 as Auth0
    participant ITX as ITX Voting API

    Client->>API: HTTP request + JWT Bearer token
    API->>Heimdall: validate JWKS signature (cached)
    Heimdall-->>API: public key
    API->>API: extract principal (LFX username) → store in context
    API->>Conv: Goa payload → internal request type
    Conv-->>API: CreateVoteRequest / UpdateVoteRequest / etc.
    API->>Svc: delegate (CreateVote / GetVote / etc.)
    Svc->>IDMap: MapProjectV2ToV1(projectUID)
    IDMap->>IDMap: NATS request/reply lfx.lookup_v1_mapping
    IDMap-->>Svc: v1 projectSFID
    Svc->>IDMap: MapCommitteeV2ToV1(committeeUID)
    IDMap-->>Svc: v1 committeeSFID
    Svc->>Proxy: CreatePoll(itx.CreatePollRequest)
    Proxy->>Auth0: client-credentials token (cached)
    Auth0-->>Proxy: M2M access token
    Proxy->>ITX: POST /v2/voting/poll (+ Bearer token)
    ITX-->>Proxy: itx.PollResponse (v1 IDs)
    Proxy-->>Svc: PollResponse
    Svc->>IDMap: MapProjectV1ToV2(projectSFID)
    IDMap-->>Svc: v2 projectUID
    Svc->>IDMap: MapCommitteeV1ToV2(committeeSFID)
    IDMap-->>Svc: v2 committeeUID
    Svc-->>API: PollResponse (v2 IDs)
    API->>Conv: PollResponse → Goa VoteResult
    Conv-->>API: VoteResult
    API-->>Client: HTTP 201/200 + JSON body
```

**Evidence:** `internal/service/vote_service.go`, `internal/infrastructure/proxy/client.go`,
`cmd/voting-api/api_votes.go`, `cmd/voting-api/service/vote_converters.go`

---

## Flow 2 — Vote Response (Ballot Submission)

This covers `POST /vote_responses` and `PUT /vote_responses/{id}`. The flow is the same shape as
Flow 1 but goes through `VoteResponseService` and calls ITX's `/v2/voting/vote` endpoints.

ID mapping applies to `project_uid` only (no committee mapping for vote responses).

**Evidence:** `internal/service/vote_response_service.go`, `cmd/voting-api/api_vote_responses.go`

---

## Flow 3 — JWT Authentication

JWT validation is delegated to the `JWTAuth` method on the service, which is called by the
Goa-generated dispatcher before every protected endpoint.

```mermaid
sequenceDiagram
    participant Goa as Goa Dispatcher (gen/)
    participant API as VotingAPI.JWTAuth (api.go)
    participant Svc as VoteService.JWTAuth
    participant Auth as JWTAuth (infrastructure/auth)
    participant Heimdall as Heimdall JWKS

    Goa->>API: JWTAuth(ctx, bearerToken, scheme)
    API->>Svc: JWTAuth(ctx, bearerToken, scheme)
    Svc->>Auth: ParsePrincipal(ctx, token)
    Auth->>Heimdall: fetch JWKS (cached by go-jwt-middleware)
    Heimdall-->>Auth: public keys
    Auth-->>Svc: principal string (LFX username)
    Svc->>Svc: store principal in context
    Svc-->>API: enriched context
    API-->>Goa: enriched context (handler proceeds)
```

**Local dev bypass:** When `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL` is set, `ParsePrincipal`
returns that env-var value without contacting Heimdall.

**Evidence:** `internal/infrastructure/auth/jwt.go`, `internal/service/vote_service.go:JWTAuth()`

---

## Flow 4 — NATS KV Event Processing (v1 → v2 Sync)

This is the asynchronous path. It runs entirely independently of the HTTP API path.

```mermaid
sequenceDiagram
    participant V1KV as NATS KV: v1-objects
    participant Proc as EventProcessor
    participant Router as KvHandler (router)
    participant Handler as Entity Handler<br/>(vote / vote_response / vote_result)
    participant IDMap as ID Mapper (NATS)
    participant Mappings as NATS KV: v1-mappings
    participant Pub as NATSPublisher
    participant Indexer as indexer-service
    participant FGA as fga-sync

    V1KV->>Proc: JetStream message (KV PUT or DEL)
    Proc->>Router: kvMessageHandler(msg)
    Router->>Router: route by key prefix<br/>(itx-poll.* / itx-poll-vote.* / itx-poll-results.*)
    Router->>Handler: handleVoteUpdate / handleVoteDelete / etc.
    Handler->>Handler: decode payload (JSON or msgpack)
    Handler->>Handler: unmarshal into PollDBRaw / VoteDBRaw / PollResultDBRaw<br/>(custom UnmarshalJSON: coerce string→int)
    Handler->>Mappings: get v1-mappings key → determine create vs update
    Handler->>IDMap: MapProjectV1ToV2(projectSFID)
    IDMap-->>Handler: v2 projectUID
    Handler->>IDMap: MapCommitteeV1ToV2(committeeSFID)
    IDMap-->>Handler: v2 committeeUID
    Handler->>Handler: build VoteData / VoteResponseData / PollResultData
    Handler->>Pub: PublishVoteEvent(action, voteData)
    Pub->>Indexer: NATS publish lfx.index.vote (IndexerMessageEnvelope)
    Pub->>FGA: NATS publish lfx.fga-sync.update_access (GenericFGAMessage)
    Handler->>Mappings: store tracking key (value "1" or "!del")
    Handler-->>Proc: Ack (success) or Nak (transient error)
```

**Error classification:** `domain.ErrorTypeInternal` and `ErrorTypeUnavailable` → NAK (retry up
to 3×). Invalid data, missing fields, malformed JSON → ACK (skip permanently).

**Evidence:** `cmd/voting-api/eventing/kv_handler.go`, `vote_event_handler.go`,
`vote_response_event_handler.go`, `vote_result_event_handler.go`,
`internal/infrastructure/eventing/nats_publisher.go`, `docs/event-processing.md`

---

## Flow 5 — Invite Sending (optional, INVITES_ENABLED=true)

This flow runs inside the vote-response event handler after the response is published.

```mermaid
sequenceDiagram
    participant Handler as VoteResponseInviteHandler
    participant Mappings as NATS KV: v1-mappings
    participant V1KV as NATS KV: v1-objects
    participant AuthSvc as auth-service (NATS)
    participant InviteSvc as invite-service (NATS)

    Handler->>Handler: check if username present → skip if yes
    Handler->>Mappings: check invite_sent marker → skip if already sent
    Handler->>AuthSvc: lfx.auth-service.email_to_username (email lookup)
    AuthSvc-->>Handler: username or "not found"
    Handler->>Handler: skip if user already has LFID
    Handler->>V1KV: get itx-poll.{poll_id} → read vote name
    Handler->>InviteSvc: lfx.invite-service.send_invite
    Handler->>Mappings: store invite_sent marker (best-effort)
```

All invite steps are best-effort. Errors are logged but do not cause the parent KV message to be
NAK'd.

**Evidence:** `cmd/voting-api/eventing/vote_response_invite.go`,
`cmd/voting-api/eventing/invite_config.go`, `docs/lfid-invites.md`

---

## Flow 6 — Invite Acceptance Enrichment (optional, INVITES_ENABLED=true)

This runs on a separate NATS subscriber, independent of the KV event processor.

```mermaid
sequenceDiagram
    participant InviteSvc as invite-service (NATS)
    participant Sub as InviteAcceptedSubscriber
    participant Proxy as ITX Proxy Client
    participant ITX as ITX Voting API

    InviteSvc->>Sub: lfx.invite-service.invite_accepted (queue group)
    Sub->>Proxy: AcceptInvite(ctx, email, username)
    Proxy->>ITX: POST /v2/voting/vote/invite_accepted
    ITX-->>Proxy: 2xx
```

**Evidence:** `cmd/voting-api/eventing/invite_accepted_subscriber.go`,
`internal/infrastructure/proxy/client.go:AcceptInvite()`

---

## Flow 7 — ITX OAuth2 M2M Token Acquisition

The proxy client uses a cached, automatically-renewed token. This flow happens transparently on
the first ITX call and on every token expiry.

```mermaid
sequenceDiagram
    participant Proxy as ITX Proxy Client
    participant TokenSrc as auth0TokenSource
    participant Reuse as oauth2.ReuseTokenSource
    participant Auth0 as Auth0

    Proxy->>Reuse: get token for ITX request
    alt token is valid (not expired with 60s leeway)
        Reuse-->>Proxy: cached token
    else token expired
        Reuse->>TokenSrc: Token()
        TokenSrc->>Auth0: LoginWithClientCredentials (private-key JWT assertion)
        Auth0-->>TokenSrc: access token + expires_in
        TokenSrc-->>Reuse: oauth2.Token (expiry - 60s leeway)
        Reuse-->>Proxy: fresh token
    end
    Proxy->>Proxy: attach as Authorization: Bearer <token>
```

**Evidence:** `internal/infrastructure/proxy/client.go:NewClient()`, `auth0TokenSource.Token()`

---

## Documentation Gaps

- The exact retry and timeout behaviour of the NATS request/reply call inside `IDMapper` under
  prolonged NATS unavailability is not tested or documented.
- There is no documented flow for what happens when the ITX API returns a non-standard error body
  that doesn't contain `message` or `error` JSON fields.
- The `v1-mappings` KV bucket is written by this service but may also be written by other services.
  The full lifecycle of this bucket is not documented here.
