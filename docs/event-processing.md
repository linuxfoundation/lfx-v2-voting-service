# Event Processing

## Overview

The voting service implements NATS KV bucket event processing to automatically sync vote (poll) and vote response data from the v1 system to the v2 system. This enables real-time data synchronization, search indexing, and access control updates without manual intervention.

## Architecture

### Components

```
┌─────────────────┐
│  v1 DynamoDB    │
│   (via KV)      │
└────────┬────────┘
         │
         ├─ itx-poll.*
         └─ itx-poll-vote.*
         │
         v
┌─────────────────────────────────┐
│   NATS KV Bucket: v1-objects    │
└────────┬────────────────────────┘
         │
         v
┌─────────────────────────────────┐
│     Event Processor             │
│  (JetStream Consumer)           │
└────────┬────────────────────────┘
         │
         ├─ Transform v1 → v2
         ├─ Map IDs (v1 SFID → v2 UUID)
         │
         v
┌────────────────┬────────────────┐
│                │                │
│   Indexer      │   FGA-Sync     │
│   Service      │   Service      │
│                │                │
│ (Search Index) │ (Access Control)
└────────────────┴────────────────┘
```

### Event Flow

1. **Watch**: Event processor watches the `v1-objects` KV bucket for keys matching:
   - `itx-poll.*` - Vote (poll) data
   - `itx-poll-vote.*` - Vote response data

2. **Transform**: Converts v1 format to v2 format:
   - String fields → proper types (strings to ints)
   - v1 SFIDs → v2 UUIDs (committees, projects)
   - `poll_id` → `vote_uid` (terminology translation)
   - `vote_id` → `vote_response_uid`
   - Ranked choice answers: string `choice_rank` → int

3. **Publish**: Sends transformed data to two downstream services:
   - **Indexer Service** (`lfx.index.vote`, `lfx.index.vote_response`)
     - Enables search functionality
     - Includes parent references (committee, project)
     - Provides access control metadata

   - **FGA-Sync Service** (`lfx.fga-sync.update_access`, `lfx.fga-sync.delete_access`)
     - Updates Fine-Grained Authorization (FGA) tuples
     - Manages viewer/writer/auditor permissions
     - Links votes to committees and projects

4. **Track**: Records processed events in `v1-mappings` KV bucket for deduplication

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EVENT_PROCESSING_ENABLED` | `true` | Enable/disable event processing |
| `EVENT_CONSUMER_NAME` | `voting-service-kv-consumer` | JetStream consumer name |
| `EVENT_STREAM_NAME` | `KV_v1-objects` | JetStream stream name |
| `EVENT_FILTER_SUBJECT` | `$KV.v1-objects.>` | NATS subject filter pattern |
| `NATS_URL` | `nats://nats:4222` | NATS server URL |

### Consumer Configuration

- **Delivery Policy**: `DeliverLastPerSubjectPolicy` - Processes the latest version of each key
- **Ack Policy**: `AckExplicitPolicy` - Requires explicit acknowledgment
- **Max Deliver**: `3` - Retries transient failures up to 3 times
- **Ack Wait**: `30s` - Timeout before message redelivery
- **Max Ack Pending**: `1000` - Maximum unacknowledged messages

## Data Transformation

### Vote (Poll) Data

**v1 Format (DynamoDB/KV)**:
- All numeric fields stored as strings (e.g., `"total_voting_request_invitations": "42"`)
- v1 SFIDs for committees and projects
- Key: `itx-poll.{poll_id}`

**v2 Format (Transformed)**:
- Proper types (integers, booleans)
- v2 UUIDs for committees and projects
- Mapped via IDMapper service
- `poll_id` → `vote_uid` (terminology translation)

