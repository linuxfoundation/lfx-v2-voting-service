# LFX V2 Voting Service

A proxy service for the ITX voting system with LFXv2 authentication and API patterns.

## Overview

This service provides a proxy layer between LFXv2 clients and the legacy ITX voting service. It handles:

- **Authentication**: Validates JWT tokens from LFXv2 clients using Heimdall
- **Service-to-Service Auth**: Uses service account tokens for ITX API calls
- **Terminology Translation**: Maps LFXv2 "vote" terminology to ITX "poll" terminology
- **ID Schema Translation**: Bidirectional mapping between LFXv2 UUIDs and LFXv1 Salesforce IDs via NATS
- **Event Processing**: Real-time sync of v1 voting data to v2 indexer and FGA (see [Event Processing](docs/event-processing.md))
- **Error Handling**: Provides consistent error responses following LFXv2 patterns
- **Logging**: Structured logging with request tracking

## Architecture

- **Framework**: [Goa v3](https://goa.design/) - Design-first API development
- **Authentication**: JWT validation via Heimdall JWKS
- **Proxy Pattern**: HTTP client with service account authentication
- **Clean Architecture**: Domain-driven design with clear layer separation
- **Middleware Stack**: Authorization → Request ID → Request Logger

### Project Structure

```text
lfx-v2-voting-service/
├── api/voting/v1/design/    # Goa DSL API definitions
├── charts/                   # Helm chart for Kubernetes deployment
│   └── lfx-v2-voting-service/
│       ├── templates/        # Kubernetes manifests + Heimdall ruleset
│       ├── values.yaml       # Default Helm values
│       └── values.local.example.yaml  # Local dev values template
├── cmd/voting-api/           # Application entry point
│   ├── eventing/             # Event processing handlers
│   ├── service/              # Request/response converters
│   ├── api.go                # Goa interface implementation (votes)
│   ├── api_votes.go          # Vote handler helpers
│   ├── api_vote_responses.go # Vote response handler implementations
│   └── main.go               # App startup and wiring
├── docs/                     # Documentation
│   ├── api-contracts.md      # LFXv2 ↔ ITX field mappings and examples
│   ├── event-processing.md   # NATS event flow and operational guide
│   ├── glossary.md           # Voting-service-specific terms (ITX, SFID, v1/v2, FGA roles)
│   └── itx-proxy-implementation.md  # Proxy architecture deep-dive
├── gen/                      # Generated Goa code (never edit directly)
├── internal/
│   ├── domain/               # Interfaces and domain models
│   │   └── models/           # Shared domain model types
│   ├── service/              # Business logic layer
│   │   ├── vote_service.go
│   │   └── vote_response_service.go
│   ├── infrastructure/       # External system integrations
│   │   ├── auth/             # JWT authentication (Heimdall)
│   │   ├── eventing/         # NATS event publisher
│   │   ├── idmapper/         # NATS-based v1↔v2 ID mapping
│   │   └── proxy/            # ITX HTTP client
│   ├── middleware/           # HTTP middleware (auth, request ID, logger)
│   └── log/                  # Logging configuration
└── pkg/
    ├── constants/            # Shared constants
    ├── models/itx/           # ITX request/response model types
    └── utils/                # Utility functions
```

## Getting Started

### Prerequisites

- Go 1.24 or later
- Goa CLI: `go install goa.design/goa/v3/cmd/goa@latest`
- ITX service account credentials (see [Getting Dev Credentials](CONTRIBUTING.md#getting-dev-credentials))
- **[lfx-platform Helm chart](https://github.com/linuxfoundation/lfx-v2-helm/tree/main/charts/lfx-platform)** — provides NATS and Heimdall for local development (optional: can be bypassed with env flags)

### Installation

```bash
# Clone the repository
git clone https://github.com/linuxfoundation/lfx-v2-voting-service.git
cd lfx-v2-voting-service

# Install dependencies
make deps

# Generate API code from Goa design
make apigen

# Build the service
make build
```

### Configuration

Configure the service using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `LOG_LEVEL` | Logging level (`debug`, `info`, `warn`, `error`) | `debug` |
| `LOG_ADD_SOURCE` | Add source file/line to logs (`true`, `false`) | `false` |
| `JWKS_URL` | Heimdall JWKS endpoint | `http://heimdall:4457/.well-known/jwks` |
| `AUDIENCE` | JWT audience claim | `lfx-v2-voting-service` |
| `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL` | Any non-empty string disables JWT validation and is used as the mock principal identity | `""` (disabled) |
| `ITX_BASE_URL` | ITX API base URL | `https://api.dev.itx.linuxfoundation.org/` |
| `ITX_AUTH0_DOMAIN` | Auth0 domain for ITX M2M auth | `linuxfoundation-dev.auth0.com` |
| `ITX_CLIENT_ID` | OAuth2 client ID for ITX | **(required)** |
| `ITX_CLIENT_PRIVATE_KEY` | RSA private key in PEM format for JWT assertion | **(required)** |
| `ITX_AUDIENCE` | OAuth2 audience for ITX | `https://api.dev.itx.linuxfoundation.org/` |
| `NATS_URL` | NATS server URL for v1/v2 ID mapping | `nats://nats:4222` |
| `ID_MAPPING_DISABLED` | Disable ID mapping (use for local dev without NATS) | `false` (set to `true` to disable) |

### Event Processing

- `EVENT_PROCESSING_ENABLED` - Enable/disable event processing (default: true)
- `EVENT_CONSUMER_NAME` - JetStream consumer name (default: voting-service-kv-consumer)
- `EVENT_STREAM_NAME` - JetStream stream name (default: KV_v1-objects)
- `EVENT_FILTER_SUBJECT` - NATS subject filter (default: $KV.v1-objects.>)

See [Event Processing Documentation](docs/event-processing.md) for details.

### Local Infrastructure (NATS + Heimdall)

The service depends on NATS (for ID mapping and event processing) and Heimdall (for JWT validation). The easiest way to run both locally is with the [lfx-platform Helm chart](https://github.com/linuxfoundation/lfx-v2-helm/tree/main/charts/lfx-platform):

```bash
kubectl create namespace lfx

# Latest version (may include breaking changes):
helm install -n lfx lfx-platform \
  oci://ghcr.io/linuxfoundation/lfx-v2-helm/chart/lfx-platform

# Pinned version (recommended for reproducible local setup):
helm install -n lfx lfx-platform \
  oci://ghcr.io/linuxfoundation/lfx-v2-helm/chart/lfx-platform \
  --version <version>
```

For available versions, see the [lfx-v2-helm releases](https://github.com/linuxfoundation/lfx-v2-helm/releases).

This deploys NATS, Heimdall, Traefik, OpenFGA, and other platform services into your local Kubernetes cluster. Once running, the default `NATS_URL` in [.env.example](.env.example) (`nats://lfx-platform-nats.lfx.svc.cluster.local:4222`) will connect automatically.

If you prefer to skip NATS and Heimdall entirely, the defaults in [.env.example](.env.example) already set `ID_MAPPING_DISABLED=true`, `EVENT_PROCESSING_ENABLED=false`, and `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL=test-user@example.com` so no cluster is required.

### Running Locally

Copy the example environment file and fill in your ITX credentials (see [Getting Dev Credentials](CONTRIBUTING.md#getting-dev-credentials)):

```bash
cp .env.example .env
# edit .env and set ITX_CLIENT_ID and ITX_CLIENT_PRIVATE_KEY
```

Then source it and run:

```bash
source .env && make run
make debug # or run with debug log level
```

The defaults in [.env.example](.env.example) already set `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL=test-user@example.com`, `ID_MAPPING_DISABLED=true`, and `EVENT_PROCESSING_ENABLED=false` so you don't need a running Heimdall or NATS instance for local development.

The service will start on `http://localhost:8080`

### Running with Docker

```bash
# Build the Docker image
docker build -t lfx-v2-voting-service .

# Run the container
docker run -p 8080:8080 \
  -e ITX_CLIENT_ID=<your-client-id> \
  -e ITX_CLIENT_PRIVATE_KEY="$(cat /path/to/your/private-key.pem)" \
  lfx-v2-voting-service
```

### Deploying with Helm

The service includes a Helm chart for Kubernetes deployment.

#### Prerequisites: Kubernetes Secret

Regardless of which install method you use, first create the `lfx-v2-voting-service` secret in the `lfx` namespace. The `ITX_CLIENT_ID` and `ITX_CLIENT_PRIVATE_KEY` values are in 1Password under the **LFX V2** vault, in the note **LFX V2 Voting Service Env Vars**.

```bash
# Create the namespace if it doesn't exist
kubectl create namespace lfx

kubectl create secret generic lfx-v2-voting-service -n lfx \
  --from-literal=ITX_CLIENT_ID="<client-id-from-1password>" \
  --from-file=ITX_CLIENT_PRIVATE_KEY=/path/to/your/private.key
```

#### Option 1: Install from GHCR (no local build needed)

Use this when you just want to run the service without making code changes:

```bash
make helm-install
```

#### Option 2: Install from Local Build (for active development)

Use this when making code changes and testing them in Kubernetes. The chart uses `pullPolicy: Never` and pulls from the local image `linuxfoundation/lfx-v2-voting-service`.

First, copy the example local values file and fill in any overrides you need:

```bash
cp charts/lfx-v2-voting-service/values.local.example.yaml \
   charts/lfx-v2-voting-service/values.local.yaml
```

Then build the image and install. Re-run `make docker-build` whenever you want to pick up new changes:

```bash
make docker-build
make helm-install-local
```

#### Preview Helm Templates

```bash
# With default values
make helm-templates

# With local values override
make helm-templates-local
```

#### Uninstall

```bash
make helm-uninstall
```

## API Documentation

### API Design Files

The API is defined using [Goa DSL](https://goa.design/) (Design-first approach):

- **API Design**: [`api/voting/v1/design/voting.go`](api/voting/v1/design/voting.go) - Service and method definitions
- **Type Definitions**: [`api/voting/v1/design/types.go`](api/voting/v1/design/types.go) - Request/response types

### Generating OpenAPI/Swagger Documentation

Generate OpenAPI 3.0 specification and Swagger UI:

```bash
# Generate API code and OpenAPI spec
make apigen

# The generated files will be in:
# - gen/http/openapi.json  - OpenAPI 3.0 JSON spec
# - gen/http/openapi.yaml  - OpenAPI 3.0 YAML spec
# - gen/http/openapi3.json - OpenAPI 3.0 JSON spec (alternative)
# - gen/http/openapi3.yaml - OpenAPI 3.0 YAML spec (alternative)
```

### Viewing API Documentation

To view the interactive Swagger UI documentation:

1. **Generate the OpenAPI spec**: `make apigen`
2. **Start the service**: `make run`
3. **Access Swagger UI**: Import `gen/http/openapi.yaml` into [Swagger Editor](https://editor.swagger.io/)

Or browse the deployed docs directly:

- **Dev**: [lfx-api.dev.v2.cluster.linuxfound.info/docs](https://lfx-api.dev.v2.cluster.linuxfound.info/docs/#/)
- **Production**: [lfx-api.v2.cluster.lfx.dev/docs](https://lfx-api.v2.cluster.lfx.dev/docs/#/)

### Available Endpoints

The service provides the following endpoints:

#### Health Checks

- `GET /health` - Service health status
- `GET /livez` - Kubernetes liveness probe
- `GET /readyz` - Kubernetes readiness probe

#### Vote Management (Polls)

- `POST /api/v1/votes` - Create a new vote/poll
- `GET /api/v1/votes/{vote_uid}` - Get vote/poll details
- `PUT /api/v1/votes/{vote_uid}` - Update a vote/poll (only when status is "disabled")
- `DELETE /api/v1/votes/{vote_uid}` - Delete a vote/poll (only when status is "disabled")
- `POST /api/v1/votes/{vote_uid}/extend` - Extend a vote/poll end time
- `POST /api/v1/votes/{vote_uid}/enable` - Enable a vote/poll for voting
- `POST /api/v1/votes/{vote_uid}/bulk_resend` - Bulk resend vote emails to recipients
- `GET /api/v1/votes/{vote_uid}/results` - Get aggregated vote results

#### Vote Responses (Ballot Submissions)

- `POST /api/v1/vote_response/{vote_response_uid}` - Submit a vote response
- `GET /api/v1/vote_response/{vote_response_uid}` - Get vote response details
- `PUT /api/v1/vote_response/{vote_response_uid}` - Update a vote response
- `POST /api/v1/vote_response/{vote_response_uid}/resend` - Resend vote email

For detailed request/response schemas, authentication requirements, and examples, refer to the generated OpenAPI specification or the Goa design files.

## Development

### Running Tests

```bash
make test
```

### Regenerating API Code

After modifying files in `api/voting/v1/design/`:

```bash
make apigen
```

### Adding New Endpoints

1. **Define the endpoint** in `api/voting/v1/design/voting.go`
2. **Add types** in `api/voting/v1/design/types.go` if needed
3. **Regenerate code**: `make apigen`
4. **Add proxy method** in `internal/infrastructure/proxy/client.go`
5. **Add service method** in `internal/service/voting_service.go`
6. **Add converters** in `cmd/voting-api/service/` (request and response)
7. **Implement API handler** in `cmd/voting-api/api.go`

### Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- All exported functions/types must have comments
- Domain errors use the `ErrorType` enum pattern

## Monitoring and Observability

### Logging

The service uses structured logging (`slog`) with the following context:

- Request ID (X-REQUEST-ID header)
- HTTP method and path
- User agent and remote address
- JWT principal (user ID)
- Response status and duration

### Metrics

Health check endpoints are available for monitoring:

- `/health` - Returns 200 if service is running
- `/livez` - Kubernetes liveness probe
- `/readyz` - Kubernetes readiness probe

## Security

- **JWT Authentication**: All endpoints require valid JWT tokens from Heimdall
- **OAuth2 M2M Authentication**: ITX calls use OAuth2 client credentials flow with JWT assertion (private key) for enhanced security, with automatic token caching and renewal
- **HTTPS**: Use HTTPS in production (configure via reverse proxy)
- **Secrets Management**: Store `ITX_CLIENT_ID` and `ITX_CLIENT_PRIVATE_KEY` securely (e.g., Kubernetes secrets, AWS Secrets Manager)

## License

MIT License - See [LICENSE](LICENSE) file for details.

## Contributing

Please open an issue or pull request on GitHub for details on our code of conduct and the process for submitting pull requests.

## Support

For issues and questions:

- Create an issue in the GitHub repository
- Contact the LFX team at <support@linuxfoundation.org>
