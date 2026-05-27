// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	// PS256 is the default for Heimdall's JWT finalizer.
	signatureAlgorithm = validator.PS256
	defaultIssuer      = "heimdall"
	defaultAudience    = "lfx-v2-voting-service"
	defaultJWKSURL     = "http://heimdall:4457/.well-known/jwks"
	jwksClientTimeout  = 10 * time.Second
)

// Config holds the configuration parameters for JWT authentication.
type Config struct {
	// JWKSURL is the URL to the JSON Web Key Set endpoint
	JWKSURL string
	// Audience is the intended audience for the JWT token
	Audience string
	// MockLocalPrincipal is used for local development to bypass JWT validation
	MockLocalPrincipal string
}

var (
	// Factory for custom JWT claims target.
	customClaims = func() validator.CustomClaims {
		return &HeimdallClaims{}
	}
)

// HeimdallClaims contains extra custom claims we want to parse from the JWT token.
type HeimdallClaims struct {
	Principal string `json:"principal"`
	Email     string `json:"email,omitempty"`
}

// Validate provides additional middleware validation of any claims defined in HeimdallClaims.
func (c *HeimdallClaims) Validate(_ context.Context) error {
	if c.Principal == "" {
		return errors.New("principal must be provided")
	}
	return nil
}

type JWTAuth struct {
	validator *validator.Validator
	config    Config
}

// Ensure JWTAuth implements domain.Authenticator interface
var _ domain.Authenticator = (*JWTAuth)(nil)

func NewJWTAuth(config Config) (*JWTAuth, error) {
	// Set up defaults if not provided
	jwksURLStr := config.JWKSURL
	if jwksURLStr == "" {
		jwksURLStr = defaultJWKSURL
	}
	audience := config.Audience
	if audience == "" {
		audience = defaultAudience
	}

	// Set up Heimdall JWKS key provider.
	jwksURL, err := url.Parse(jwksURLStr)
	if err != nil {
		slog.Error("invalid JWKS_URL", "error", err)
		return nil, err
	}
	var issuer *url.URL
	issuer, err = url.Parse(defaultIssuer)
	if err != nil {
		// This shouldn't happen; a bare hostname is a valid URL.
		slog.Error("unexpected URL parsing of default issuer", "error", err)
		return nil, err
	}
	otelClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   jwksClientTimeout,
	}
	provider := jwks.NewCachingProvider(issuer, 5*time.Minute, jwks.WithCustomJWKSURI(jwksURL), jwks.WithCustomClient(otelClient))

	// Set up the JWT validator.
	jwtValidator, err := validator.New(
		provider.KeyFunc,
		signatureAlgorithm,
		issuer.String(),
		[]string{audience},
		validator.WithCustomClaims(customClaims),
		validator.WithAllowedClockSkew(5*time.Second),
	)
	if err != nil {
		slog.Error("failed to set up the Heimdall JWT validator", "error", err)
		return nil, err
	}

	return &JWTAuth{
		validator: jwtValidator,
		config:    config,
	}, nil
}

// ParsePrincipal extracts the principal from the JWT claims.
func (j *JWTAuth) ParsePrincipal(ctx context.Context, token string, logger *slog.Logger) (string, error) {
	// To avoid having to use a valid JWT token for local development, we can use the
	// MockLocalPrincipal configuration parameter.
	if j.config.MockLocalPrincipal != "" {
		logger.InfoContext(ctx, "JWT authentication is disabled, returning mock principal",
			"principal", j.config.MockLocalPrincipal,
		)
		return j.config.MockLocalPrincipal, nil
	}

	if j.validator == nil {
		return "", errors.New("JWT validator is not set up")
	}

	parsedJWT, err := j.validator.ValidateToken(ctx, token)
	if err != nil {
		// Drop tertiary (and deeper) nested errors for security reasons. This is
		// using colons as an approximation for error nesting, which may not
		// exactly match to error boundaries. Unwrapping the error twice, then
		// dropping the suffix of the 3rd error's String() method could be more
		// accurate to error boundaries, but could also expose tertiary errors if
		// errors are not wrapped with Go 1.13 `%w` semantics.
		logger.WarnContext(ctx, "authorization failed",
			"default_audience", defaultAudience,
			"default_issuer", defaultIssuer,
			"error", err,
		)
		errString := err.Error()
		firstColon := strings.Index(errString, ":")
		if firstColon != -1 && firstColon+1 < len(errString) {
			errString = strings.Replace(errString, ": go-jose/go-jose/jwt", "", 1)
			secondColon := strings.Index(errString[firstColon+1:], ":")
			if secondColon != -1 {
				// Error has two colons (which may be 3 or more errors), so drop the
				// second colon and everything after it.
				errString = errString[:firstColon+secondColon+1]
			}
		}
		return "", errors.New(errString)
	}

	claims, ok := parsedJWT.(*validator.ValidatedClaims)
	if !ok {
		// This should never happen.
		return "", errors.New("failed to get validated authorization claims")
	}

	customClaims, ok := claims.CustomClaims.(*HeimdallClaims)
	if !ok {
		// This should never happen.
		return "", errors.New("failed to get custom authorization claims")
	}

	logger.DebugContext(ctx, "JWT principal parsed",
		"principal", customClaims.Principal,
		"email", customClaims.Email,
	)

	return customClaims.Principal, nil
}
