# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT

# Build stage
FROM cgr.dev/chainguard/go:latest AS builder

ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application with version info
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

RUN go build -o /go/bin/voting-api -trimpath \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    github.com/linuxfoundation/lfx-v2-voting-service/cmd/voting-api

# Final stage
FROM cgr.dev/chainguard/static:latest

# Use non-root user
USER nonroot

# Copy binary from builder
COPY --from=builder /go/bin/voting-api /cmd/voting-api

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/cmd/voting-api", "-health-check"] || exit 1

ENTRYPOINT ["/cmd/voting-api"]
