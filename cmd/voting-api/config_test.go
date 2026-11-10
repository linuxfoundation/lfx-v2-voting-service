// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// configEnvKeys are every variable loadConfig and resolveSelfServeBaseURL read.
var configEnvKeys = []string{
	"PORT", "JWKS_URL", "AUDIENCE", "JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL",
	"ITX_BASE_URL", "ITX_AUTH0_DOMAIN", "ITX_CLIENT_ID", "ITX_CLIENT_PRIVATE_KEY", "ITX_AUDIENCE",
	"NATS_URL", "ID_MAPPING_DISABLED", "EVENT_PROCESSING_ENABLED", "EVENT_CONSUMER_NAME",
	"EVENT_STREAM_NAME", "VOTE_LINK_SIGNING_KEYS", "VOTE_LINK_ACTIVE_KID",
	"LFX_SELF_SERVE_BASE_URL", "LFX_ENVIRONMENT",
}

// clearConfigEnv blanks every config variable for the duration of the test (an empty value
// is treated as unset by getEnv, and t.Setenv restores the originals afterwards).
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range configEnvKeys {
		t.Setenv(k, "")
	}
}

func TestGetEnv(t *testing.T) {
	t.Run("returns the value when set", func(t *testing.T) {
		t.Setenv("VOTING_TEST_KEY", "configured")
		assert.Equal(t, "configured", getEnv("VOTING_TEST_KEY", "fallback"))
	})

	t.Run("returns the default when unset or empty", func(t *testing.T) {
		t.Setenv("VOTING_TEST_KEY", "")
		assert.Equal(t, "fallback", getEnv("VOTING_TEST_KEY", "fallback"))
	})

	t.Run("an empty default is allowed", func(t *testing.T) {
		t.Setenv("VOTING_TEST_KEY", "")
		assert.Equal(t, "", getEnv("VOTING_TEST_KEY", ""))
	})
}

func TestLoadConfig_Defaults(t *testing.T) {
	clearConfigEnv(t)

	cfg := loadConfig()

	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "http://heimdall:4457/.well-known/jwks", cfg.JWKSURL)
	assert.Equal(t, "lfx-v2-voting-service", cfg.Audience)
	assert.Empty(t, cfg.MockLocalPrincipal, "JWT validation must be on by default")
	assert.Equal(t, "https://api.dev.itx.linuxfoundation.org/", cfg.ITXBaseURL)
	assert.Equal(t, "linuxfoundation-dev.auth0.com", cfg.ITXAuth0Domain)
	assert.Empty(t, cfg.ITXClientID)
	assert.Empty(t, cfg.ITXPrivateKey)
	assert.Equal(t, "https://api.dev.itx.linuxfoundation.org/", cfg.ITXAudience)
	assert.Equal(t, 30*time.Second, cfg.ITXTimeout)
	assert.Equal(t, "nats://nats:4222", cfg.NATSURL)
	assert.Equal(t, 5*time.Second, cfg.NATSTimeout)
	assert.False(t, cfg.IDMappingDisabled, "ID mapping is on unless explicitly disabled")
	assert.True(t, cfg.EventProcessingEnabled, "event processing is on unless explicitly disabled")
	assert.Equal(t, "voting-service-kv-consumer", cfg.EventConsumerName)
	assert.Equal(t, "KV_v1-objects", cfg.EventStreamName)
	assert.Empty(t, cfg.VoteLinkSigningKeys, "vote links are disabled unless keys are configured")
	assert.Empty(t, cfg.VoteLinkActiveKID)
}

