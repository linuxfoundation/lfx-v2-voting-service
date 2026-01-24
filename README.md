# LFX V2 Voting Service

A proxy service for the ITX voting system with LFXv2 authentication and API patterns.

## Overview

This service provides a proxy layer between LFXv2 clients and the legacy ITX voting service. It handles:

- **Authentication**: Validates JWT tokens from LFXv2 clients using Heimdall
- **Service-to-Service Auth**: Uses service account tokens for ITX API calls
- **Terminology Translation**: Maps LFXv2 "vote" terminology to ITX "poll" terminology
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
├── gen/                      # Generated Goa code (auto-generated)
├── cmd/voting-api/           # Application entry point
│   ├── service/              # Request/response converters
│   ├── api.go                # API layer (Goa interface implementation)
│   └── main.go               # Main application
├── internal/
│   ├── domain/               # Domain models and interfaces
│   ├── service/              # Business logic layer
│   ├── infrastructure/       # External integrations
│   │   ├── auth/             # JWT authentication
│   │   └── proxy/            # ITX HTTP client
│   ├── middleware/           # HTTP middleware
│   └── log/                  # Logging configuration
└── pkg/
    ├── constants/            # Shared constants
    └── utils/                # Utility functions
```

## Getting Started

### Prerequisites

- Go 1.24 or later
- Goa CLI: `go install goa.design/goa/v3/cmd/goa@latest`
- Access to Heimdall JWKS endpoint
- ITX service account token

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
| `JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL` | Mock principal for local dev (disables auth) | `""` |
| `ITX_BASE_URL` | ITX API base URL | `https://api.dev.itx.linuxfoundation.org/` |
| `ITX_AUTH0_DOMAIN` | Auth0 domain for ITX M2M auth | `linuxfoundation-dev.auth0.com` |
| `ITX_CLIENT_ID` | OAuth2 client ID for ITX | **(required)** |
| `ITX_CLIENT_SECRET` | OAuth2 client secret for ITX | **(required)** |
| `ITX_AUDIENCE` | OAuth2 audience for ITX | `https://api.dev.itx.linuxfoundation.org/` |

### Running Locally

```bash
# Set required OAuth2 credentials for ITX
export ITX_CLIENT_ID=<your-oauth2-client-id>
export ITX_CLIENT_SECRET=<your-oauth2-client-secret>

# For local development without JWT auth
export JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL=test-user@example.com

# Run the service
make run
make debug # or run with debug log level
```

The service will start on <http://localhost:8080>

### Running with Docker

```bash
# Build the Docker image
docker build -t lfx-v2-voting-service .

# Run the container
docker run -p 8080:8080 \
  -e ITX_CLIENT_ID=<your-client-id> \
  -e ITX_CLIENT_SECRET=<your-client-secret> \
  lfx-v2-voting-service
```

### Deploying with Helm

The service includes Helm charts for Kubernetes deployment.

#### Install with production values

```bash
make helm-install
```

#### Install with local development values

```bash
make helm-install-local
```

#### Preview Helm templates

```bash
# With production values
make helm-templates

# With local values
make helm-templates-local
```

#### Uninstall

```bash
make helm-uninstall
```

#### Helm Configuration

- **Chart location**: `charts/lfx-v2-voting-service/`
- **Production values**: `charts/lfx-v2-voting-service/values.yaml`
- **Local values**: `charts/lfx-v2-voting-service/values.local.yaml`

The Helm chart includes:

- Kubernetes Deployment with health checks
- ClusterIP Service
- ServiceAccount with RBAC
- HTTPRoute (Gateway API) for Traefik ingress
- Heimdall middleware for authentication
- Heimdall RuleSet for authorization rules
- OpenFGA integration (optional)

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
3. **Access Swagger UI**: Use a tool like [Swagger Editor](https://editor.swagger.io/) and import `gen/http/openapi.yaml`

Alternatively, you can serve the Swagger UI locally:

```bash
# Using npx and swagger-ui-watcher
npx swagger-ui-watcher gen/http/openapi.yaml
```

### Available Endpoints

The service provides the following endpoints:

- **Health Checks**: `/health`, `/livez`, `/readyz`
- **Voting Operations**: `POST`, `GET`, `PUT`, `DELETE` operations on `/api/v1/votes`

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
- **OAuth2 M2M Authentication**: ITX calls use OAuth2 client credentials flow with automatic token caching and renewal
- **HTTPS**: Use HTTPS in production (configure via reverse proxy)
- **Secrets Management**: Store `ITX_CLIENT_ID` and `ITX_CLIENT_SECRET` securely (e.g., Kubernetes secrets)

## License

MIT License - See [LICENSE](LICENSE) file for details.

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## Support

For issues and questions:

- Create an issue in the GitHub repository
- Contact the LFX team at <support@linuxfoundation.org>