**Example Transformation**:
```json
// v1 Input
{
  "poll_id": "poll-123",
  "name": "Board Election 2024",
  "project_id": "a094V00000A1XyzQAF",        // v1 SFID
  "committee_id": "a094V00000A1BcdQAF",      // v1 SFID
  "total_voting_request_invitations": "100", // String
  "num_response_received": "42",             // String
  "num_winners": "3",                        // String
  "status": "active"
}

// v2 Output
{
  "vote_uid": "poll-123",
  "poll_id": "poll-123",
  "name": "Board Election 2024",
  "project_uid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",  // v2 UUID
  "committee_uid": "550e8400-e29b-41d4-a716-446655440000", // v2 UUID
  "total_voting_request_invitations": 100,                // Integer
  "num_response_received": 42,                            // Integer
  "num_winners": 3,                                       // Integer
  "status": "active"
}
```

### Vote Response Data

**v1 Format**:
- Key: `itx-poll-vote.{vote_id}`
- String-based ranked choice ranks
- v1 references (`poll_id`, `project_id`)

**v2 Format**:
- `vote_id` → `uid` (primary key)
- `poll_id` → `vote_uid` (parent reference)
- Integer ranked choice ranks
- Mapped project UIDs
- User-specific permissions (writer/viewer)

**Example Ranked Choice Transformation**:
```json
// v1 Input
{
  "vote_id": "vote-response-123",
  "poll_id": "poll-456",
  "project_id": "a094V00000A1XyzQAF",
  "poll_answers": [{
    "question_id": "q1",
    "type": "ranked_choice",
    "ranked_user_choice": [
      {
        "choice_id": "c1",
        "choice_text": "Candidate A",
        "choice_rank": "1"  // String in v1
      },
      {
        "choice_id": "c2",
        "choice_text": "Candidate B",
        "choice_rank": "2"  // String in v1
      }
    ]
  }]
}

// v2 Output
{
  "uid": "vote-response-123",
  "vote_uid": "poll-456",
  "project_uid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
  "poll_answers": [{
    "question_id": "q1",
    "type": "ranked_choice",
    "ranked_user_choice": [
      {
        "choice_id": "c1",
        "choice_text": "Candidate A",
        "choice_rank": 1  // Integer in v2
      },
      {
        "choice_id": "c2",
        "choice_text": "Candidate B",
        "choice_rank": 2  // Integer in v2
      }
    ]
  }]
}
```

## Error Handling

### Transient Errors (Retry)
These errors trigger NAK (negative acknowledgment) for automatic retry:
- NATS connection timeouts
- IDMapper service unavailable
- Network failures
- Temporary downstream service outages

**Action**: Message redelivered up to `MaxDeliver` times (3 attempts)

### Permanent Errors (Skip)
These errors trigger ACK to skip and move on:
- Invalid JSON structure
- Missing required fields (e.g., empty `poll_id` or `vote_id`)
- No parent references (vote/response orphaned)
- Malformed data
- JSON unmarshaling errors

**Action**: Log warning and continue processing other messages

### ID Mapping Failures
When v1→v2 ID mapping fails:
- Log warning with v1 SFID
- Skip setting v2 UID for that reference
- Continue processing with remaining valid data
- Vote/response may be skipped if critical references (project_uid) are missing

## Operations

### Starting the Service

Event processing starts automatically when the service starts:

```bash
# Default (event processing enabled)
./voting-api

# Explicitly enable
EVENT_PROCESSING_ENABLED=true ./voting-api

# Disable event processing
EVENT_PROCESSING_ENABLED=false ./voting-api
```

### Monitoring

**Log Messages**:
```
INFO  Event processing is ENABLED - initializing event processor
INFO  Event processor started in background
INFO  processing vote update key=itx-poll.poll-123
INFO  successfully sent vote indexer and access messages vote_id=poll-123
INFO  processing vote response update key=itx-poll-vote.vote-response-123
INFO  successfully sent vote response indexer and access messages vote_response_id=vote-response-123
```

**Consumer Status**:
```bash
# Check consumer status
nats consumer info KV_v1-objects voting-service-kv-consumer

# View consumer metrics
nats consumer report KV_v1-objects voting-service-kv-consumer
```

### Lifecycle

1. **Startup**: Event processor initializes after IDMapper
2. **Running**: Processes events in background goroutine
3. **Shutdown**: Graceful shutdown sequence:
   - Context cancellation stops the consumer
   - Consumer drains pending messages (30s timeout)
   - NATS connection closed
   - HTTP server shutdown

