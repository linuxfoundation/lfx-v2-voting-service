# LFID Invite Flow

## Overview

When a participant casts a vote in the ITX system without an LFX account (LFID), the voting service sends them an LFID invite so their vote-response record can later be enriched with their platform identity. Once the participant accepts, the service calls ITX to backfill their username and profile data across all vote-response records associated with their email address.

This feature mirrors the pattern used by the meeting service ([LFXV2-1831](https://linuxfoundation.atlassian.net/browse/LFXV2-1831)) and is gated behind the `INVITES_ENABLED` flag.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `INVITES_ENABLED` | `false` | Gates both outbound invite sending and the `invite_accepted` enrichment subscriber. Set to `true`, `1`, or `yes` to enable. |
| `LFX_SELF_SERVE_BASE_URL` | derived from `LFX_ENVIRONMENT` | Base URL embedded in invite emails as `return_url`. Derived automatically from `LFX_ENVIRONMENT` (`prod` → `https://app.lfx.dev`, `staging` → `https://app.staging.lfx.dev`, otherwise `https://app.dev.lfx.dev`). Override with an explicit URL when needed. |

## Two Independent Paths

### Path 1 — Outbound invite (during KV event processing)

Triggered when `handleVoteResponseUpdate` processes an `itx-poll-vote.*` KV entry for the first time (i.e. `ActionCreated`) and the vote response has no username but does have an email address.

```
v1-objects KV (itx-poll-vote.*)
        │
        ▼
handleVoteResponseUpdate
  [index + FGA sync]
        │
        ▼
shouldSendVoteResponseInvite?
  • ActionCreated
  • username == ""
  • email != ""
        │ yes
        ▼
VoteResponseInviteHandler.maybeSendInvite
  1. Check invite-sent marker  ──── already sent? → skip
     (v1-mappings: v1_vote_response_lfid_invite_sent.{uid})
  2. UsernameByEmail via NATS  ──── has LFID? → skip
     (lfx.auth-service.email_to_username)          transient error? → proceed (best-effort)
  3. Resolve vote name from KV ──── not found? → skip
     (v1-objects: itx-poll.{poll_id})
  4. Write "pending" invite marker
  5. lfx.invite-service.send_invite
     • resource.type = vote
     • role = Voter
     • return_url = {LFX_SELF_SERVE_BASE_URL}/votes/{poll_id}
     • expiration_days = 30
  6. Update marker with invite UID (best-effort)
```

**Idempotency**: The invite-sent marker (step 1) is written before `SendInvite` is called (step 4), using the value `"pending"`. This prevents a concurrent redelivery from sending a duplicate invite during the `SendInvite` round-trip. After success, the marker is updated with the actual invite UID.

**Best-effort contract**: All invite operations are best-effort. Errors are logged and never cause the KV message to be NAK'd or retried — the indexing and FGA sync have already completed successfully at this point.

### Path 2 — Invite acceptance enrichment (independent subscriber)

Runs independently of KV event processing. When `INVITES_ENABLED=true`, `main.go` starts a NATS queue subscriber on `lfx.invite-service.invite_accepted` using queue group `voting-service-invite-accepted`.

```
lfx.invite-service.invite_accepted (NATS core, queue group)
        │
        ▼
InviteAcceptedSubscriber.handle
  • parse InviteServiceAcceptedEvent
  • validate: email + username both non-empty
        │
        ▼
ITX: POST /v2/voting/vote/invite_accepted
  { "email": "...", "username": "..." }
  ──── enriches all vote-response DynamoDB records for this email
```

The enrichment subscriber runs regardless of whether `EVENT_PROCESSING_ENABLED` is set. It handles all resource types (the ITX endpoint enriches by email, not by vote ID), so accepting an invite to any LFX resource triggers the backfill.

**Shutdown safety**: `InviteAcceptedSubscriber.Stop()` drains the NATS subscription before cancelling the context, ensuring in-flight `AcceptInvite` HTTP calls to ITX complete rather than being aborted.

## NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `lfx.invite-service.send_invite` | outbound (request/reply) | Send an LFID invite to a vote-response participant |
| `lfx.invite-service.invite_accepted` | inbound (subscribe) | Receive notification when a participant accepts an invite |
| `lfx.auth-service.email_to_username` | outbound (request/reply) | Look up LFX username by email before sending an invite |

## KV Markers

All markers are stored in the `v1-mappings` NATS KV bucket.

| Key pattern | Value | Purpose |
|-------------|-------|---------|
| `v1_vote_response_lfid_invite_sent.{vote_response_uid}` | `"pending"` then invite UID | Guards against duplicate invites; "pending" is written before `SendInvite` and replaced with the real invite UID on success |

## Depends On

- **lfx-v2-invite-service**: provides `send_invite` and `invite_accepted` NATS subjects
- **itx-service-voting** [PR #257](https://github.com/linuxfoundation-it/itx-service-voting/pull/257): adds `POST /v2/voting/vote/invite_accepted` to enrich DynamoDB records

## Related Docs

- [Event Processing](event-processing.md) — full KV sync pipeline context
- [Meeting Service LFID Invites](https://github.com/linuxfoundation/lfx-v2-meeting-service) — reference implementation this mirrors (LFXV2-1831)
