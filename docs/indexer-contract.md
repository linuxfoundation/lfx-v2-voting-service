# Indexer Contract — Voting Service

This document is the authoritative reference for all data the voting service sends to the indexer service, which makes resources searchable via the [query service](https://github.com/linuxfoundation/lfx-v2-query-service).

The voting service is a **wrapper service** — it ingests data from the legacy ITX system via NATS KV bucket events and republishes index messages to the LFX indexer. It does not own data directly.

**Update this document in the same PR as any change to indexer message construction.**

---

## Resource Types

- [Vote (Poll)](#vote-poll)
- [Vote Response](#vote-response)
- [Vote Result](#vote-result)

---

## Vote (Poll)

**Object type:** `vote`

**Source struct:** `internal/domain/event_models.go` — `VoteData`

**Publisher:** `internal/infrastructure/eventing/nats_publisher.go` — `sendVoteIndexerMessage`

**NATS subject:** `lfx.index.vote`

**Indexed on:** create, update, delete events from the ITX NATS KV bucket (poll data).

### Data Schema

These fields are indexed and queryable via `filters` or `cel_filter` in the query service.

| Field | Type | Description |
| --- | --- | --- |
| `vote_uid` | string | Vote unique identifier (v2 primary key; same value as `poll_id`) |
| `poll_id` | string | ITX poll identifier (v1 primary key) |
| `name` | string | Vote/poll display name |
| `description` | string | Vote description |
| `creation_time` | string | Creation timestamp from ITX |
| `last_modified_time` | string | Last modified timestamp from ITX |
| `end_time` | string | Vote end time |
| `end_time_timezone` | string | IANA timezone name used to interpret `end_time` (e.g., `America/New_York`). Absent for polls without a stored timezone. |
| `early_end_time` | string | Actual close time when the poll auto-ended (all voters responded before `end_time`). Absent for polls that closed on schedule. RFC3339. |
| `status` | string | Vote status (e.g., `Open`, `Closed`) |
| `project_id` | string | ITX project identifier (v1 SFID) |
| `project_uid` | string | LFX project UID (v2) |
| `project_name` | string | Project display name |
| `committee_id` | string | ITX committee identifier (v1 SFID) |
| `committee_uid` | string | LFX committee UID (v2) |
| `committee_name` | string | Committee display name |
| `committee_type` | string | Committee type |
| `committee_voting_status` | bool | Whether the committee has voting enabled |
| `committee_filters` | []string | Committee filter values applied to this vote |
| `total_voting_request_invitations` | int | Total number of voting invitations sent |
| `poll_questions` | []object | Poll questions (see [Poll Question schema](#poll-question-schema)) |
| `poll_comment_prompts` | []object | Free-text comment prompts (see [Poll Comment Prompt schema](#poll-comment-prompt-schema)). Separate from `poll_questions`; not counted toward poll results |
| `num_response_received` | int | Number of responses received |
| `poll_type` | string | Poll type (e.g., `election`, `general`) |
| `pseudo_anonymity` | bool | Whether responses are pseudo-anonymous |
| `num_winners` | int | Number of winners for election-type polls |
| `allow_abstain` | bool | Whether abstaining is allowed |

#### Poll Question Schema

Each element in `poll_questions` has:

| Field | Type | Description |
| --- | --- | --- |
| `question_id` | string | Question identifier |
| `prompt` | string | Question text |
| `type` | string | Question type (e.g., `single_choice`, `multiple_choice`, `ranked_choice`) |
| `choices` | []object | Available choices (each has `choice_id` string and `choice_text` string) |

#### Poll Comment Prompt Schema

Each element in `poll_comment_prompts` has:

| Field | Type | Description |
| --- | --- | --- |
| `prompt_id` | string | Comment prompt identifier (server-assigned by ITX) |
| `prompt` | string | Comment prompt text (max 500 characters) |

### Tags

| Tag Format | Example | Purpose |
| --- | --- | --- |
| `committee_uid:{value}` | `committee_uid:061a110a-7c38-4cd3-bfcf-fc8511a37f35` | Find votes for a committee |
| `project_uid:{value}` | `project_uid:cbef1ed5-17dc-4a50-84e2-6cddd70f6878` | Find votes for a project |

> Tags are only emitted when the corresponding UID is non-empty.

### Access Control (IndexingConfig)

| Field | Value |
| --- | --- |
| `access_check_object` | `vote:{vote_uid}` |
| `access_check_relation` | `viewer` |
| `history_check_object` | `vote:{vote_uid}` |
| `history_check_relation` | `auditor` |

### Search Behavior

| Field | Value |
| --- | --- |
| `fulltext` | `name`, `description` (space-joined) |
| `name_and_aliases` | `name` (when non-empty) |
| `sort_name` | `name` |
| `public` | `false` (always) |

### Parent References

| Ref | Condition |
| --- | --- |
| `project:{project_uid}` | Only when `project_uid` is non-empty |
| `committee:{committee_uid}` | Only when `committee_uid` is non-empty |

---

## Vote Response

**Object type:** `vote_response`

**Source struct:** `internal/domain/event_models.go` — `VoteResponseData`

**Publisher:** `internal/infrastructure/eventing/nats_publisher.go` — `sendVoteResponseIndexerMessage`

**NATS subject:** `lfx.index.vote_response`

**Indexed on:** create, update, delete events from the ITX NATS KV bucket (vote response data).

### Data Schema

| Field | Type | Description |
| --- | --- | --- |
| `uid` | string | Vote response unique identifier (v2 primary key; same value as `vote_id`) |
| `vote_id` | string | ITX vote identifier (v1 primary key) |
| `vote_uid` | string | UID of the parent vote/poll (v2) |
| `poll_id` | string | ITX poll identifier (v1) |
| `project_id` | string | ITX project identifier (v1 SFID) |
| `project_uid` | string | LFX project UID (v2) |
| `vote_creation_time` | string | Time the vote response was created |
| `user_id` | string | Auth0 user identifier |
| `user_email` | string | User's email address (lowercased at index time, GH #3063) |
| `user_role` | string | User's role at time of voting |
| `user_name` | string | User's display name |
| `username` | string | User's LFX username |
| `profile_picture` | string | URL to user's profile picture |
| `user_voting_status` | string | User's voting eligibility status |
| `user_org_id` | string | User's organization identifier |
| `user_org_name` | string | User's organization name |
| `poll_answers` | []object | User's answers to poll questions (see [Poll Answer schema](#poll-answer-schema)) |
| `comment_responses` | []object | User's free-text responses to comment prompts (see [Comment Response schema](#comment-response-schema)). May be present even when `abstained` is true |
| `vote_status` | string | Vote response status |
| `abstained` | bool | Whether the user abstained |
| `voter_removed` | bool | Whether the voter was removed |
| `ses_message_id` | string | AWS SES message identifier |
| `ses_message_last_sent_time` | string | Timestamp of last SES email send |
| `ses_bounce_type` | string | SES bounce type (if any) |
| `ses_bounce_subtype` | string | SES bounce subtype (if any) |
| `ses_delivery_successful` | bool | Whether SES delivery succeeded |
| `ses_complaint_exists` | bool | Whether an SES complaint was filed |
| `ses_complaint_type` | string | SES complaint type (if any) |
| `ses_complaint_date` | string | Date of SES complaint (if any) |
| `ses_email_opened` | bool | Whether the email was opened |
| `ses_email_opened_first_time` | string | First time the email was opened |
| `ses_email_opened_last_time` | string | Last time the email was opened |
| `ses_link_clicked` | bool | Whether a link in the email was clicked |
| `ses_link_clicked_first_time` | string | First time a link was clicked |
| `ses_link_clicked_last_time` | string | Last time a link was clicked |

#### Poll Answer Schema

Each element in `poll_answers` has:

| Field | Type | Description |
| --- | --- | --- |
| `question_id` | string | Question identifier |
| `prompt` | string | Question text |
| `type` | string | Question type |
| `user_choice` | []object (optional) | Selected choices for non-ranked questions (each has `choice_id` and `choice_text`) |
| `ranked_user_choice` | []object (optional) | Ranked choices for ranked questions (each has `choice_id`, `choice_text`, `choice_rank` int) |

#### Comment Response Schema

Each element in `comment_responses` has:

| Field | Type | Description |
| --- | --- | --- |
| `prompt_id` | string | Identifier of the poll's comment prompt being answered |
| `comment_text` | string | Voter's free-text response (max 5000 characters) |

### Tags

| Tag Format | Example | Purpose |
| --- | --- | --- |
| `vote_uid:{value}` | `vote_uid:abc123-...` | Find responses for a vote |
| `project_uid:{value}` | `project_uid:cbef1ed5-...` | Find responses for a project |

> Tags are only emitted when the corresponding UID is non-empty.

### Access Control (IndexingConfig)

| Field | Value |
| --- | --- |
| `access_check_object` | `vote_response:{uid}` |
| `access_check_relation` | `auditor` |
| `history_check_object` | `vote_response:{uid}` |
| `history_check_relation` | `auditor` |

> Note: access checks are scoped to the response itself. `vote_response#auditor` is `owner or auditor from vote`, so a response is visible to the voter who cast it and to the vote's project/committee auditors. It must not be gated on `vote#viewer`: invitees are `participant`s of the vote (see `fga-contract.md`), which grants `viewer`, and that would expose every voter's `poll_answers` to every other participant.
>
> Documents indexed before this change still carry `access_check_object: vote:{vote_uid}` until they are re-indexed. Run `reindex-vote-responses` (below) to re-emit them.

#### Reindexing vote responses

`scripts/reindex-vote-responses` reindexes responses the same way `reindex_meetings` does in lfx-v2-meeting-service. It scrolls OpenSearch for the indexed `vote_response` documents, then re-puts each one's `itx-poll-vote.{object_id}` record in the `v1-objects` KV bucket with its existing bytes. That lands as a new revision on `$KV.v1-objects.itx-poll-vote.>`, which the voting consumer already watches, so every record goes through the live handler: the indexer message with the current `IndexingConfig`, the `vote_response` owner tuple, and the vote participant tuple (`fga-contract.md`).

OpenSearch drives the run because `v1-objects` holds every v1 object type and is far too large to list; the key for each response is built from its indexed `object_id` instead.

```bash
make build-reindex
# NATS and OpenSearch, e.g. via kubectl port-forward (svc/lfx-platform-nats 4222, svc/opensearch-cluster-master 9200, namespace lfx)
export NATS_URL=nats://localhost:4222
export OPENSEARCH_URL=http://localhost:9200

bin/reindex-vote-responses -list                       # dry run: print every record it would replay
bin/reindex-vote-responses -vote-uid <uid> -apply      # one vote first; check the indexed doc and read_tuples
bin/reindex-vote-responses -apply                      # everything
```

- Run it **before** enabling `VOTE_PARTICIPANT_GRANTS_ENABLED`, then again after (see `fga-contract.md` § Rollout). It is what moves existing responses to `vote_response#auditor`, and the second run writes the participant tuples.
- It is a **dry run unless `-apply` is passed**, and reports a count for every category it skips.
- The re-put is a **compare-and-swap on the revision it read**. The bucket is written by the upstream sync, so a record that changed in between is never overwritten with stale bytes; it is counted as a conflict (the newer write already went through the consumer).
- **Transient KV read errors are retried** (3 retries over about 4 seconds) before the run aborts, so a single timeout does not kill a long run. If the run still aborts, re-run it with `-skip-reindexed`: it skips responses already on `vote_response#auditor`, so it resumes where the last run stopped instead of re-putting everything. Do **not** use `-skip-reindexed` for the pass after enabling `VOTE_PARTICIPANT_GRANTS_ENABLED`: those responses are already on the new check but still need their participant tuple.
- It **fails instead of skipping** when a read is incomplete: any KV read error other than "not found", an OpenSearch error, or a partial OpenSearch result (`timed_out` or failed shards) aborts the run with a non-zero exit, so a partial run is never mistaken for a complete one.
- It processes records as it reads them, so record payloads are never accumulated. It does keep the set of response IDs seen so far (to drop duplicates) and one entry per parent vote, so memory still grows with the number of responses, but only by those IDs.
- The OpenSearch scroll is kept alive for the time one page takes at the configured `-rate` (doubled, plus 2 minutes), so a low rate does not outlive it.
- It only touches responses that are indexed; a response with no document in OpenSearch is left alone, and an indexed response with no `v1-objects` record is counted as `indexed but not in v1-objects`.
- It replays only records the consumer has **already processed** (they have a `vote_response.{uid}` mapping and a mapped parent vote), so the handler treats them as updates and the LFID invite path, which fires only on a first create, does not run. Soft-deleted and tombstoned records are skipped.
- It is paced at `-rate` (default 50/s) because each record fans out to the indexer and fga-sync. Watch consumer lag on `KV_v1-objects` during a full run.
- If a replay is applied while the consumer is down, nothing is lost: the messages queue and are consumed when it returns.

### Search Behavior

| Field | Value |
| --- | --- |
| `fulltext` | `username` |
| `name_and_aliases` | `username` (when non-empty) |
| `sort_name` | `username` |
| `public` | `false` (always) |

### Parent References

| Ref | Condition |
| --- | --- |
| `project:{project_uid}` | Only when `project_uid` is non-empty |
| `vote:{vote_uid}` | Only when `vote_uid` is non-empty |

---

## Vote Result

**Object type:** `vote_result`

**Source struct:** `internal/domain/event_models.go` — `PollResultData`

**Publisher:** `internal/infrastructure/eventing/nats_publisher.go` — `sendVoteResultIndexerMessage`

**NATS subject:** `lfx.index.vote_result`

**KV source:** `v1-objects` bucket, key prefix `itx-poll-results.*`

**Indexed on:** create, update, delete events streamed from the `itx-poll-results` DynamoDB table via `lfx-v1-sync-helper`.

**No FGA messages are emitted.** Access to `vote_result` is governed entirely by the parent vote's `results_viewer` relation in OpenFGA.

### Data Schema

| Field | Type | Description |
|---|---|---|
| `vote_uid` | string | Vote UID (v2 primary key; same value as `poll_id`) |
| `poll_id` | string | ITX poll identifier (v1 primary key) |
| `committee_id` | string | ITX committee identifier (v1 SFID) |
| `committee_uid` | string | LFX committee UID (v2) |
| `project_id` | string | ITX project identifier (v1 SFID) |
| `project_uid` | string | LFX project UID (v2) |
| `status` | string | Vote status at snapshot time (e.g., `Open`, `Closed`) |
| `num_recipients` | int | Number of eligible voters |
| `num_votes_cast` | int | Number of votes submitted |
| `num_abstained` | int | Number of abstentions |
| `poll_end_time` | string | Scheduled end time of the vote |
| `created_time` | string | When the result snapshot was first created |
| `last_modified_time` | string | When the result snapshot was last updated |
| `poll_questions_result` | []object | Per-question choice tallies (see schema below) |

#### Poll Question Result Schema

Each element in `poll_questions_result` has:

| Field | Type | Description |
|---|---|---|
| `question_id` | string | Question identifier |
| `prompt` | string | Question text |
| `choice_results` | []object | Per-choice tallies (each has `choice_id`, `choice_text`, `vote_count` int, `percentage` float64) |

### Tags

| Tag Format | Condition |
|---|---|
| `vote_uid:{value}` | Always (required field) |
| `committee_uid:{value}` | Only when `committee_uid` is non-empty |
| `project_uid:{value}` | Only when `project_uid` is non-empty |

### Access Control (IndexingConfig)

| Field | Value |
|---|---|
| `access_check_object` | `vote:{vote_uid}` |
| `access_check_relation` | `results_viewer` |
| `history_check_object` | `vote:{vote_uid}` |
| `history_check_relation` | `auditor` |
| `public` | `false` (always) |

### Parent References

| Ref | Condition |
|---|---|
| `vote:{vote_uid}` | Always (required) |
| `project:{project_uid}` | Only when `project_uid` is non-empty |
| `committee:{committee_uid}` | Only when `committee_uid` is non-empty |
