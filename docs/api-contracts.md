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
  "num_winners": 5,
  "allow_abstain": false,
  "quorum_percentage": 50,
  "winning_threshold_percentage": 50
}
```

**Response** (201 Created):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
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
  "num_winners": 5,
  "allow_abstain": false,
  "quorum_percentage": 50,
  "winning_threshold_percentage": 50
}
```

**Response** (200 OK):

```json
{
  "poll_id": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-01-23T10:00:00Z",
  "end_time": "2026-02-15T23:59:59Z",
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

When a poll auto-ends because all eligible voters respond (by voting or being removed) before its scheduled `end_time`, ITX stamps `early_end_time` with the actual close time and preserves the original `end_time`. Extending an ended poll reactivates it and clears `early_end_time`. The proxy forwards `early_end_time` as a read-only field, present only on auto-ended polls, and normalizes zero-value timestamps to absent (mirroring ITX, since the NATS KV ingress reads raw DynamoDB attributes):

```jsonc
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "status": "ended",
  "end_time": "2026-02-15T23:59:59Z",
  "early_end_time": "2026-02-10T14:32:11Z"  // Only present when all voters responded before end_time
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
  "poll_type": "generic",
  "num_winners": 1,
  "allow_abstain": true,
  "quorum_percentage": 60,
  "winning_threshold_percentage": 60
}
```

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
  "num_winners": 1,
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

| Type | Description | `num_winners` Required? |
|------|-------------|------------------------|
| `generic` | Simple poll | No |
| `condorcet_irv` | Condorcet IRV voting | No |
| `meek_stv` | Meek STV (Single Transferable Vote) | Yes (must be ≥ 2) |

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
  "end_time": "2026-03-01T23:59:59Z"
}
```

**Response** (200 OK):

```json
{
  "vote_uid": "a02bdbaf-53b1-4d47-bc04-dd7e459dd308",
  "name": "Q1 2026 TSC Election",
  "description": "Technical Steering Committee Election for Q1 2026",
  "creation_time": "2026-01-23T10:00:00Z",
  "last_modified_time": "2026-02-20T09:00:00Z",
  "end_time": "2026-03-01T23:59:59Z",
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
  "end_time": "2026-03-01T23:59:59Z"
}
```

**Response** (200 OK): Same structure as ITX `GetPoll` response (see GET above).

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
    "user-uuid-1",
    "user-uuid-2"
  ]
}
```

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

**Request Body**:

```json
{
  "recipient_ids": [
    "user-uuid-1",
    "user-uuid-2"
  ]
}
```

**Note**: `recipient_ids` are passed through to ITX without transformation.

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
      "ranked_choice_votes": [],
      "ranked_choice_winner_info": null,
      "irv_round_summary": [],
      "meek_stv_round_summary": []
    }
  ],
  "comment_results": [
    {
      "prompt": "Any additional comments?",
      "comments": ["Great candidates this year.", "No objections."]
    }
  ]
}
```

**Note**: `ranked_choice_votes`, `irv_round_summary`, and `meek_stv_round_summary` are populated only for the corresponding `poll_type` values (`condorcet_irv`, `instant_runoff_vote`, `meek_stv`). For `generic` polls, only `generic_choice_votes` is populated.

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

## Notes for Developers

1. **ID Format**: LFXv2 uses `_uid` suffix for consistency, but these map to ITX `_id` fields
2. **Choice Structure**: In requests to ITX, choices are sent as string arrays. In responses, they come back as objects with IDs
3. **Authentication**: The proxy handles all Auth0 OAuth2 M2M token management internally
4. **Caching**: M2M tokens are cached and automatically refreshed
5. **Error Translation**: All ITX errors are normalized to LFXv2 error format
6. **Field Mapping**: The proxy automatically maps between `*_uid` and `*_id` fields
