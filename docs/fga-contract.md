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

_(none set by the vote message itself)_

`participant` tuples on the vote are written per vote response with `member_put` / `member_remove` (see [Vote Participant](#vote-participant)). The vote `update_access` message therefore sends `exclude_relations: ["participant"]`: `update_access` is a full sync, and without the exclusion every vote update would delete the participants.

### References

| Reference | Value | Condition |
|---|---|---|
| `project` | `ProjectUID` | Only when `ProjectUID` is non-empty |
| `committee` | `CommitteeUID` | Only when `CommitteeUID` is non-empty |

> The update message is skipped entirely if both `ProjectUID` and `CommitteeUID` are empty.

### Vote Participant

`vote#viewer` is `[user:*] or auditor or participant`, and both `GET /votes/{uid}` and the indexed `vote_response` rows (`AccessCheckObject: vote:{uid}`, `AccessCheckRelation: viewer`) are gated on it. An invitee's `owner` tuple on their own `vote_response` does not grant `viewer` on the vote, so every invitee with a username is made a `participant` of the vote.

| Field | Value |
|---|---|
| Subject | `lfx.fga-sync.member_put` (`member_remove` when `VoterRemoved` is true) |
| `object_type` | `vote` |
| `uid` | `VoteUID` |
| `username` | LFX username (from v1 `username` field) |
| `relations` | `["participant"]` |

Sent on create and update of a vote response, only when both `Username` and `VoteUID` are non-empty. Because every recipient of a vote has a vote response, this covers recipients present when the vote is created as well as recipients added later. A voter whose `Username` is backfilled after accepting an LFID invite becomes a participant on that update.

#### Backfilling existing votes

`member_put` is only sent when a vote response event is processed, so votes whose responses were consumed before this behaviour shipped have no participant tuples. Run `scripts/reindex-vote-responses` (see `indexer-contract.md` § Reindexing vote responses): it replays each response record through the live handler, which writes the participant tuple along with the indexer message and the owner tuple.

Run it before, or together with, enabling participants: responses indexed with the old `vote#viewer` access check must be reindexed to `vote_response#auditor`, or every participant could read other voters' ballots until they are.

On delete of a vote response, `delete_access` removes only `vote_response:{uid}`, so the participant tuple is removed separately with `member_remove` on the vote. A **soft delete** (`_sdc_deleted_at`) still carries the record, so the voter and vote are known and the participant is removed. A **hard delete or purge** carries no value, so the voter is unknown and the participant tuple is not removed. That tuple is then cleared only through `VoterRemoved`, or when the vote itself is deleted (`delete_access` removes all tuples for `vote:{uid}`).

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
| Create / update vote response | `vote` | `lfx.fga-sync.member_put` | `participant` for the response's user; `member_remove` if `VoterRemoved`; skipped if `Username` or `VoteUID` is empty |
| Delete vote response | `vote` | `lfx.fga-sync.member_remove` | `participant` for the response's user; only on a soft delete, which still carries the record |
| Delete vote response | `vote_response` | `lfx.fga-sync.delete_access` | Always sent |