### Troubleshooting

**No events processing**:
- Check `EVENT_PROCESSING_ENABLED=true`
- Verify NATS connection: `NATS_URL`
- Check consumer exists: `nats consumer ls KV_v1-objects`
- Verify `v1-mappings` KV bucket exists

**Events failing repeatedly**:
- Check logs for permanent errors
- Verify IDMapper service is running
- Confirm indexer and FGA-sync services are available
- Check NATS JetStream is healthy

**Duplicate processing**:
- Check `v1-mappings` KV bucket for tracking entries
- Verify consumer name is unique per instance
- Ensure `DeliverLastPerSubjectPolicy` is configured

**ID mapping failures**:
- Ensure IDMapper service has v1↔v2 mappings populated
- Check project/committee references exist in v1 system
- Verify NATS request-reply timeout settings

## Deduplication

The service uses the `v1-mappings` KV bucket to track processed events:

**Key Pattern**:
- Votes: `vote.{poll_id}`
- Vote Responses: `vote_response.{vote_id}`

**Value**: Revision number (incremented on updates)

**Logic**:
- If mapping exists → **UPDATE** operation
- If mapping missing → **CREATE** operation
- After processing → Store/update mapping entry

This ensures:
- First event creates the resource
- Subsequent events update the resource
- No duplicate resources in downstream services

## Performance Considerations

**Concurrency**:
- Single consumer per service instance
- Messages processed sequentially per consumer
- Multiple service instances = parallel processing (shared consumer group)

**Throughput**:
- `MaxAckPending=1000` allows up to 1000 in-flight messages
- Adjust based on processing speed and resource availability
- Average processing time: <100ms per message

**Backpressure**:
- Consumer automatically pauses when `MaxAckPending` reached
- Resumes when pending count drops below threshold
- NATS handles queuing automatically

**Resource Usage**:
- Event processor runs in background goroutine (low overhead)
- NATS connection shared with IDMapper
- Memory footprint minimal (streaming model)
- CPU usage scales with event volume

## Related Services

### IDMapper Service
- Maps v1 SFIDs ↔ v2 UUIDs
- Required for event processing
- Queries via NATS request-reply pattern
- Caches mappings for performance

### Indexer Service
- Receives transformed vote/response data
- Indexes in OpenSearch for search functionality
- Handles `ActionCreated`, `ActionUpdated`, `ActionDeleted`
- Subjects: `lfx.index.vote`, `lfx.index.vote_response`

### FGA-Sync Service
- Receives access control updates
- Manages OpenFGA authorization tuples
- Links resources to parent entities (committees, projects)
- Subjects: `lfx.fga-sync.update_access`, `lfx.fga-sync.delete_access`

## Development

### Testing Event Processing

1. **Disable in local development**:
   ```bash
   export EVENT_PROCESSING_ENABLED=false
   ```

2. **Watch consumer activity**:
   ```bash
   nats consumer next KV_v1-objects voting-service-kv-consumer --count 10
   ```

3. **Trigger test event**:
   ```bash
   # Put test vote in v1-objects KV
   nats kv put v1-objects itx-poll.test-123 '{
     "poll_id": "test-123",
     "name": "Test Vote",
     "project_id": "a094V00000TestSFID",
     "status": "active",
     "total_voting_request_invitations": "10",
     "poll_questions": []
   }'

   # Put test vote response
   nats kv put v1-objects itx-poll-vote.vote-response-123 '{
     "vote_id": "vote-response-123",
     "poll_id": "test-123",
     "project_id": "a094V00000TestSFID",
     "user_id": "user-123",
     "poll_answers": []
   }'
   ```

4. **Check processing logs**:
   ```bash
   # Look for processing messages
   grep "processing vote update" logs/voting-api.log
   grep "successfully sent vote" logs/voting-api.log
   ```

5. **Verify mappings stored**:
   ```bash
   nats kv get v1-mappings vote.test-123
   nats kv get v1-mappings vote_response.vote-response-123
   ```

