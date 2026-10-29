# API Contracts: LFXv2 Proxy vs ITX Voting Service

This document outlines the differences between the LFXv2 Voting Service proxy API and the underlying ITX Voting Service API.

## Key Differences

### Terminology

| LFXv2 Proxy | ITX Service | Description |
|-------------|-------------|-------------|
| `vote` | `poll` | Main resource name |
| `vote_uid` | `poll_id` | Resource identifier |
| `project_uid` | `project_id` | Project identifier |
| `committee_uid` | `committee_id` | Committee identifier |
| `committee_uids` | `committee_ids` | Multiple committee identifiers |

### ID Schema Translation (v1 ↔ v2)

The proxy service acts as a translation layer between LFX v2 (UUID-based) and v1 (Salesforce ID-based) identifier schemas:

| Identifier Type | LFX v2 (Client) | LFX v1 (ITX) | Mapping Service |
| --------------- | --------------- | ------------ | --------------- |
| Project ID | UUID format | Salesforce ID (SFID) | NATS-based bidirectional lookup |
| Committee ID | UUID format | Salesforce ID (SFID) | NATS-based bidirectional lookup |
| Poll/Vote ID | UUID format | UUID format | No mapping needed |

**NATS Message Subject**: `lfx.lookup_v1_mapping`

**Mapping Request Formats**:

- Project v2→v1: `project.uid.{v2_uuid}` → `{v1_sfid}`
- Project v1→v2: `project.sfid.{v1_sfid}` → `{v2_uuid}`
- Committee v2→v1: `committee.uid.{v2_uuid}` → `{project_sfid}:{committee_sfid}`
- Committee v1→v2: `committee.sfid.{v1_sfid}` → `{v2_uuid}`

**Bidirectional Mapping**:

- **Requests (Client → ITX)**: v2 UIDs are mapped to v1 SFIDs before sending to ITX
- **Responses (ITX → Client)**: v1 SFIDs are mapped back to v2 UIDs before returning to client
- **Error Handling**: If reverse mapping (v1→v2) fails, the field is returned empty and an error is logged

This allows the LFXv2 API to maintain a consistent UUID-based schema while transparently communicating with the legacy v1-based ITX service.

### Authentication

| Aspect | LFXv2 Proxy | ITX Service |
|--------|-------------|-------------|
| User Auth | JWT Bearer token (Heimdall) | Not required (proxy handles) |
| Service Auth | Not exposed to client | OAuth2 M2M (Auth0) |
| Header | `Authorization: Bearer <jwt>` | `Authorization: Bearer <m2m-token>` |

---

## POST /votes (Create Vote)

### LFXv2 Proxy API

**Endpoint**: `POST /votes`

**Required permission**: `writer` on `project:{project_uid}` (from request body)

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Request Body**:

```json
{
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_uids": [
    "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
    "b03cebcg-64c2-5e58-cd15-ee8f560ee419"
  ],
  "committee_filters": ["Voting Rep", "Alternate Voting Rep"],
  "poll_questions": [
    {
      "prompt": "Select up to 5 TSC members",
      "type": "multiple_choice",
      "choices": [
        {"choice_text": "Alice Johnson"},
        {"choice_text": "Bob Smith"},
        {"choice_text": "Carol White"}
      ]
    }
  ],
  "poll_comment_prompts": [
    {"prompt": "Any additional comments?"}
  ],
  "pseudo_anonymity": false,
  "poll_type": "generic",
  "allow_abstain": false,
  "quorum_percentage": 50,
  "winning_threshold_percentage": 50
}
```

`end_time_timezone` is required: an IANA timezone name (e.g., `America/New_York`) that ITX uses to interpret `end_time` exactly as sent. Omitting it fails contract validation with a 400 before ITX is called. The proxy passes the value through untouched and performs no validation of its own; ITX rejects invalid timezone names with a 400, which the proxy surfaces as a 400.

