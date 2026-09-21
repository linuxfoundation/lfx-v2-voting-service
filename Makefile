# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT

.PHONY: all help setup deps apigen generate verify build run debug test test-cover lint fmt check tidy ci clean clean-bin docker-build helm-install helm-install-local helm-templates helm-templates-local helm-uninstall

# Default target
all: clean deps apigen fmt lint test build

# Docker configuration
DOCKER_IMAGE=linuxfoundation/lfx-v2-voting-service
DOCKER_TAG=latest

# Helm configuration
HELM_CHART_PATH=./charts/lfx-v2-voting-service
HELM_RELEASE_NAME=lfx-v2-voting-service
HELM_NAMESPACE=lfx
HELM_VALUES_FILE=./charts/lfx-v2-voting-service/values.local.yaml

# Go files
GO_FILES=$(shell find . -name "*.go" -not -path "./gen/*" -not -path "./vendor/*")

# Help target
help:
	@echo "LFX V2 Voting Service — available make targets"
	@echo ""
	@echo "First-time setup:"
	@echo "  setup          - Copy .env.example → .env (if .env does not exist)"
	@echo "  deps           - Install goa CLI, golangci-lint, and download Go modules"
	@echo ""
	@echo "Core workflow:"
	@echo "  generate       - Regenerate API code from Goa design files (alias: apigen)"
	@echo "  build          - Build binary to bin/voting-api"
	@echo "  run            - Build and run the service (requires env vars)"
	@echo "  debug          - Build and run with LOG_LEVEL=debug"
	@echo ""
	@echo "Testing:"
	@echo "  test           - Run all tests with -race and -timeout 5m"
	@echo "  test-cover     - Run tests with coverage; write coverage.out"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt            - Format all Go source files in place"
	@echo "  lint           - Run golangci-lint"
	@echo "  check          - Check formatting and lint without modifying files"
	@echo "  tidy           - Run go mod tidy"
	@echo ""
	@echo "Validation:"
	@echo "  verify         - Regenerate API code and fail if gen/ changed (CI use)"
	@echo "  ci             - Full pre-submit check: verify + check + build + test"
	@echo ""
	@echo "Cleanup:"
	@echo "  clean          - Remove gen/ and bin/ (run 'make generate' after)"
	@echo "  clean-bin      - Remove bin/ only (preserves gen/)"
	@echo ""
	@echo "Container / Kubernetes:"
	@echo "  docker-build          - Build Docker image"
	@echo "  helm-install          - Install Helm chart with default values"
	@echo "  helm-install-local    - Install Helm chart with values.local.yaml"
	@echo "  helm-templates        - Render Helm templates with default values"
	@echo "  helm-templates-local  - Render Helm templates with values.local.yaml"
	@echo "  helm-uninstall        - Uninstall Helm chart"

# First-time setup: create .env from .env.example if it doesn't exist
setup:
	@if [ ! -f .env ]; then \
		echo "==> Creating .env from .env.example..."; \
		cp .env.example .env; \
		echo "==> .env created. Set ITX_CLIENT_ID and ITX_CLIENT_PRIVATE_KEY before running."; \
		echo "==> See CONTRIBUTING.md#getting-dev-credentials for instructions."; \
	else \
		echo "==> .env already exists, skipping. Delete it first to regenerate."; \
	fi

# Install dependencies
deps:
	@echo "==> Installing dependencies..."
	@echo "==> Installing goa CLI (version pinned to go.mod)..."
	@go install goa.design/goa/v3/cmd/goa@$(shell go list -m -f '{{.Version}}' goa.design/goa/v3)
	@echo "==> Installing golangci-lint v2.12.2..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	@echo "==> Downloading Go modules..."
	@go mod download

apigen:
	@echo "==> Generating API code from Goa design..."
	$(shell go env GOPATH)/bin/goa gen github.com/linuxfoundation/lfx-v2-voting-service/api/voting/v1/design
	@echo "==> API generation complete"

# Alias for apigen — preferred name for documentation and agent use
generate: apigen

build:
	@echo "==> Building voting service..."
	go build -o bin/voting-api ./cmd/voting-api

test:
	@echo "==> Running tests..."
	go test ./... -v -race -timeout 5m

test-cover:
	@echo "==> Running tests with coverage..."
	go test ./... -v -race -timeout 5m -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	@echo "==> Coverage report written to coverage.out"
	@echo "==> Open HTML report: go tool cover -html=coverage.out"

run: build
	@echo "==> Running voting service..."
	./bin/voting-api

debug: build
	@echo "==> Running voting service in debug mode..."
	LOG_LEVEL=debug ./bin/voting-api

clean:
	@echo "==> Removing gen/ and bin/ (run 'make generate' to regenerate API code)..."
	rm -rf gen/
	rm -rf bin/

clean-bin:
	@echo "==> Removing bin/ (gen/ preserved)..."
	rm -rf bin/

# Run linter
lint:
	@echo "==> Running linter..."
	$(shell go env GOPATH)/bin/golangci-lint run ./...

# Format code
fmt:
	@echo "==> Formatting code..."
	@go fmt ./...
	@gofmt -s -w $(GO_FILES)

# Check formatting and linting without modifying files
check:
	@echo "==> Checking code format..."
	@if [ -n "$$(gofmt -l $(GO_FILES))" ]; then \
		echo "The following files need formatting:"; \
		gofmt -l $(GO_FILES); \
		exit 1; \
	fi
	@echo "==> Code format check passed"
	@$(MAKE) lint

# Tidy Go module dependencies
tidy:
	@echo "==> Tidying Go modules..."
	go mod tidy

# Full pre-submit check: mirrors what CI validates.
# Run this before opening a pull request.
ci: verify check build test
	@echo "==> All CI checks passed"

# Verify that generated code is up to date
verify: apigen
	@echo "==> Verifying generated code is up to date..."
	@if [ -n "$$(git status --porcelain gen/)" ]; then \
		echo "Generated code is out of date. Run 'make apigen' and commit the changes."; \
		git status --porcelain gen/; \
		exit 1; \
	fi
	@echo "==> Generated code is up to date"

# Docker targets
docker-build:
	@echo "==> Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) -f ./Dockerfile .
	@echo "==> Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

# Helm targets
helm-install:
	@echo "==> Installing Helm chart with default values..."
	helm upgrade --force --install $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) --create-namespace

helm-install-local:
	@echo "==> Installing Helm chart with local values..."
	helm upgrade --force --install $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--values $(HELM_VALUES_FILE)

helm-templates:
	@echo "==> Rendering Helm templates with default values..."
	helm template $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE)

helm-templates-local:
	@echo "==> Rendering Helm templates with local values..."
	helm template $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) \
		--values $(HELM_VALUES_FILE)

helm-uninstall:
	@echo "==> Uninstalling Helm chart..."
	helm uninstall $(HELM_RELEASE_NAME) --namespace $(HELM_NAMESPACE)
