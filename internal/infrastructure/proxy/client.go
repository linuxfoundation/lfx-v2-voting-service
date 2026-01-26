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
	"net/url"
	"time"

	"github.com/linuxfoundation/lfx-v2-voting-service/internal/domain"
	"github.com/linuxfoundation/lfx-v2-voting-service/pkg/models/itx"
	"golang.org/x/oauth2/clientcredentials"
)

// Config holds ITX proxy configuration
type Config struct {
	BaseURL      string
	Auth0Domain  string
	ClientID     string
	ClientSecret string
	Audience     string
	Timeout      time.Duration
}

// Client implements domain.ITXProxyClient
type Client struct {
	httpClient *http.Client
	config     Config
}

// NewClient creates a new ITX proxy client with OAuth2 M2M authentication
func NewClient(config Config) *Client {
	// Configure OAuth2 client credentials flow
	oauthConfig := &clientcredentials.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		TokenURL:     fmt.Sprintf("https://%s/oauth/token", config.Auth0Domain),
		Scopes:       []string{"read:projects", "manage:projects"},
		EndpointParams: url.Values{
			"audience": []string{config.Audience},
		},
	}

	// Create HTTP client with OAuth2 token source
	// The oauth2 package will automatically handle token caching and renewal
	ctx := context.Background()
	httpClient := oauthConfig.Client(ctx)
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
		return domain.NewValidationError(message)
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
