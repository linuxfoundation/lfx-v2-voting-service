// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

module github.com/linuxfoundation/lfx-v2-voting-service

go 1.25.4

require (
	github.com/auth0/go-auth0 v1.48.0
	github.com/auth0/go-jwt-middleware/v2 v2.3.1
	github.com/go-chi/chi/v5 v5.3.2
	github.com/google/uuid v1.6.0
	github.com/linuxfoundation/lfx-v2-fga-sync v0.3.9
	github.com/linuxfoundation/lfx-v2-indexer-service v0.4.26
	github.com/linuxfoundation/lfx-v2-invite-service v0.1.15
	github.com/nats-io/nats-server/v2 v2.12.6
	github.com/nats-io/nats.go v1.53.1
	github.com/remychantenay/slog-otel v1.3.5
	github.com/stretchr/testify v1.12.1
	github.com/vmihailenco/msgpack/v5 v5.4.1
	go.opentelemetry.io/contrib/exporters/autoexport v0.71.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0
	go.opentelemetry.io/contrib/propagators/autoprop v0.71.0
	go.opentelemetry.io/otel v1.46.0
	go.opentelemetry.io/otel/log v0.22.0
	go.opentelemetry.io/otel/sdk v1.46.0
	go.opentelemetry.io/otel/sdk/log v0.22.0
	go.opentelemetry.io/otel/sdk/metric v1.46.0
	go.opentelemetry.io/otel/trace v1.46.0
	goa.design/goa/v3 v3.30.0
	golang.org/x/oauth2 v0.36.0
)

require (
	github.com/PuerkitoBio/rehttp v1.4.0 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.6.0-default-no-op // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/dimfeld/httppath v0.0.0-20170720192232-ee938bf73598 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gohugoio/hashstructure v1.0.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.6 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/jwx/v2 v2.1.7 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/manveru/faker v0.0.0-20171103152722-9fbc68a78c4d // indirect
	github.com/minio/highwayhash v1.0.4-0.20251030100505-070ab1a87a76 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.1 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_golang v1.24.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/otlptranslator v1.0.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.devnw.com/structs v1.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/bridges/prometheus v0.71.0 // indirect
	go.opentelemetry.io/contrib/propagators/aws v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/b3 v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/jaeger v1.46.0 // indirect
	go.opentelemetry.io/contrib/propagators/ot v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.68.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.22.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260825221802-da73d73af1c5 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260825221802-da73d73af1c5 // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/go-jose/go-jose.v2 v2.6.3 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
