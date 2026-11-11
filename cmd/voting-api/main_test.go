// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestParseBoolEnv(t *testing.T) {
	const key = "VOTE_PARTICIPANT_GRANTS_ENABLED_TEST"

	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset is off", set: false, want: false},
		{name: "empty is off", set: true, value: "", want: false},
		{name: "true", set: true, value: "true", want: true},
		{name: "1", set: true, value: "1", want: true},
		{name: "yes", set: true, value: "YES", want: true},
		{name: "false", set: true, value: "false", want: false},
		// A typo must never switch a gated behaviour on.
		{name: "unrecognised is off", set: true, value: "enabled-please", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.value)
			}
			assert.Equal(t, tt.want, parseBoolEnv(key, slog.Default()))
		})
	}
}

// parseInviteConfig shares parseBoolEnv, so it accepts the same values and a typo is off.
func TestParseInviteConfig_UsesTheSharedBoolParsing(t *testing.T) {
	tests := map[string]bool{"true": true, "1": true, "yes": true, "false": false, "": false, "enabled-please": false}
	for value, want := range tests {
		t.Run("INVITES_ENABLED="+value, func(t *testing.T) {
			t.Setenv("INVITES_ENABLED", value)
			t.Setenv("LFX_SELF_SERVE_BASE_URL", "https://app.example.test")
			assert.Equal(t, want, parseInviteConfig(slog.Default()).Enabled)
		})
	}
}

func TestLimitVoteLinkBody(t *testing.T) {
	var readErr error
	handler := limitVoteLinkBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	oversized := strings.Repeat("a", maxVoteLinkBodyBytes+1)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{path: "/vote_links/responses", wantErr: true},
		{path: "/vote_responses", wantErr: false},
		{path: "/votes/x/bulk_resend", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			readErr = nil
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(oversized)))
			if (readErr != nil) != tt.wantErr {
				t.Errorf("read error = %v, wantErr %v", readErr, tt.wantErr)
			}
		})
	}
}

func TestNewVoteLinkSigner(t *testing.T) {
	key := func(fill string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(fill, 32))) }

	tests := []struct {
		name       string
		cfg        config
		wantNil    bool
		wantErr    bool
		wantActive string
	}{
		{name: "unset disables the feature", cfg: config{}, wantNil: true},
		{name: "single key is active", cfg: config{VoteLinkSigningKeys: "k1:" + key("a")}, wantActive: "k1"},
		{name: "first listed key is active by default", cfg: config{VoteLinkSigningKeys: "k2:" + key("b") + ",k1:" + key("a")}, wantActive: "k2"},
		{name: "explicit active kid wins", cfg: config{VoteLinkSigningKeys: "k2:" + key("b") + ",k1:" + key("a"), VoteLinkActiveKID: "k1"}, wantActive: "k1"},
		{name: "unknown active kid fails", cfg: config{VoteLinkSigningKeys: "k1:" + key("a"), VoteLinkActiveKID: "k9"}, wantErr: true},
		{name: "malformed keys fail", cfg: config{VoteLinkSigningKeys: "nocolon"}, wantErr: true},
		{name: "short key fails", cfg: config{VoteLinkSigningKeys: "k1:" + base64.StdEncoding.EncodeToString([]byte("short"))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := newVoteLinkSigner(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if (signer == nil) != tt.wantNil {
				t.Fatalf("signer nil = %v, want %v", signer == nil, tt.wantNil)
			}
			if signer != nil && signer.ActiveKeyID() != tt.wantActive {
				t.Errorf("active key = %q, want %q", signer.ActiveKeyID(), tt.wantActive)
			}
		})
	}
}
