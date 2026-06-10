# FGA Contract — Voting Service

This document is the authoritative reference for all messages the voting service sends to the fga-sync service, which writes and deletes [OpenFGA](https://openfga.dev/) relationship tuples to enforce access control.

The full OpenFGA type definitions (relations, schema) for all object types are defined in the [platform model](https://github.com/linuxfoundation/lfx-v2-helm/blob/main/charts/lfx-platform/templates/openfga/model.yaml).

**Update this document in the same PR as any change to FGA message construction.**

---

## Prerequisites

> **Deployment order:** `fga-sync` must be updated to accept LFX usernames in relation values (e.g., `owner`) before this service version is deployed. See [LFXV2-1962](https://linuxfoundation.atlassian.net/browse/LFXV2-1962).

> **Username handling:** This service forwards the v1 `username` field unchanged. fga-sync builds OpenFGA user principals as `user:{username}` without additional sanitization. LFX usernames are expected to be valid LFID identifiers that do not contain OpenFGA-reserved characters (`:`, `*`, `#`).

---

## Object Types

- [Vote](#vote)
- [Vote Response](#vote-response)

---

## Message Format

All messages use the generic FGA message format on the following NATS subjects:

| Subject | Used for |
|---|---|
| `lfx.fga-sync.update_access` | Create and update operations |
| `lfx.fga-sync.delete_access` | Delete operations |

Each message carries `object_type`, `operation`, and a `data` map. The sections below describe the `data` contents for each object type.

---

## Vote

**Source struct:** `internal/domain/` — `VoteData`

**Synced on:** create, update, delete of a vote (poll).

### Access Config

| Field | Value |
|---|---|
| `object_type` | `vote` |
| `public` | `false` (always) |

### Relations

_(none set by this service)_

### References

| Reference | Value | Condition |
|---|---|---|
| `project` | `ProjectUID` | Only when `ProjectUID` is non-empty |
| `committee` | `CommitteeUID` | Only when `CommitteeUID` is non-empty |

> The update message is skipped entirely if both `ProjectUID` and `CommitteeUID` are empty.

### Delete

On delete, only `uid` is sent — all FGA tuples for `vote:{uid}` are removed by the fga-sync service.

---

## Vote Response

**Source struct:** `internal/domain/` — `VoteResponseData`

**Synced on:** create, update, delete of a vote response.

### Access Config

| Field | Value |
|---|---|
| `object_type` | `vote_response` |
| `public` | `false` (always) |

### Relations

| Relation | Value | Condition |
|---|---|---|
| `owner` | LFX username (from v1 `username` field) | Only when `Username` is non-empty |

### References

| Reference | Value | Condition |
|---|---|---|
| `vote` | `VoteUID` | Only when `VoteUID` is non-empty |

> The update message is skipped entirely if `Username` and `VoteUID` are both empty.

### Delete

On delete, only `uid` is sent — all FGA tuples for `vote_response:{uid}` are removed by the fga-sync service.

---

## Triggers

| Operation | Object Type | Subject | Notes |
|---|---|---|---|
| Create vote | `vote` | `lfx.fga-sync.update_access` | Skipped if both `ProjectUID` and `CommitteeUID` are empty |
| Update vote | `vote` | `lfx.fga-sync.update_access` | Skipped if both `ProjectUID` and `CommitteeUID` are empty |
| Delete vote | `vote` | `lfx.fga-sync.delete_access` | Always sent |
| Create vote response | `vote_response` | `lfx.fga-sync.update_access` | Skipped if `Username` and `VoteUID` are both empty |
| Update vote response | `vote_response` | `lfx.fga-sync.update_access` | Skipped if `Username` and `VoteUID` are both empty |
| Delete vote response | `vote_response` | `lfx.fga-sync.delete_access` | Always sent |