**Create defaults:** `POST /votes` defaults an omitted `poll_type` to `generic`. An omitted `num_winners` defaults to two only for `meek_stv` creates; other poll types forward a winner count only when explicitly provided ([#2922](https://github.com/linuxfoundation/lfx-self-serve/issues/2922)). Explicit values retain the enum and count range (2–10) constraints. ITX [PR #285](https://github.com/linuxfoundation-it/itx-service-voting/pull/285) additionally validates Meek STV winner counts on creation: the count must not exceed the choices in any question; an incompatible count returns 400, not a silently reduced count.

**Response** (201 Created):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "status": "disabled",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_name": "Technical Steering Committee",
  "committee_type": "TSC",
  "committee_voting_status": true,
  "pseudo_anonymity": false,
  "total_voting_request_invitations": 0,
  "num_response_received": 0,
  "allow_abstain": false,
  "poll_questions": [
    {
      "question_id": "q1-uuid",
      "prompt": "Select up to 5 TSC members",
      "type": "multiple_choice",
      "choices": [
        {
          "choice_id": "c1-uuid",
          "choice_text": "Alice Johnson"
        },
        {
          "choice_id": "c2-uuid",
          "choice_text": "Bob Smith"
        },
        {
          "choice_id": "c3-uuid",
          "choice_text": "Carol White"
        }
      ]
    }
  ]
}
```

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/poll`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Request Body** (Note the field name differences):

```json
{
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "project_id": "a09P000000DsCBuIRT",
  "committee_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_ids": [
    "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
    "b03cebcg-64c2-5e58-cd15-ee8f560ee419"
  ],
  "committee_filters": ["Voting Rep", "Alternate Voting Rep"],
  "poll_questions": [
    {
      "prompt": "Select up to 5 TSC members",
      "type": "multiple_choice",
      "choices": ["Alice Johnson", "Bob Smith", "Carol White"]
    }
  ],
  "poll_comment_prompts": [
    {"prompt": "Any additional comments?"}
  ],
  "pseudo_anonymity": false,
  "poll_type": "generic",
  "allow_abstain": false,
  "quorum_percentage": 50,
  "winning_threshold_percentage": 50
}
```

**Poll origin and email routing**: The proxy automatically adds `source: "self_serve"`
to the ITX create request; clients do not supply it through the V2 API. ITX stores
this origin and uses it for both the creator's poll-ended results link and voter
email destinations. For `self_serve`, invitation, extension, resend, and reminder
emails open `/votes/{poll_id}` on the environment's Self Serve app. The poll ID is
the same identifier exposed as the V2 vote UID; no ID translation is required.
Other stored origins (`pcc`, `unknown`, or an empty legacy value) retain the
configured MyProfile/OpenProfile voter link and the PCC results link for creators.

**Response** (200 OK):

```json
{
  "poll_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "status": "disabled",
  "project_id": "a09P000000DsCBuIRT",
  "committee_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_name": "Technical Steering Committee",
  "committee_type": "TSC",
  "committee_voting_status": true,
  "pseudo_anonymity": false,
  "total_voting_request_invitations": 0,
  "num_response_received": 0,
  "allow_abstain": false,
  "poll_questions": [
    {
      "question_id": "q1-uuid",
      "prompt": "Select up to 5 TSC members",
      "type": "multiple_choice",
      "choices": [
        {
          "choice_id": "c1-uuid",
          "choice_text": "Alice Johnson"
        },
        {
          "choice_id": "c2-uuid",
          "choice_text": "Bob Smith"
        },
        {
          "choice_id": "c3-uuid",
          "choice_text": "Carol White"
        }
      ]
    }
  ]
}
```

---

## GET /votes/{vote_uid} (Get Vote)

### LFXv2 Proxy API

**Endpoint**: `GET /votes/{vote_uid}`

**Required permission**: `viewer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Response** (200 OK):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "status": "active",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_name": "Technical Steering Committee",
  "committee_type": "TSC",
  "committee_voting_status": true,
  "pseudo_anonymity": false,
  "total_voting_request_invitations": 25,
  "num_response_received": 10,
  "allow_abstain": false,
  "poll_questions": [...]
}
```

When a poll ends early, either manually or automatically because all eligible voters respond (by voting or being removed) before its scheduled `end_time`, ITX stamps `early_end_time` with the actual close time and preserves the original `end_time`. Extending an ended poll reactivates it and clears `early_end_time`. The proxy forwards `early_end_time` as a read-only field, present for both manual and automatic early endings, and normalizes zero-value timestamps to absent (mirroring ITX, since the NATS KV ingress reads raw DynamoDB attributes):

```jsonc
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "status": "ended",
  "end_time": "2026-02-15T23:59:59Z",
  "early_end_time": "2026-02-10T14:32:11Z"  // Present for manual or automatic endings before end_time
}
```

---

### ITX Service API (Underlying)

**Endpoint**: `GET /v2/voting/poll/{poll_id}`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Path Parameters**:

- `poll_id` (string, UUID): The poll identifier

**Response** (200 OK):

```json
{
  "poll_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "status": "active",
  "project_id": "a09P000000DsCBuIRT",
  "committee_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_name": "Technical Steering Committee",
  "committee_type": "TSC",
  "committee_voting_status": true,
  "pseudo_anonymity": false,
  "total_voting_request_invitations": 25,
  "num_response_received": 10,
  "allow_abstain": false,
  "poll_questions": [...]
}
```

---

## PUT /votes/{vote_uid} (Update Vote)

### LFXv2 Proxy API

**Endpoint**: `PUT /votes/{vote_uid}`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Request Body**:

```json
{
  "name": "Q1 2026 TSC Election - Updated",
  "description": "Updated description",
  "end_time": "2026-02-20T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_uids": ["a02bdbaf-53b1-4d47-bc04-dd7e459dd308"],
  "committee_filters": ["Voting Rep"],
  "poll_questions": [
    {
      "prompt": "Updated prompt",
      "type": "single_choice",
      "choices": [
        {"choice_text": "Option A"},
        {"choice_text": "Option B"}
      ]
    }
  ],
  "poll_comment_prompts": [
    {"prompt": "Comments?"}
  ],
  "pseudo_anonymity": true,
  "allow_abstain": true,
  "quorum_percentage": 60,
  "winning_threshold_percentage": 60
}
```

`end_time_timezone` is required, as on create. Omitting it fails contract validation with a 400 before ITX is called, so ITX always receives an explicit timezone on every update. As on create, ITX rejects invalid timezone names with a 400, surfaced as a 400.

**Update method/count contract:** `PUT /votes/{uid}` keeps `poll_type` and `num_winners` optional. Omitted values remain absent from the upstream ITX request; neither the HTTP decoder, generated client/CLI nor converter supplies create defaults. The public OpenAPI contract permits omission, not JSON `null`; callers must omit these fields to retain existing settings. The current decoder also treats `null` as omission, but spec-validating clients or gateways may reject it. Explicit valid values are intentional replacements, including an explicit `generic` type. Empty/unknown types and counts outside 2–10 fail voting-service validation with 400 before ITX is called. This is not a general PATCH/merge API: the other required replacement fields remain required.

Stored-value preservation is owned by ITX [#3428](https://github.com/linuxfoundation/lfx-self-serve/issues/3428), with the following update precedence:

| Effective method/count input | ITX #3428 behavior |
|---|---|
| Type omitted | Retain the stored type |
| Explicit valid type | Replace the stored type |
| Existing Meek STV, count omitted | Retain the stored count, including four; applies when type is omitted or explicitly repeats `meek_stv` |
| Effective Meek STV, explicit count | Use the supplied valid count |
| Explicit non-STV → Meek STV transition, count omitted | Use two winners |
| Effective non-STV | Do not retain a previous STV count |

The voting service does not choose a winner default on updates based on the request's decoded type; ITX resolves the effective type against storage. The Meek STV default in #2922 applies only to creates.

Merged ITX PR #285 also validates the resolved Meek STV winner count against **every replacement question's choice count**. An edit that preserves four winners but reduces a question to three choices returns 400 before persistence; it does not reset the stored count. The same bound applies to explicit winner counts and creation. Keep enough choices or explicitly select a valid smaller count.

**Frontend compatibility and release gate:** Self Serve's draft/publish updates omit both fields; PCC's disabled type control is included by `getRawValue`, so its explicit type remains supported. Neither frontend needs changes. Implement, verify with persisted readback, and release ITX #3428 **before** voting-service [#2974](https://github.com/linuxfoundation/lfx-self-serve/issues/2974). Old ITX still defaults an omitted type to `generic`; wire-omission tests alone do not prove preservation. Until the changed ITX version is available and paired smoke passes, this voting change is not release-ready. ITX's existing read-then-replace persistence is unchanged; no atomicity guarantee is added.

**Response** (200 OK):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election - Updated",
  "description": "Updated description",
  "last_modified_time": "2026-01-23T15:30:00Z",
  "end_time": "2026-02-20T23:59:59Z",
  "status": "disabled",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "poll_questions": [...]
}
```

**Note**: Updates are only allowed when `status == "disabled"`

---

### ITX Service API (Underlying)

**Endpoint**: `PUT /v2/voting/poll/{poll_id}`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Path Parameters**:

- `poll_id` (string, UUID): The poll identifier

**Request Body** (Note field name differences):

```json
{
  "name": "Q1 2026 TSC Election - Updated",
  "description": "Updated description",
  "end_time": "2026-02-20T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "project_id": "a09P000000DsCBuIRT",
  "committee_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_ids": ["a02bdbaf-53b1-4d47-bc04-dd7e459dd308"],
  "committee_filters": ["Voting Rep"],
  "poll_questions": [
    {
      "prompt": "Updated prompt",
      "type": "single_choice",
      "choices": ["Option A", "Option B"]
    }
  ],
  "poll_comment_prompts": [
    {"prompt": "Comments?"}
  ],
  "pseudo_anonymity": true,
  "poll_type": "generic",
  "allow_abstain": true,
  "quorum_percentage": 60,
  "winning_threshold_percentage": 60
}
```

**Response** (200 OK):

```json
{
  "poll_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election - Updated",
  "description": "Updated description",
  "last_modified_time": "2026-01-23T15:30:00Z",
  "end_time": "2026-02-20T23:59:59Z",
  "status": "disabled",
  "project_id": "a09P000000DsCBuIRT",
  "committee_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "poll_questions": [...]
}
```

---

## DELETE /votes/{vote_uid} (Delete Vote)

### LFXv2 Proxy API

**Endpoint**: `DELETE /votes/{vote_uid}`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Response** (204 No Content):

```
(empty body)
```

**Note**: Deletion is only allowed when `status == "disabled"`

---

### ITX Service API (Underlying)

**Endpoint**: `DELETE /v2/voting/poll/{poll_id}`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Path Parameters**:

- `poll_id` (string, UUID): The poll identifier

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## Field Mapping Reference

### Request Fields (LFXv2 → ITX)

| LFXv2 API Field | ITX API Field | Notes |
|-----------------|---------------|-------|
| `project_uid` | `project_id` | Salesforce ID format |
| `committee_uid` | `committee_id` | UUID format |
| `committee_uids` | `committee_ids` | Array of UUIDs |
| `poll_questions[].choices[].choice_text` | `choices[]` | Flattened to string array in ITX request |

### Response Fields (ITX → LFXv2)

| ITX API Field | LFXv2 API Field | Notes |
|---------------|-----------------|-------|
| `poll_id` | `vote_uid` | UUID |
| `project_id` | `project_uid` | Salesforce ID |
| `committee_id` | `committee_uid` | UUID |
| `choices[]` | `poll_questions[].choices[]` | Expanded to objects with `choice_id` and `choice_text` |

---

## Error Responses

### LFXv2 Proxy Errors

The proxy returns consistent error responses:

```json
{
  "message": "Invalid request: missing required field 'name'",
  "code": "BAD_REQUEST"
}
```

HTTP Status Codes:

- `400` - Bad Request (validation errors)
- `401` - Unauthorized (invalid/missing JWT)
- `403` - Forbidden (insufficient permissions)
- `404` - Not Found (vote does not exist)
- `409` - Conflict (business rule violation)
- `500` - Internal Server Error
- `503` - Service Unavailable (ITX service down)

### ITX Service Errors

ITX returns various error formats. The proxy normalizes these into the LFXv2 error format.

Example ITX Error:

```json
{
  "error": "Poll not found",
  "status": 404
}
```

Maps to LFXv2:

```json
{
  "message": "Poll not found",
  "code": "NOT_FOUND"
}
```

---

## Authentication Flow

### LFXv2 Proxy Flow

1. Client sends request with Heimdall JWT token
2. Proxy validates JWT using Heimdall JWKS
3. Proxy extracts principal (user ID) from JWT
4. Proxy obtains OAuth2 M2M token for ITX (cached)
5. Proxy calls ITX API with M2M token
6. Proxy translates response fields
7. Client receives response

### Headers Added by Proxy

The proxy adds these headers when calling ITX:

```http
Authorization: Bearer <oauth2-m2m-token-from-auth0>
Content-Type: application/json
Accept: application/json
x-scope: manage:voting
```

---

## Business Rules

### Vote Status Lifecycle

| Status | Can Update? | Can Delete? | Can Activate? | Description |
|--------|-------------|-------------|---------------|-------------|
| `disabled` | ✅ Yes | ✅ Yes | ✅ Yes | Initial state, not yet sent |
| `active` | ❌ No | ❌ No | N/A | Currently accepting votes |
| `ended` | ❌ No | ❌ No | N/A | Voting period closed |

### Poll Types

| Type | Description | `num_winners` |
|------|-------------|---------------|
| `generic` | Simple poll | Not used; forwarded only if explicitly provided |
| `condorcet_irv` | Condorcet IRV voting | Not used; forwarded only if explicitly provided |
| `instant_runoff_vote` | Instant runoff voting | Not used; forwarded only if explicitly provided |
| `meek_stv` | Meek STV (Single Transferable Vote) | Optional; defaults to 2 on create when omitted, must be 2–10 when provided; omitted update counts are resolved by ITX |

`num_winners` is sent upstream only when explicitly provided in the request — the one exception is a `meek_stv` create, which defaults to 2 winners when omitted. All updates leave omitted counts absent from the ITX request, including explicit `meek_stv` updates, so ITX can preserve the stored count or resolve a method transition as described above. Responses omit `num_winners` entirely when no winner count is stored upstream.

---

## POST /votes/{vote_uid}/extend (Extend Vote)

### LFXv2 Proxy API

**Endpoint**: `POST /votes/{vote_uid}/extend`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Request Body**:

```json
{
  "end_time": "2026-03-01T23:59:59Z",
  "end_time_timezone": "America/New_York"
}
```

`end_time_timezone` is required, as on create and update. Omitting it fails contract validation with a 400 before ITX is called; ITX rejects invalid timezone names with a 400, surfaced as a 400.

**Response** (200 OK):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-02-20T09:00:00Z",
  "end_time": "2026-03-01T23:59:59Z",
  "end_time_timezone": "America/New_York",
  "status": "active",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "committee_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "committee_name": "Technical Steering Committee",
  "committee_type": "TSC",
  "committee_voting_status": true,
  "pseudo_anonymity": false,
  "total_voting_request_invitations": 25,
  "num_response_received": 10,
  "allow_abstain": false,
  "poll_questions": [...]
}
```

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/poll/{poll_id}/extend`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Request Body**:

```json
{
  "end_time": "2026-03-01T23:59:59Z",
  "end_time_timezone": "America/New_York"
}
```

**Response** (200 OK): Same structure as ITX `GetPoll` response (see GET above).

---

## POST /votes/{vote_uid}/end (End Vote)

### LFXv2 Proxy API

**Endpoint**: `POST /votes/{vote_uid}/end`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Request Body**: None (there is no `reason` field; ITX records no reason for an early end)

**Response** (200 OK): Same structure as `GET /votes/{vote_uid}`. For an active vote the result has `status: "ended"`, `early_end_time` set to the moment the vote was ended, and the original `end_time` preserved.

**Behavior**:

| Current vote state | Result |
|---|---|
| `active` | Ended: `status: "ended"`, `early_end_time` stamped, `end_time` preserved |
| `ended` | `200 OK` with the current state (idempotent no-op) |
| `disabled` | `409 Conflict` — the service reads the vote from ITX first and never calls the ITX end endpoint |
| `active` but already past its scheduled `end_time` | `400 Bad Request` passed through from ITX (`poll is already past its scheduled end time`) |

Extending an ended vote reactivates it and clears `early_end_time` (see the `extend` section above). Status changes flow through the same NATS KV eventing path as any other status change.

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/poll/{poll_id}/end`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Request Body**: None

**Response** (200 OK): Same structure as ITX `GetPoll` response (see GET above).

**Errors**: ITX returns `400` for a disabled poll (`poll has not started`) and for an active poll past its `end_time`, `409` when a concurrent extend wins the race, and `404` when the poll does not exist.

---

## PUT /votes/{vote_uid}/enable (Enable Vote)

### LFXv2 Proxy API

**Endpoint**: `PUT /votes/{vote_uid}/enable`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Request Body**: None

**Response** (204 No Content):

```
(empty body)
```

**Note**: Transitions a vote from `disabled` to `active`, sending ballot emails to all recipients.

---

### ITX Service API (Underlying)

**Endpoint**: `PUT /v2/voting/poll/{poll_id}/enable`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## POST /votes/{vote_uid}/bulk_resend (Bulk Resend Vote Emails)

### LFXv2 Proxy API

**Endpoint**: `POST /votes/{vote_uid}/bulk_resend`

**Required permission**: `writer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Request Body**:

```json
{
  "recipient_ids": [
    "cba14f40-1636-11ec-9621-0242ac130002",
    "cba14f40-1636-11ec-9621-0242ac130003"
  ],
  "personalized_links": true
}
```

- `recipient_ids` (required): **vote response (ballot) UIDs** — the ITX `vote_id` of each recipient's ballot, not user IDs. ITX rejects the whole request with a 400 if any ID is not an active ballot of this vote whose voter is still a committee member.
- `personalized_links` (optional, default `false`): when `true`, each recipient's email links to a personal HMAC-signed URL that lets them submit or update their vote response without logging in (see [Personalized Vote Links](vote-links.md)). Returns 503 if the service has no signing keys configured, and 400 if `recipient_ids` is empty.

**Response** (204 No Content):

```
(empty body)
```

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/poll/{poll_id}/bulk_resend`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Request Body** (`personalized_links` false or omitted — unchanged):

```json
{
  "recipient_ids": ["cba14f40-1636-11ec-9621-0242ac130002"]
}
```

**Request Body** (`personalized_links: true`):

```json
{
  "recipient_ids": ["cba14f40-1636-11ec-9621-0242ac130002"],
  "recipient_links": {
    "cba14f40-1636-11ec-9621-0242ac130002": "https://app.lfx.dev/votes/8f3c2a1e-4b5d-4e6f-9a7b-1c2d3e4f5a6b#vote_token=<signed-vote-token>"
  }
}
```

**Note**: `recipient_ids` are passed through to ITX without transformation. `recipient_links` is keyed by `vote_id`; ITX uses each value as that recipient's `VotingLink` in place of the standard Self Serve link. **`recipient_links` requires ITX support** — see [Personalized Vote Links](vote-links.md#itx-dependency).

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## GET /votes/{vote_uid}/results (Get Vote Results)

### LFXv2 Proxy API

**Endpoint**: `GET /votes/{vote_uid}/results`

**Required permission**: `results_viewer` on `vote:{vote_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_uid` (string, UUID): The vote identifier

**Response** (200 OK):

```json
{
  "num_recipients": 25,
  "num_votes_cast": 18,
  "num_abstained": 2,
  "poll_end_time": "2026-02-15T23:59:59Z",
  "poll_end_time_timezone": "America/New_York",
  "poll_results": [
    {
      "question": {
        "question_id": "q1-uuid",
        "prompt": "Select up to 5 TSC members",
        "type": "multiple_choice",
        "choices": [
          {"choice_id": "c1-uuid", "choice_text": "Alice Johnson"},
          {"choice_id": "c2-uuid", "choice_text": "Bob Smith"}
        ]
      },
      "generic_choice_votes": [
        {"choice_id": "c1-uuid", "vote_count": 14, "percentage": 77.8},
        {"choice_id": "c2-uuid", "vote_count": 10, "percentage": 55.6}
      ],
      "ranked_choice_winner_info": {"poll_choices": [], "condorcet_irv_used_for_eliminations": false}
    }
  ],
  "comment_results": [
    {
      "prompt": {"prompt_id": "p1-uuid", "prompt": "Any additional comments?"},
      "responses": [
        {
          "vote_id": "v1-uuid",
          "user_id": "example-user-id",
          "comment_text": "Great candidates this year.",
          "user_name": "Example User",
          "profile_picture": "https://example.org/avatar.png",
          "vote_creation_time": "2026-02-10T15:04:05Z",
          "abstained": false
        },
        {
          "vote_id": "v2-uuid",
          "user_id": "example-user-id-2",
          "comment_text": "No objections.",
          "user_name": "Example Voter",
          "profile_picture": "https://example.org/avatar2.png",
          "vote_creation_time": "2026-02-11T09:30:00Z",
          "abstained": false
        }
      ]
    }
  ]
}
```

**Note**: `ranked_choice_votes`, `irv_round_summary`, and `meek_stv_round_summary` are populated only for the corresponding `poll_type` values (`condorcet_irv`, `instant_runoff_vote`, `meek_stv`). For `generic` polls, only `generic_choice_votes` is populated. Unpopulated optional keys are omitted from the response entirely (never serialized as `null` or `[]`), which is why the example above has no `ranked_choice_votes` / `irv_round_summary` / `meek_stv_round_summary` keys. `ranked_choice_winner_info` is always present (ITX emits it for every poll type), with `poll_choices: []` when no winner has been determined (the HTTP encoder normalizes nil choices to an empty array). On comment responses, `user_id`, `user_name`, and `profile_picture` are absent entirely when the poll has `pseudo_anonymity` enabled.

---

### ITX Service API (Underlying)

**Endpoint**: `GET /v2/voting/poll/{poll_id}/results`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Response**: Same structure — results are passed through from ITX without field transformation.

---

## POST /vote_responses (Submit Vote Response)

### LFXv2 Proxy API

**Endpoint**: `POST /vote_responses`

**Required permission**: `owner` on `vote_response:{vote_response_uid}` (from request body)

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Request Body** — standard choice (`vote_response_uid` is provided in the body, not the path):

```json
{
  "vote_response_uid": "vr-uuid-123",
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "abstain": false,
  "user_vote_content": [
    {
      "question_id": "q1-uuid",
      "choice_ids": ["c1-uuid", "c3-uuid"]
    }
  ]
}
```

**Request Body** — ranked choice:

```json
{
  "vote_response_uid": "vr-uuid-123",
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "abstain": false,
  "user_vote_content": [
    {
      "question_id": "q1-uuid",
      "ranked_choices": [
        {"choice_id": "c1-uuid", "choice_rank": 1},
        {"choice_id": "c2-uuid", "choice_rank": 2}
      ]
    }
  ]
}
```

**Request Body** — abstain:

```json
{
  "vote_response_uid": "vr-uuid-123",
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "abstain": true,
  "user_vote_content": []
}
```

**Response** (204 No Content):

```
(empty body)
```

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/vote`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Note**: `vote_uid` is passed as `poll_id` to ITX. No ID mapping is applied to vote response identifiers — they are UUIDs in both systems.

**Request Body**:

```json
{
  "vote_id": "vote-response-uuid",
  "poll_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "abstain": false,
  "user_vote_content": [
    {
      "question_id": "q1-uuid",
      "choice_ids": ["c1-uuid", "c3-uuid"]
    }
  ]
}
```

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## GET /vote_responses/{vote_response_uid} (Get Vote Response)

### LFXv2 Proxy API

**Endpoint**: `GET /vote_responses/{vote_response_uid}`

**Required permission**: `auditor` on `vote_response:{vote_response_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_response_uid` (string, UUID): The vote response identifier

**Response** (200 OK):

```json
{
  "vote_response_uid": "vr-uuid-123",
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "project_uid": "c01adbaf-53b1-4d47-bc04-dd7e459dd301",
  "vote_status": "voted",
  "abstained": false,
  "allow_abstain": false,
  "vote_creation_time": "2026-01-25T14:30:00Z",
  "user_name": "Alice Johnson",
  "profile_picture": "https://example.com/alice.jpg",
  "user_id": "user-uuid-alice",
  "user_email": "alice@example.com",
  "user_role": "Voting Rep",
  "user_voting_status": "voted",
  "user_org_name": "Acme Corp",
  "user_org_id": "org-uuid-acme",
  "ses_message_id": "ses-msg-id-123",
  "ses_message_last_sent_time": "2026-01-24T10:00:00Z",
  "ses_delivery_successful": true,
  "ses_email_opened": true,
  "ses_email_opened_last_time": "2026-01-24T11:30:00Z",
  "ses_link_clicked": true,
  "ses_link_clicked_last_time": "2026-01-25T14:28:00Z",
  "ses_bounce_type": "",
  "ses_bounce_subtype": "",
  "ses_complaint_exists": false,
  "ses_complaint_type": "",
  "ses_complaint_date": null,
  "poll_answers": [
    {
      "question_id": "q1-uuid",
      "prompt": "Select up to 5 TSC members",
      "type": "multiple_choice",
      "user_choice": [
        {"choice_id": "c1-uuid", "choice_text": "Alice Johnson"},
        {"choice_id": "c3-uuid", "choice_text": "Carol White"}
      ],
      "ranked_user_choice": []
    }
  ]
}
```

**Note**: The `ses_*` fields reflect email delivery and engagement tracking from AWS SES. `project_uid` is mapped from the ITX v1 Salesforce ID back to a LFXv2 UUID.

---

### ITX Service API (Underlying)

**Endpoint**: `GET /v2/voting/vote/{vote_id}`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Response**: Same structure with ITX field names — `vote_response_uid` is `vote_id`, `vote_uid` is `poll_id`, and `project_uid` is `project_id` (Salesforce ID format).

---

## PUT /vote_responses/{vote_response_uid} (Update Vote Response)

### LFXv2 Proxy API

**Endpoint**: `PUT /vote_responses/{vote_response_uid}`

**Required permission**: `owner` on `vote_response:{vote_response_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
Content-Type: application/json
```

**Path Parameters**:

- `vote_response_uid` (string, UUID): The vote response identifier

**Request Body**:

```json
{
  "abstain": false,
  "user_vote_content": [
    {
      "question_id": "q1-uuid",
      "choice_ids": ["c2-uuid", "c3-uuid"]
    }
  ]
}
```

**Response** (204 No Content):

```
(empty body)
```

---

### ITX Service API (Underlying)

**Endpoint**: `PUT /v2/voting/vote/{vote_id}`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
Content-Type: application/json
x-scope: manage:voting
```

**Request Body**: Same structure as create. No ID mapping applied to vote response fields.

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## POST /vote_responses/{vote_response_uid}/resend (Resend Vote Email)

### LFXv2 Proxy API

**Endpoint**: `POST /vote_responses/{vote_response_uid}/resend`

**Required permission**: `owner` on `vote_response:{vote_response_uid}`

**Headers**:

```http
Authorization: Bearer <heimdall-jwt-token>
```

**Path Parameters**:

- `vote_response_uid` (string, UUID): The vote response identifier

**Request Body**: None

**Response** (204 No Content):

```
(empty body)
```

---

### ITX Service API (Underlying)

**Endpoint**: `POST /v2/voting/vote/{vote_id}/resend`

**Headers**:

```http
Authorization: Bearer <oauth2-m2m-token>
x-scope: manage:voting
```

**Response** (200 OK or 204 No Content):

```
(empty body)
```

---

## POST /vote_links/responses (Submit Vote Response by Link)

### LFXv2 Proxy API

**Endpoint**: `POST /vote_links/responses`

**Required permission**: none — Heimdall admits the request anonymously. The `vote_token` from a personalized vote link is the only authorization; the service verifies its HMAC signature. See [Personalized Vote Links](vote-links.md).

**Headers**:

```http
Content-Type: application/json
```

**Request Body**:

```json
{
  "vote_token": "<signed-vote-token>",
  "user_vote_content": [
    {
      "question_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
      "choice_ids": ["b03cdbaf-53b1-4d47-bc04-dd7e459dd309"]
    }
  ],
  "abstain": false,
  "comment_responses": []
}
```

The ballot is identified **only** by the token. The body has no `vote_response_uid`, and any such field is ignored.

**Response** (204 No Content). Errors:

| Status | Cause |
|---|---|
| 400 | Payload validation, or ITX rejection (`poll has ended`, `vote is not awaiting response`, `vote cannot be submitted for a removed voter`, ...) |
| 404 | `invalid voting link` — malformed, tampered, or unknown-key token |
| 503 | Personalized vote links are not configured on this service |

### ITX Service API (Underlying)

`POST /v2/voting/vote` with `vote_id` set to the token's vote response UID — identical to [POST /vote_responses](#post-vote_responses-submit-vote-response).

---

## POST /vote_links/ballot (Get Ballot by Link)

### LFXv2 Proxy API

**Endpoint**: `POST /vote_links/ballot`

**Required permission**: none. Heimdall admits the request anonymously; the `vote_token` is the only authorization. See [Personalized Vote Links](vote-links.md#reading-the-ballot).

**Request Body**:

```json
{ "vote_token": "<signed-vote-token>" }
```

**Response** (200 OK):

```json
{
  "vote_uid": "8f3c2a1e-4b5d-4e6f-9a7b-1c2d3e4f5a6b",
  "project_uid": "7cad5a8d-19d0-41a4-81a6-043453daf9ee",
  "project_slug": "cncf",
  "name": "TSC election",
  "description": "Pick one",
  "status": "active",
  "end_time": "2026-12-01T23:59:59Z",
  "poll_type": "generic",
  "allow_abstain": true,
  "pseudo_anonymity": false,
  "poll_questions": [
    { "question_id": "q1", "prompt": "Who?", "type": "single_choice",
      "choices": [ { "choice_id": "c-a", "choice_text": "Alice" } ] }
  ],
  "ballot": {
    "vote_status": "responded",
    "already_voted": true,
    "voter_removed": false,
    "abstained": false,
    "poll_answers": [ { "question_id": "q1", "prompt": "Who?", "type": "single_choice",
      "user_choice": [ { "choice_id": "c-a", "choice_text": "Alice" } ] } ],
    "comment_responses": []
  }
}
```

- `ballot.abstained`, `ballot.poll_answers` and `ballot.comment_responses` are present only after voting, and never on pseudo-anonymous votes. If ITX omits `pseudo_anonymity` or returns `null`, the vote is treated (and reported) as pseudo-anonymous.
- Optional vote fields (`end_time_timezone`, `early_end_time`, `num_winners`, `poll_comment_prompts`) follow the same rules as `GET /votes/{vote_uid}`.
- `project_uid` is the v2 project UID. `project_slug` is ITX's poll `project_name`, which holds the slug, and is omitted when empty. Use the query service for other project details.

**Errors**: 404 `invalid voting link` (bad token, or the ballot isn't part of the token's vote), 404 from ITX (deleted vote or ballot), 503 (not configured, ITX unavailable, or the project ID can't be mapped).

### ITX Service API (Underlying)

`GET /v2/voting/vote/{vote_id}` (ballot from the token's `r`), then `GET /v2/voting/poll/{poll_id}` (vote from the token's `v`). The ITX vote response's `voter_removed` field is mapped to `ballot.voter_removed`. The poll's `project_id` (SFID) is mapped to `project_uid` via `lfx.lookup_v1_mapping`.

---

## PUT /vote_links/responses (Update Vote Response by Link)

### LFXv2 Proxy API

**Endpoint**: `PUT /vote_links/responses`

**Required permission**: none — authorized by `vote_token`, as for `POST /vote_links/responses`.

**Request Body**: same fields as `POST /vote_links/responses`. As with `PUT /vote_responses/{vote_response_uid}`, omitting `comment_responses` preserves existing comments; including it (even as `[]`) replaces them.

**Response** (204 No Content). Errors are as for `POST /vote_links/responses`; ITX returns 400 when no ballot has been submitted yet (`vote_status != responded`).

### ITX Service API (Underlying)

`PUT /v2/voting/vote/{vote_id}` with `vote_id` from the token — identical to [PUT /vote_responses/{vote_response_uid}](#put-vote_responsesvote_response_uid-update-vote-response).

---

## Notes for Developers

1. **ID Format**: LFXv2 uses `_uid` suffix for consistency, but these map to ITX `_id` fields
2. **Choice Structure**: In requests to ITX, choices are sent as string arrays. In responses, they come back as objects with IDs
3. **Authentication**: The proxy handles all Auth0 OAuth2 M2M token management internally
4. **Caching**: M2M tokens are cached and automatically refreshed
5. **Error Translation**: All ITX errors are normalized to LFXv2 error format
6. **Field Mapping**: The proxy automatically maps between `*_uid` and `*_id` fields
