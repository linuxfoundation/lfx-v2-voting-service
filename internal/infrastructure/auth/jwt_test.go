// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	jose "gopkg.in/go-jose/go-jose.v2"
	"gopkg.in/go-jose/go-jose.v2/jwt"
)

const testKeyID = "test-key-1"

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// jwksFixture serves a JWKS for a freshly generated RSA key and signs tokens with it.
type jwksFixture struct {
	server *httptest.Server
	signer jose.Signer
}

func newJWKSFixture(t *testing.T) *jwksFixture {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwk := jose.JSONWebKey{Key: &key.PublicKey, KeyID: testKeyID, Algorithm: string(jose.PS256), Use: "sig"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	}))
	t.Cleanup(server.Close)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.PS256, Key: jose.JSONWebKey{Key: key, KeyID: testKeyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)

	return &jwksFixture{server: server, signer: signer}
}

func (f *jwksFixture) sign(t *testing.T, std jwt.Claims, custom HeimdallClaims) string {
	t.Helper()
	token, err := jwt.Signed(f.signer).Claims(std).Claims(custom).CompactSerialize()
	require.NoError(t, err)
	return token
}

func validClaims() jwt.Claims {
	return jwt.Claims{
		Issuer:   defaultIssuer,
		Audience: jwt.Audience{defaultAudience},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
}

func TestHeimdallClaims_Validate(t *testing.T) {
	t.Run("accepts a claim with a principal", func(t *testing.T) {
		c := &HeimdallClaims{Principal: "alice"}
		assert.NoError(t, c.Validate(context.Background()))
	})

	t.Run("rejects an empty principal", func(t *testing.T) {
		c := &HeimdallClaims{Email: "alice@example.com"}
		err := c.Validate(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "principal")
	})
}

func TestNewJWTAuth(t *testing.T) {
	t.Run("defaults are applied when config is empty", func(t *testing.T) {
		j, err := NewJWTAuth(Config{})
		require.NoError(t, err)
		require.NotNil(t, j)
		assert.NotNil(t, j.validator)
	})

	t.Run("keeps the supplied config", func(t *testing.T) {
		cfg := Config{JWKSURL: "http://jwks.example.com/keys", Audience: "aud", MockLocalPrincipal: "alice"}
		j, err := NewJWTAuth(cfg)
		require.NoError(t, err)
		assert.Equal(t, cfg, j.config)
	})

	t.Run("invalid JWKS URL is rejected", func(t *testing.T) {
		j, err := NewJWTAuth(Config{JWKSURL: "://not a url"})
		require.Error(t, err)
		assert.Nil(t, j)
	})
}

func TestParsePrincipal_MockAndGuards(t *testing.T) {
	t.Run("mock principal bypasses validation", func(t *testing.T) {
		j, err := NewJWTAuth(Config{MockLocalPrincipal: "local-user"})
		require.NoError(t, err)

		principal, err := j.ParsePrincipal(context.Background(), "not-even-a-jwt", discardLogger)

		require.NoError(t, err)
		assert.Equal(t, "local-user", principal)
	})

	t.Run("missing validator returns an error", func(t *testing.T) {
		j := &JWTAuth{}

		principal, err := j.ParsePrincipal(context.Background(), "token", discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
		assert.Contains(t, err.Error(), "not set up")
	})
}

func TestParsePrincipal_ValidateToken(t *testing.T) {
	fx := newJWKSFixture(t)
	j, err := NewJWTAuth(Config{JWKSURL: fx.server.URL})
	require.NoError(t, err)

	t.Run("valid token returns the principal", func(t *testing.T) {
		token := fx.sign(t, validClaims(), HeimdallClaims{Principal: "alice", Email: "alice@example.com"})

		principal, err := j.ParsePrincipal(context.Background(), token, discardLogger)

		require.NoError(t, err)
		assert.Equal(t, "alice", principal)
	})

	t.Run("token without a principal is rejected", func(t *testing.T) {
		token := fx.sign(t, validClaims(), HeimdallClaims{Email: "alice@example.com"})

		principal, err := j.ParsePrincipal(context.Background(), token, discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Expiry = jwt.NewNumericDate(time.Now().Add(-time.Hour))
		token := fx.sign(t, claims, HeimdallClaims{Principal: "alice"})

		principal, err := j.ParsePrincipal(context.Background(), token, discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Audience = jwt.Audience{"some-other-service"}
		token := fx.sign(t, claims, HeimdallClaims{Principal: "alice"})

		principal, err := j.ParsePrincipal(context.Background(), token, discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
	})

	t.Run("wrong issuer is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Issuer = "someone-else"
		token := fx.sign(t, claims, HeimdallClaims{Principal: "alice"})

		principal, err := j.ParsePrincipal(context.Background(), token, discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
	})

	t.Run("custom audience from config is honoured", func(t *testing.T) {
		custom, err := NewJWTAuth(Config{JWKSURL: fx.server.URL, Audience: "custom-aud"})
		require.NoError(t, err)
		claims := validClaims()
		claims.Audience = jwt.Audience{"custom-aud"}
		token := fx.sign(t, claims, HeimdallClaims{Principal: "bob"})

		principal, err := custom.ParsePrincipal(context.Background(), token, discardLogger)

		require.NoError(t, err)
		assert.Equal(t, "bob", principal)
	})

	t.Run("malformed token is rejected with a trimmed error", func(t *testing.T) {
		principal, err := j.ParsePrincipal(context.Background(), "garbage", discardLogger)

		require.Error(t, err)
		assert.Empty(t, principal)
		assert.NotContains(t, err.Error(), "go-jose/go-jose/jwt")
	})
}
