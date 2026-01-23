.PHONY: deps apigen build test clean run debug

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