### Unit Tests

The event processing code includes comprehensive unit tests:

```bash
# Run all tests
make test

# Run only event processing tests
go test ./cmd/voting-api/eventing/... -v

# Run with coverage
go test ./cmd/voting-api/eventing/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

**Test Coverage**:
- Event processor lifecycle (Start/Stop)
- KV message handling and routing
- Vote event handlers (create/update/delete)
- Vote response event handlers
- Data transformations (v1 to v2)
- Error handling (transient vs permanent)

### Code Structure

```
cmd/voting-api/eventing/
├── event_processor.go                 # Lifecycle management
├── event_processor_test.go            # Lifecycle tests
├── kv_handler.go                      # Event routing by key prefix
├── kv_handler_test.go                 # Routing tests
├── vote_event_handler.go              # Vote transformation logic
├── vote_event_handler_test.go         # Vote tests
├── vote_response_event_handler.go     # Response transformation logic
└── vote_response_event_handler_test.go # Response tests

internal/domain/
├── event_models.go                    # v1 and v2 data models
└── event_publisher.go                 # Publisher interface

internal/infrastructure/eventing/
├── event_config.go                    # Configuration structs
└── nats_publisher.go                  # NATS publishing implementation
```

### Adding New Event Types

To add processing for new entity types:

1. Create handler in `cmd/voting-api/eventing/{entity}_event_handler.go`
2. Add routing logic in `kv_handler.go`
3. Define v2 model in `internal/domain/event_models.go`
4. Add publisher method in `internal/infrastructure/eventing/nats_publisher.go`
5. Create test file `{entity}_event_handler_test.go`
6. Update documentation

## Message Formats

### Indexer Message (Create/Update)

```json
{
  "action": "created",
  "headers": {
    "authorization": "Bearer voting-service"
  },
  "data": {
    "vote_uid": "poll-123",
    "name": "Board Election 2024",
    "project_uid": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
    "committee_uid": "550e8400-e29b-41d4-a716-446655440000",
    "status": "active"
  },
  "indexing_config": {
    "object_id": "poll-123",
    "access_check_object": "vote:poll-123",
    "access_check_relation": "viewer",
    "history_check_object": "vote:poll-123",
    "history_check_relation": "auditor",
    "sort_name": "Board Election 2024",
    "name_and_aliases": ["Board Election 2024"],
    "parent_refs": ["project:6ba7b810-9dad-11d1-80b4-00c04fd430c8", "committee:550e8400-e29b-41d4-a716-446655440000"],
    "fulltext": "Board Election 2024 ...",
    "public": false
  }
}
```

### Indexer Message (Delete)

```json
{
  "action": "deleted",
  "headers": {
    "authorization": "Bearer voting-service"
  },
  "data": "poll-123",
  "indexing_config": {
    "object_id": "poll-123",
    "access_check_object": "vote:poll-123",
    "access_check_relation": "viewer"
  }
}
```

### FGA Update Access Message

```json
{
  "object_type": "vote",
  "operation": "update_access",
  "data": {
    "uid": "poll-123",
    "public": false,
    "references": {
      "project": ["6ba7b810-9dad-11d1-80b4-00c04fd430c8"],
      "committee": ["550e8400-e29b-41d4-a716-446655440000"]
    }
  }
}
```

### FGA Delete Access Message

```json
{
  "object_type": "vote",
  "operation": "delete_access",
  "data": {
    "uid": "poll-123"
  }
}
```

## References

- [NATS JetStream Documentation](https://docs.nats.io/nats-concepts/jetstream)
- [NATS KV Store Guide](https://docs.nats.io/nats-concepts/jetstream/key-value-store)
- [OpenFGA Authorization](https://openfga.dev/)
- [Survey Service Event Processing](https://github.com/linuxfoundation/lfx-v2-survey-service/blob/main/docs/event-processing.md) - Similar implementation
- [Voting Service PR #8](https://github.com/linuxfoundation/lfx-v2-voting-service/pull/8) - Initial implementation
