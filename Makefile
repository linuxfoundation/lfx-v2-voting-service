.PHONY: deps apigen build test clean run debug docker-build helm-install helm-install-local helm-templates helm-templates-local helm-uninstall

# Docker configuration
DOCKER_IMAGE=linuxfoundation/lfx-v2-voting-service
DOCKER_TAG=latest

# Helm configuration
HELM_CHART_PATH=./charts/lfx-v2-voting-service
HELM_RELEASE_NAME=lfx-v2-voting-service
HELM_NAMESPACE=lfx
HELM_VALUES_FILE=./charts/lfx-v2-voting-service/values.local.yaml

deps:
	@echo "Installing dependencies..."
	go install goa.design/goa/v3/cmd/goa@latest
	go mod download

apigen:
	@echo "Generating API code from Goa design..."
	goa gen github.com/linuxfoundation/lfx-v2-voting-service/api/voting/v1/design

build:
	@echo "Building voting service..."
	go build -o bin/voting-api ./cmd/voting-api

test:
	@echo "Running tests..."
	go test ./... -v -race -timeout 5m

run: build
	@echo "Running voting service..."
	./bin/voting-api

debug: build
	@echo "Running voting service in debug mode..."
	LOG_LEVEL=debug ./bin/voting-api

clean:
	@echo "Cleaning generated files..."
	rm -rf gen/
	rm -rf bin/

# Docker targets
docker-build:
	@echo "==> Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) -f ./Dockerfile .
	@echo "==> Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

# Helm targets
helm-install:
	@echo "Installing Helm chart with default values..."
	helm upgrade --force --install $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) --create-namespace

helm-install-local:
	@echo "Installing Helm chart with local values..."
	helm upgrade --force --install $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--values $(HELM_VALUES_FILE)

helm-templates:
	@echo "Rendering Helm templates with default values..."
	helm template $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE)

helm-templates-local:
	@echo "Rendering Helm templates with local values..."
	helm template $(HELM_RELEASE_NAME) $(HELM_CHART_PATH) \
		--namespace $(HELM_NAMESPACE) \
		--values $(HELM_VALUES_FILE)

helm-uninstall:
	@echo "Uninstalling Helm chart..."
	helm uninstall $(HELM_RELEASE_NAME) --namespace $(HELM_NAMESPACE)
