# Contributing to LFX V2 Voting Service

Contributions are what make the open-source community such an amazing place to learn, inspire, and create.

Thank you for your interest in contributing to the LFX V2 Voting Service! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Getting Dev Credentials](#getting-dev-credentials)
- [License Headers](#license-headers)
- [Code Style](#code-style)
- [Architecture Guidelines](#architecture-guidelines)
- [Commit Messages](#commit-messages)
- [Pull Request Process](#pull-request-process)
- [Testing](#testing)

## Code of Conduct

By participating in this project, you agree to abide by the [Linux Foundation Code of Conduct](https://www.linuxfoundation.org/code-of-conduct/).

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Create a new branch for your feature or bug fix
4. Make your changes
5. Push your changes to your fork
6. Submit a pull request

## Development Setup

See [README.md — Getting Started](README.md#getting-started) for full setup instructions: prerequisites, installation, environment variables, running locally, and optional NATS + Heimdall infrastructure via the lfx-platform Helm chart.

## Getting Dev Credentials

The service requires ITX OAuth2 credentials (`ITX_CLIENT_ID` and `ITX_CLIENT_PRIVATE_KEY`). Find them in:

> **1Password** → Linux Foundation org → **LFX V2** vault → **LFX V2 Voting Service Env Vars** (secure note)

- The private key is an RSA key in PEM format
- Store the key locally at `tmp/local.private.key` (gitignored) and reference it with `$(cat tmp/local.private.key)`

For local development, you can bypass NATS and JWT auth entirely using the flags shown in the Quick Start above. The only credential you truly need to make proxy calls to the ITX dev environment is `ITX_CLIENT_ID` and `ITX_CLIENT_PRIVATE_KEY`.

## License Headers

**IMPORTANT**: All source code files must include the appropriate license header. This is enforced by our CI/CD pipeline.

### Required Format

The license header must appear at the top of every source file and must contain:

```text
Copyright The Linux Foundation and each contributor to LFX.
SPDX-License-Identifier: MIT
```

### File Type Examples

#### Go Files (.go)

```go
// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package yourpackage
```

#### YAML Files (.yml, .yaml)

```yaml
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
```

#### Makefile / Shell Scripts

```bash
# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT
```

### Automated Checks

- **CI Pipeline**: GitHub Actions verifies all files have proper headers on every pull request (excludes `gen/*` and `cmd/voting-api/kodata/*`)

## Code Style

### General Guidelines

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` / `goimports` for formatting — run `make fmt` before committing
- All exported functions and types must have doc comments
- Domain errors use the `ErrorType` enum pattern (see `internal/domain/`)
- Use structured logging (`slog`) — never `fmt.Println` or `log.Printf`

### Linting

The project uses `golangci-lint`. Run linting before committing:

```bash
# Run linter
make lint

# Check formatting + lint without modifying files
make check
```

The linter config is in [revive.toml](revive.toml) and `.golangci.yml`.

## Architecture Guidelines

### Respect the Layer Separation

This service uses clean architecture with clear layer boundaries. Understand them before making changes:

- **`api/voting/v1/design/`** — Goa DSL only; no business logic
- **`internal/domain/`** — interfaces and domain models; no external dependencies
- **`internal/service/`** — business logic; depends only on domain interfaces
- **`internal/infrastructure/`** — external integrations (ITX proxy, NATS, auth)
- **`cmd/voting-api/`** — wiring, converters, and Goa interface implementation

Dependencies flow inward. Infrastructure depends on domain, not the other way around.

### Adding New Endpoints

Follow the checklist in the README's [Adding New Endpoints](README.md#adding-new-endpoints) section — it covers the full Goa → service → proxy chain.

### Fixing Problems at the Source

When something doesn't work, fix the root cause. Disabling auth, bypassing linting with `//nolint` without explanation, or adding build hacks are not fixes. `TODO: TEMPORARY` bypasses have a tendency to reach production.

### Architectural Changes Need Their Own PRs

Changes that affect the entire application — middleware, NATS consumer configuration, new infrastructure integrations — are architectural decisions. They need standalone PRs with focused review, not bundled inside feature work.

## Commit Messages

### Format

Follow the conventional commit format:

```text
type(scope): subject

body

footer
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Examples

```text
feat(votes): add bulk resend endpoint

Implements POST /api/v1/votes/{vote_uid}/bulk_resend to proxy the
ITX bulk email resend call.

Closes #42
```

### Sign-off

All commits must be signed off per the [Developer Certificate of Origin](https://developercertificate.org/):

```bash
git commit --signoff
```

This adds a `Signed-off-by` line to your commit message.

## Pull Request Process

### Branch Naming

Use the format `<type>/<ticket-or-description>`, for example:

- `feat/LFXV2-123-add-bulk-resend`
- `fix/LFXV2-456-fix-id-mapping`
- `chore/update-dependencies`

### PR Scope

- **Keep PRs focused on a single concern** — a feature PR should contain only the feature
- **Architectural decisions require their own PR** — changes to middleware, infrastructure wiring, or NATS configuration need standalone discussion and approval
- **Never mix security changes with feature work** — auth middleware modifications must be reviewed independently

### PR Checklist

1. **Update Documentation**: Update README or docs/ for any new features or configuration changes
2. **Add Tests**: Include unit tests for new functionality
3. **Pass All Checks**: Ensure `make check` and `make test` pass locally
4. **License Headers**: Verify all new files have the correct license header
5. **API Generation**: If you modified Goa design files, run `make apigen` and commit the regenerated files
6. **Clear Description**: Provide a clear description of changes and link any related issues or Jira tickets

### PR Title Format

Use the same conventional commit format for PR titles:

```text
feat(votes): add bulk resend endpoint
```

## Testing

### Running Tests

```bash
# Run all tests
make test
```

### Test Requirements

- All new features must include unit tests
- Tests must pass with `-race` flag (already enforced by `make test`)
- Mock external dependencies — tests should not require a live ITX or NATS connection

## Questions?

If you have questions about contributing, please:

1. Check existing issues and discussions on GitHub
2. Open a new issue for clarification
3. Contact the LFX team at <support@linuxfoundation.org>

Thank you for contributing to LFX V2 Voting Service!
