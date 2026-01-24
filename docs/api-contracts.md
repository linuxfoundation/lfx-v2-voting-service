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

### Authentication

| Aspect | LFXv2 Proxy | ITX Service |
|--------|-------------|-------------|
| User Auth | JWT Bearer token (Heimdall) | Not required (proxy handles) |
| Service Auth | Not exposed to client | OAuth2 M2M (Auth0) |
| Header | `Authorization: Bearer <jwt>` | `Authorization: Bearer <m2m-token>` |

---

## POST /api/v1/votes (Create Vote)

### LFXv2 Proxy API

**Endpoint**: `POST /api/v1/votes`

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
  "project_uid": "a09P000000DsCBuIRT",
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
  "project_uid": "a09P000000DsCBuIRT",
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

## GET /api/v1/votes/{vote_uid} (Get Vote)

### LFXv2 Proxy API

**Endpoint**: `GET /api/v1/votes/{vote_uid}`

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
  "project_uid": "a09P000000DsCBuIRT",
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

## PUT /api/v1/votes/{vote_uid} (Update Vote)

### LFXv2 Proxy API

**Endpoint**: `PUT /api/v1/votes/{vote_uid}`

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
  "project_uid": "a09P000000DsCBuIRT",
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
  "project_uid": "a09P000000DsCBuIRT",
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

## DELETE /api/v1/votes/{vote_uid} (Delete Vote)

### LFXv2 Proxy API

**Endpoint**: `DELETE /api/v1/votes/{vote_uid}`

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

## Notes for Developers

1. **ID Format**: LFXv2 uses `_uid` suffix for consistency, but these map to ITX `_id` fields
2. **Choice Structure**: In requests to ITX, choices are sent as string arrays. In responses, they come back as objects with IDs
3. **Authentication**: The proxy handles all Auth0 OAuth2 M2M token management internally
4. **Caching**: M2M tokens are cached and automatically refreshed
5. **Error Translation**: All ITX errors are normalized to LFXv2 error format
6. **Field Mapping**: The proxy automatically maps between `*_uid` and `*_id` fields