func TestLoadConfig_Overrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("JWKS_URL", "https://jwks.example.com/keys")
	t.Setenv("AUDIENCE", "custom-audience")
	t.Setenv("JWT_AUTH_DISABLED_MOCK_LOCAL_PRINCIPAL", "local-user")
	t.Setenv("ITX_BASE_URL", "https://itx.example.com/")
	t.Setenv("ITX_AUTH0_DOMAIN", "auth.example.com")
	t.Setenv("ITX_CLIENT_ID", "client-1")
	t.Setenv("ITX_CLIENT_PRIVATE_KEY", "pem-body")
	t.Setenv("ITX_AUDIENCE", "https://itx-audience.example.com/")
	t.Setenv("NATS_URL", "nats://nats.example.com:4222")
	t.Setenv("EVENT_CONSUMER_NAME", "consumer-1")
	t.Setenv("EVENT_STREAM_NAME", "STREAM_1")
	t.Setenv("VOTE_LINK_SIGNING_KEYS", "k1:abc")
	t.Setenv("VOTE_LINK_ACTIVE_KID", "k1")

	cfg := loadConfig()

	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "https://jwks.example.com/keys", cfg.JWKSURL)
	assert.Equal(t, "custom-audience", cfg.Audience)
	assert.Equal(t, "local-user", cfg.MockLocalPrincipal)
	assert.Equal(t, "https://itx.example.com/", cfg.ITXBaseURL)
	assert.Equal(t, "auth.example.com", cfg.ITXAuth0Domain)
	assert.Equal(t, "client-1", cfg.ITXClientID)
	assert.Equal(t, "pem-body", cfg.ITXPrivateKey)
	assert.Equal(t, "https://itx-audience.example.com/", cfg.ITXAudience)
	assert.Equal(t, "nats://nats.example.com:4222", cfg.NATSURL)
	assert.Equal(t, "consumer-1", cfg.EventConsumerName)
	assert.Equal(t, "STREAM_1", cfg.EventStreamName)
	assert.Equal(t, "k1:abc", cfg.VoteLinkSigningKeys)
	assert.Equal(t, "k1", cfg.VoteLinkActiveKID)
}

func TestLoadConfig_BooleanFlags(t *testing.T) {
	tests := []struct {
		name, key, value string
		get              func(config) bool
		want             bool
	}{
		{"ID_MAPPING_DISABLED=true disables mapping", "ID_MAPPING_DISABLED", "true", func(c config) bool { return c.IDMappingDisabled }, true},
		{"ID_MAPPING_DISABLED=false keeps mapping", "ID_MAPPING_DISABLED", "false", func(c config) bool { return c.IDMappingDisabled }, false},
		{"ID_MAPPING_DISABLED only accepts the exact string true", "ID_MAPPING_DISABLED", "TRUE", func(c config) bool { return c.IDMappingDisabled }, false},
		{"EVENT_PROCESSING_ENABLED=false turns processing off", "EVENT_PROCESSING_ENABLED", "false", func(c config) bool { return c.EventProcessingEnabled }, false},
		{"EVENT_PROCESSING_ENABLED=true keeps processing on", "EVENT_PROCESSING_ENABLED", "true", func(c config) bool { return c.EventProcessingEnabled }, true},
		{"EVENT_PROCESSING_ENABLED treats any other value as off", "EVENT_PROCESSING_ENABLED", "1", func(c config) bool { return c.EventProcessingEnabled }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tt.key, tt.value)

			assert.Equal(t, tt.want, tt.get(loadConfig()))
		})
	}
}

func TestResolveSelfServeBaseURL(t *testing.T) {
	t.Run("an explicit base URL wins over the environment", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("LFX_SELF_SERVE_BASE_URL", "https://self-serve.example.com")
		t.Setenv("LFX_ENVIRONMENT", "staging")

		assert.Equal(t, "https://self-serve.example.com", resolveSelfServeBaseURL())
	})

	environments := map[string]string{
		"":        "https://app.lfx.dev", // unset defaults to prod
		"prod":    "https://app.lfx.dev",
		"staging": "https://app.staging.lfx.dev",
		"dev":     "https://app.dev.lfx.dev",
		"other":   "https://app.dev.lfx.dev", // anything unrecognised falls back to dev
	}
	for env, want := range environments {
		t.Run("LFX_ENVIRONMENT="+env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("LFX_ENVIRONMENT", env)

			assert.Equal(t, want, resolveSelfServeBaseURL())
		})
	}
}
