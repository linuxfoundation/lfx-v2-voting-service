// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package eventing

import (
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/propagation"
)

func TestNatsHeaderCarrier_Get(t *testing.T) {
	t.Run("returns empty string for missing key", func(t *testing.T) {
		carrier := natsHeaderCarrier(make(natsgo.Header))
		assert.Equal(t, "", carrier.Get("missing-key"))
	})

	t.Run("returns value for existing key", func(t *testing.T) {
		carrier := natsHeaderCarrier(natsgo.Header{
			"traceparent": []string{"00-trace-id-span-id-01"},
		})
		assert.Equal(t, "00-trace-id-span-id-01", carrier.Get("traceparent"))
	})

	t.Run("returns first value when multiple values set", func(t *testing.T) {
		carrier := natsHeaderCarrier(natsgo.Header{
			"key1": []string{"first", "second"},
		})
		assert.Equal(t, "first", carrier.Get("key1"))
	})

	t.Run("returns empty string for nil header", func(t *testing.T) {
		var carrier natsHeaderCarrier
		assert.Equal(t, "", carrier.Get("any-key"))
	})
}

func TestNatsHeaderCarrier_Set(t *testing.T) {
	t.Run("sets value on new key", func(t *testing.T) {
		carrier := natsHeaderCarrier(make(natsgo.Header))
		carrier.Set("traceparent", "00-abc-def-01")
		assert.Equal(t, "00-abc-def-01", carrier.Get("traceparent"))
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		carrier := natsHeaderCarrier(make(natsgo.Header))
		carrier.Set("key", "value1")
		carrier.Set("key", "value2")
		assert.Equal(t, "value2", carrier.Get("key"))
		assert.Equal(t, []string{"value2"}, natsgo.Header(carrier)["key"])
	})

	t.Run("stores full header state correctly", func(t *testing.T) {
		header := make(natsgo.Header)
		carrier := natsHeaderCarrier(header)
		carrier.Set("traceparent", "00-trace-span-01")
		carrier.Set("tracestate", "vendor=value")
		assert.Len(t, header, 2)
		assert.Equal(t, []string{"00-trace-span-01"}, header["traceparent"])
		assert.Equal(t, []string{"vendor=value"}, header["tracestate"])
	})

	t.Run("no-op on nil carrier", func(t *testing.T) {
		var carrier natsHeaderCarrier
		assert.NotPanics(t, func() { carrier.Set("key", "value") })
	})
}

func TestNatsHeaderCarrier_Keys(t *testing.T) {
	t.Run("returns empty slice for empty header", func(t *testing.T) {
		carrier := natsHeaderCarrier(make(natsgo.Header))
		assert.Empty(t, carrier.Keys())
	})

	t.Run("returns all keys for populated header", func(t *testing.T) {
		carrier := natsHeaderCarrier(make(natsgo.Header))
		carrier.Set("key1", "v1")
		carrier.Set("key2", "v2")
		keys := carrier.Keys()
		assert.Len(t, keys, 2)
		assert.Contains(t, keys, "key1")
		assert.Contains(t, keys, "key2")
	})

	t.Run("returns empty slice for nil header", func(t *testing.T) {
		var carrier natsHeaderCarrier
		assert.Empty(t, carrier.Keys())
	})
}

func TestNatsHeaderCarrier_TextMapCarrier(t *testing.T) {
	t.Run("satisfies TextMapCarrier interface", func(t *testing.T) {
		var _ propagation.TextMapCarrier = natsHeaderCarrier{}
	})

	t.Run("Set/Get round-trip preserves values", func(t *testing.T) {
		header := make(natsgo.Header)
		carrier := natsHeaderCarrier(header)

		carrier.Set("traceparent", "00-trace-id-span-id-01")
		carrier.Set("tracestate", "vendor=value")

		assert.Equal(t, "00-trace-id-span-id-01", carrier.Get("traceparent"))
		assert.Equal(t, "vendor=value", carrier.Get("tracestate"))
		assert.Len(t, header, 2)
	})
}
