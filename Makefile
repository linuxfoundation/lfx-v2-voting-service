.PHONY: all help deps apigen build test clean run debug lint fmt check verify docker-build helm-install helm-install-local helm-templates helm-templates-local helm-uninstall

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
	@echo "Available targets:"
	@echo "  all            - Run clean, deps, apigen, fmt, lint, test, and build"
	@echo "  deps           - Install dependencies including goa CLI and golangci-lint"
	@echo "  apigen         - Generate API code from design files"
	@echo "  build          - Build the binary"
	@echo "  run            - Run the service"
	@echo "  debug          - Run the service with debug logging"
	@echo "  test           - Run unit tests"
	@echo "  clean          - Remove generated files and binaries"
	@echo "  lint           - Run golangci-lint"
	@echo "  fmt            - Format Go code"
	@echo "  check          - Run fmt and lint without modifying files"
	@echo "  verify         - Verify API generation is up to date"
	@echo "  docker-build   - Build Docker image"
	@echo "  helm-install   - Install Helm chart"
	@echo "  helm-install-local - Install Helm chart with local values file"
	@echo "  helm-templates   - Print templates for Helm chart"
	@echo "  helm-templates-local - Print templates for Helm chart with local values file"
	@echo "  helm-uninstall - Uninstall Helm chart"

# Install dependencies
deps:
	@echo "==> Installing dependencies..."
	@command -v goa >/dev/null 2>&1 || { \
		echo "==> Installing goa CLI..."; \
		go install goa.design/goa/v3/cmd/goa@latest; \
	}
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "==> Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	}
	@echo "==> Downloading Go modules..."
	@go mod download

apigen:
	@echo "==> Generating API code from Goa design..."
	goa gen github.com/linuxfoundation/lfx-v2-voting-service/api/voting/v1/design
	@echo "==> API generation complete"

build:
	@echo "==> Building voting service..."
	go build -o bin/voting-api ./cmd/voting-api

test:
	@echo "==> Running tests..."
	go test ./... -v -race -timeout 5m

run: build
	@echo "==> Running voting service..."
	./bin/voting-api

debug: build
	@echo "==> Running voting service in debug mode..."
	LOG_LEVEL=debug ./bin/voting-api

clean:
	@echo "==> Cleaning generated files..."
	rm -rf gen/
	rm -rf bin/

# Run linter
lint:
	@echo "==> Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Run 'make deps' to install it."; \
		exit 1; \
	fi

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
