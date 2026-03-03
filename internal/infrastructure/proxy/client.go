// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/auth0/go-auth0/authentication"
	"github.com/auth0/go-auth0/authentication/oauth"
	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"golang.org/x/oauth2"
)

const tokenExpiryLeeway = 60 * time.Second

// Config holds ITX proxy configuration
type Config struct {
	BaseURL     string
	Auth0Domain string
	ClientID    string
	PrivateKey  string // RSA private key in PEM format
	Audience    string
	Timeout     time.Duration
}

// Client implements domain.ITXProxyClient
type Client struct {
	httpClient *http.Client
	config     Config
}

// auth0TokenSource implements oauth2.TokenSource using Auth0 SDK with private key
type auth0TokenSource struct {
	ctx        context.Context
	authConfig *authentication.Authentication
	audience   string
}

// Token implements the oauth2.TokenSource interface
func (a *auth0TokenSource) Token() (*oauth2.Token, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}

	// Build and issue a request using Auth0 SDK
	body := oauth.LoginWithClientCredentialsRequest{
		Audience: a.audience,
	}

	tokenSet, err := a.authConfig.OAuth.LoginWithClientCredentials(ctx, body, oauth.IDTokenValidationOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get token from Auth0: %w", err)
	}

	// Convert Auth0 response to oauth2.Token with leeway for expiration
	token := &oauth2.Token{
		AccessToken:  tokenSet.AccessToken,
		TokenType:    tokenSet.TokenType,
		RefreshToken: tokenSet.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenSet.ExpiresIn)*time.Second - tokenExpiryLeeway),
	}

	// Add extra fields
	token = token.WithExtra(map[string]any{
		"scope": tokenSet.Scope,
	})

	return token, nil
}

// NewClient creates a new ITX proxy client with OAuth2 M2M authentication using private key
func NewClient(config Config) *Client {
	ctx := context.Background()

	if config.PrivateKey == "" {
		panic("ITX_CLIENT_PRIVATE_KEY is required but not set")
	}

	// Create Auth0 authentication client with private key assertion (JWT)
	// The private key should be in PEM format (raw, not base64-encoded)
	authConfig, err := authentication.New(
		ctx,
		config.Auth0Domain,
		authentication.WithClientID(config.ClientID),
		authentication.WithClientAssertion(config.PrivateKey, "RS256"),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create Auth0 client: %v (ensure ITX_CLIENT_PRIVATE_KEY contains a valid RSA private key in PEM format)", err))
	}

	// Create token source
	tokenSource := &auth0TokenSource{
		ctx:        ctx,
		authConfig: authConfig,
		audience:   config.Audience,
	}

	// Wrap with oauth2.ReuseTokenSource for automatic caching and renewal
	reuseTokenSource := oauth2.ReuseTokenSource(nil, tokenSource)

	// Create HTTP client that automatically handles token management
	httpClient := oauth2.NewClient(ctx, reuseTokenSource)
	httpClient.Timeout = config.Timeout

	return &Client{
		httpClient: httpClient,
		config:     config,
	}
}

// CreatePoll creates a new poll in ITX
func (c *Client) CreatePoll(ctx context.Context, req *itx.CreatePollRequest) (*itx.PollResponse, error) {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll", c.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request (OAuth2 transport will add Authorization header automatically)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response directly into domain model
	var result itx.PollResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// GetPoll retrieves poll details from ITX
func (c *Client) GetPoll(ctx context.Context, pollID string) (*itx.PollResponse, error) {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response
	var result itx.PollResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// UpdatePoll updates a poll in ITX (only when status is "disabled")
func (c *Client) UpdatePoll(ctx context.Context, pollID string, req *itx.UpdatePollRequest) (*itx.PollResponse, error) {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes (ITX returns 400 if status is not "disabled")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response
	var result itx.PollResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// DeletePoll deletes a poll in ITX (only when status is "disabled")
func (c *Client) DeletePoll(ctx context.Context, pollID string) error {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes (ITX returns 400 if status is not "disabled")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

// ExtendPoll extends a poll's end time in ITX
func (c *Client) ExtendPoll(ctx context.Context, pollID string, req *itx.ExtendPollRequest) (*itx.PollResponse, error) {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s/extend", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response
	var result itx.PollResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// EnablePoll enables a poll for voting in ITX
func (c *Client) EnablePoll(ctx context.Context, pollID string) error {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s/enable", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

// BulkResendPoll bulk resends poll emails to select recipients in ITX
func (c *Client) BulkResendPoll(ctx context.Context, pollID string, req *itx.BulkResendRequest) error {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s/bulk_resend", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

// GetPollResults retrieves aggregated poll results from ITX
func (c *Client) GetPollResults(ctx context.Context, pollID string) (*itx.VoteResults, error) {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/poll/%s/results", c.config.BaseURL, pollID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response
	var result itx.VoteResults
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// mapHTTPError maps ITX HTTP status codes to domain errors
func (c *Client) mapHTTPError(statusCode int, body []byte) error {
	var errMsg struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &errMsg)

	message := errMsg.Message
	if message == "" {
		message = errMsg.Error
	}
	if message == "" {
		message = fmt.Sprintf("ITX API error: HTTP %d", statusCode)
	}

	switch statusCode {
	case http.StatusBadRequest:
		return domain.NewValidationError(message)
	case http.StatusUnauthorized, http.StatusForbidden:
		// There shouldn't be unauthorized or forbidden errors from ITX since we are using M2M authentication,
		// so these errors imply an internal server error due to issues with the M2M credentials.
		return domain.NewInternalError(message)
	case http.StatusNotFound:
		return domain.NewNotFoundError(message)
	case http.StatusConflict:
		return domain.NewConflictError(message)
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return domain.NewUnavailableError(message)
	default:
		return domain.NewInternalError(message)
	}
}

// CreateVote submits a vote response in ITX
func (c *Client) CreateVote(ctx context.Context, req *itx.CreateVoteRequest) error {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/vote", c.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes (ITX returns 201 on success)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

// GetVote retrieves vote response details from ITX
func (c *Client) GetVote(ctx context.Context, voteID string) (*itx.VoteResponse, error) {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/vote/%s", c.config.BaseURL, voteID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.mapHTTPError(resp.StatusCode, respBody)
	}

	// Parse response
	var result itx.VoteResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, domain.NewInternalError("failed to parse response", err)
	}

	return &result, nil
}

// UpdateVote updates a vote response in ITX
func (c *Client) UpdateVote(ctx context.Context, voteID string, req *itx.UpdateVoteRequest) error {
	// Marshal request
	body, err := json.Marshal(req)
	if err != nil {
		return domain.NewInternalError("failed to marshal request", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/vote/%s", c.config.BaseURL, voteID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes (ITX returns 204 on success)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}

// ResendVote resends the vote email in ITX
func (c *Client) ResendVote(ctx context.Context, voteID string) error {
	// Create HTTP request
	url := fmt.Sprintf("%sv2/voting/vote/%s/resend", c.config.BaseURL, voteID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return domain.NewInternalError("failed to create request", err)
	}

	// Set headers (Authorization header is automatically set by OAuth2 transport)
	httpReq.Header.Set("x-scope", "manage:voting")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.NewUnavailableError("ITX service request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.NewInternalError("failed to read response", err)
	}

	// Handle non-2xx status codes (ITX returns 204 on success)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.mapHTTPError(resp.StatusCode, respBody)
	}

	return nil
}
