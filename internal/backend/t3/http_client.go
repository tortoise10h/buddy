package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOrchestrationProtocolVersion is the fallback protocol version if discovery has not run.
	DefaultOrchestrationProtocolVersion = 2

	// OrchestrationProtocolHeader is the header name required by T3 Code orchestration endpoints.
	OrchestrationProtocolHeader = "x-t3-orchestration-protocol"
)

// HTTPClient defines the interface for executing HTTP requests against T3 Code.
type HTTPClient struct {
	baseURL         string
	httpClient      *http.Client
	tokenProvider   *SessionTokenProvider
	protocolVersion int
	mu              sync.RWMutex
}

// HTTPClientOption configures HTTPClient.
type HTTPClientOption func(*HTTPClient)

// WithBaseURL sets the base URL (e.g. "http://127.0.0.1:3773").
func WithBaseURL(url string) HTTPClientOption {
	return func(c *HTTPClient) {
		c.baseURL = strings.TrimRight(url, "/")
	}
}

// WithCustomHTTPClient overrides the underlying net/http.Client (for httptest).
func WithCustomHTTPClient(hc *http.Client) HTTPClientOption {
	return func(c *HTTPClient) {
		c.httpClient = hc
	}
}

// WithHTTPTokenProvider sets the session token provider.
func WithHTTPTokenProvider(tp *SessionTokenProvider) HTTPClientOption {
	return func(c *HTTPClient) {
		c.tokenProvider = tp
	}
}

// WithProtocolVersion overrides the orchestration protocol version header.
func WithProtocolVersion(v int) HTTPClientOption {
	return func(c *HTTPClient) {
		c.protocolVersion = v
	}
}

// NewHTTPClient creates an authenticated HTTP client for T3 Code.
func NewHTTPClient(opts ...HTTPClientOption) *HTTPClient {
	c := &HTTPClient{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		protocolVersion: DefaultOrchestrationProtocolVersion,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetBaseURL updates the client's base URL dynamically.
func (c *HTTPClient) SetBaseURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(url, "/")
}

// BaseURL returns the client's base URL.
func (c *HTTPClient) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// DiscoverProtocolVersion queries GET /.well-known/t3/environment (unauthenticated)
// to determine the server's orchestration protocol version and server version.
func (c *HTTPClient) DiscoverProtocolVersion(ctx context.Context) (*EnvironmentDescriptor, error) {
	reqURL := fmt.Sprintf("%s/.well-known/t3/environment", c.BaseURL())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create environment request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("t3 server unreachable at %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to discover t3 environment (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var desc EnvironmentDescriptor
	if err := json.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return nil, fmt.Errorf("failed to parse environment descriptor: %w", err)
	}

	if desc.OrchestrationProtocolVersion > 0 {
		c.mu.Lock()
		c.protocolVersion = desc.OrchestrationProtocolVersion
		c.mu.Unlock()
	}

	return &desc, nil
}

// ProtocolVersion returns the currently configured or discovered protocol version.
func (c *HTTPClient) ProtocolVersion() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.protocolVersion
}

// GetProjects retrieves the list of projects from GET /api/projects.
func (c *HTTPClient) GetProjects(ctx context.Context) (*ProjectSnapshot, error) {
	var snapshot ProjectSnapshot
	err := c.doGet(ctx, "/api/projects", false, &snapshot)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// GetShellSnapshot retrieves all active projects and threads from GET /api/orchestration/shell.
func (c *HTTPClient) GetShellSnapshot(ctx context.Context) (*OrchestrationShellSnapshot, error) {
	var snapshot OrchestrationShellSnapshot
	err := c.doGet(ctx, "/api/orchestration/shell", true, &snapshot)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// GetThreadSnapshot retrieves detailed state of a specific thread from GET /api/orchestration/threads/:id.
func (c *HTTPClient) GetThreadSnapshot(ctx context.Context, threadID string) (*ThreadDetailSnapshot, error) {
	if threadID == "" {
		return nil, fmt.Errorf("threadID cannot be empty")
	}
	endpoint := fmt.Sprintf("/api/orchestration/threads/%s", threadID)
	var snapshot ThreadDetailSnapshot
	err := c.doGet(ctx, endpoint, true, &snapshot)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// GetThreadTranscript retrieves the turn items and conversation transcript from
// GET /api/orchestration/threads/:id/transcript.
func (c *HTTPClient) GetThreadTranscript(ctx context.Context, threadID string) (*ThreadTranscript, error) {
	if threadID == "" {
		return nil, fmt.Errorf("threadID cannot be empty")
	}
	endpoint := fmt.Sprintf("/api/orchestration/threads/%s/transcript", threadID)
	var transcript ThreadTranscript
	err := c.doGet(ctx, endpoint, true, &transcript)
	if err != nil {
		return nil, err
	}
	return &transcript, nil
}

func (c *HTTPClient) doGet(ctx context.Context, endpoint string, needsProtocolHeader bool, target any) error {
	base := c.BaseURL()
	if base == "" {
		return fmt.Errorf("t3 base URL is not configured")
	}

	reqURL := fmt.Sprintf("%s%s", base, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", endpoint, err)
	}

	// Auth token injection
	if c.tokenProvider != nil {
		token, err := c.tokenProvider.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get session token for %s: %w", endpoint, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Protocol version header injection
	if needsProtocolHeader {
		protoVer := c.ProtocolVersion()
		req.Header.Set(OrchestrationProtocolHeader, strconv.Itoa(protoVer))
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		if c.tokenProvider != nil {
			c.tokenProvider.Invalidate()
		}
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: endpoint %s returned 401: %s", ErrAuthInvalid, endpoint, strings.TrimSpace(string(body)))
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: endpoint %s returned 404", ErrThreadNotFound, endpoint)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("t3 request to %s failed (status %d): %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("failed to decode response from %s: %w", endpoint, err)
		}
	}

	return nil
}
