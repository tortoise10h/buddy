package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client defines the pure OpenCode v2 HTTP/SSE API operations used by OpenCodeEngine.
type Client interface {
	Info(ctx context.Context) (*ServerInfo, error)
	ListProjects(ctx context.Context) ([]Project, error)
	ListSessions(ctx context.Context, opts ListSessionsOptions) ([]SessionInfo, error)
	CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error)
	GetActiveSessions(ctx context.Context) (map[string]SessionActive, error)
	GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)
	Prompt(ctx context.Context, sessionID string, req PromptRequest) error
	Interrupt(ctx context.Context, sessionID string) error
	ListMessages(ctx context.Context, sessionID string, limit int, order string) ([]MessageInfo, error)
	ListPermissions(ctx context.Context, sessionID string) ([]PermissionRequest, error)
	ListForms(ctx context.Context, sessionID string) ([]FormRequest, error)
	SubscribeEvents(ctx context.Context, types []string) (<-chan SSEEvent, error)
}

// ClientOption configures an HTTPClient.
type ClientOption func(*HTTPClient)

// WithHTTPClient sets a custom *http.Client for requests.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *HTTPClient) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithDiscoverOptions sets server discovery options.
func WithDiscoverOptions(opts DiscoverOptions) ClientOption {
	return func(c *HTTPClient) {
		c.discoverOpts = opts
	}
}

// WithEndpoint pins a fixed EndpointConfig (skips discovery; useful for tests).
func WithEndpoint(ep EndpointConfig) ClientOption {
	return func(c *HTTPClient) {
		ep.URL = strings.TrimRight(strings.TrimSpace(ep.URL), "/")
		if ep.Username == "" {
			ep.Username = defaultUsername
		}
		c.pinnedEndpoint = &ep
	}
}

// HTTPClient implements Client against an OpenCode v2 HTTP/SSE server.
type HTTPClient struct {
	httpClient     *http.Client
	discoverOpts   DiscoverOptions
	pinnedEndpoint *EndpointConfig

	mu       sync.RWMutex
	endpoint *EndpointConfig
}

// NewHTTPClient creates an HTTPClient with sensible defaults.
func NewHTTPClient(opts ...ClientOption) *HTTPClient {
	c := &HTTPClient{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		discoverOpts: DiscoverOptions{
			AutoStartService: true,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

func (c *HTTPClient) resolveEndpoint(ctx context.Context, forceRefresh bool) (EndpointConfig, error) {
	if c.pinnedEndpoint != nil {
		return *c.pinnedEndpoint, nil
	}

	if !forceRefresh {
		c.mu.RLock()
		if c.endpoint != nil {
			ep := *c.endpoint
			c.mu.RUnlock()
			return ep, nil
		}
		c.mu.RUnlock()
	}

	ep, err := DiscoverServer(ctx, c.discoverOpts)
	if err != nil {
		return EndpointConfig{}, err
	}

	c.mu.Lock()
	c.endpoint = &ep
	c.mu.Unlock()
	return ep, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, method, path string, query url.Values, reqBody any, out any) error {
	ep, err := c.resolveEndpoint(ctx, false)
	if err != nil {
		return err
	}

	err = c.doJSONWithEndpoint(ctx, ep, method, path, query, reqBody, out)
	if err != nil && c.pinnedEndpoint == nil && shouldRetryDiscovery(err) {
		// Re-discover once in case OpenCode Desktop restarted on a new port/password.
		if refreshed, rErr := c.resolveEndpoint(ctx, true); rErr == nil && (refreshed.URL != ep.URL || refreshed.Password != ep.Password) {
			return c.doJSONWithEndpoint(ctx, refreshed, method, path, query, reqBody, out)
		}
	}
	return err
}

func shouldRetryDiscovery(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnauthorized) {
		return true
	}
	// Network / connection refused errors (not context cancellation or API 4xx/5xx)
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func (c *HTTPClient) doJSONWithEndpoint(ctx context.Context, ep EndpointConfig, method, path string, query url.Values, reqBody any, out any) error {
	fullURL := ep.URL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("opencode marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("opencode build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ep.Password != "" {
		user := ep.Username
		if user == "" {
			user = defaultUsername
		}
		req.SetBasicAuth(user, ep.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("opencode %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("opencode read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			RawBody:    string(respBytes),
		}
		_ = json.Unmarshal(respBytes, apiErr)
		return apiErr
	}

	if out == nil || len(bytes.TrimSpace(respBytes)) == 0 {
		return nil
	}

	if err := json.Unmarshal(respBytes, out); err != nil {
		return fmt.Errorf("opencode decode response for %s %s: %w", method, path, err)
	}
	return nil
}

func (c *HTTPClient) Info(ctx context.Context) (*ServerInfo, error) {
	var info ServerInfo
	if err := c.doJSON(ctx, http.MethodGet, "/api/info", nil, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *HTTPClient) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.doJSON(ctx, http.MethodGet, "/api/project", nil, nil, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (c *HTTPClient) ListSessions(ctx context.Context, opts ListSessionsOptions) ([]SessionInfo, error) {
	q := url.Values{}
	if opts.Directory != "" {
		q.Set("directory", opts.Directory)
	}
	if opts.Project != "" {
		q.Set("project", opts.Project)
	}
	if opts.ParentID != "" {
		q.Set("parentID", opts.ParentID)
	}
	if opts.Archived != nil {
		q.Set("archived", strconv.FormatBool(*opts.Archived))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Order != "" {
		q.Set("order", opts.Order)
	}

	var resp struct {
		Data []SessionInfo `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *HTTPClient) CreateSession(ctx context.Context, req CreateSessionRequest) (*SessionInfo, error) {
	var resp struct {
		Data SessionInfo `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/session", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *HTTPClient) GetActiveSessions(ctx context.Context) (map[string]SessionActive, error) {
	var resp struct {
		Data map[string]SessionActive `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/active", nil, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		resp.Data = make(map[string]SessionActive)
	}
	return resp.Data, nil
}

func (c *HTTPClient) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode get session: sessionID is required")
	}
	var resp struct {
		Data SessionInfo `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID), nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *HTTPClient) Prompt(ctx context.Context, sessionID string, req PromptRequest) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("opencode prompt: sessionID is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/prompt", nil, req, nil)
}

func (c *HTTPClient) Interrupt(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("opencode interrupt: sessionID is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/api/session/"+url.PathEscape(sessionID)+"/interrupt", nil, nil, nil)
}

func (c *HTTPClient) ListMessages(ctx context.Context, sessionID string, limit int, order string) ([]MessageInfo, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode list messages: sessionID is required")
	}
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if order != "" {
		q.Set("order", order)
	}
	var resp struct {
		Data []MessageInfo `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/message", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *HTTPClient) ListPermissions(ctx context.Context, sessionID string) ([]PermissionRequest, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode list permissions: sessionID is required")
	}
	var resp struct {
		Data []PermissionRequest `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/permission", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *HTTPClient) ListForms(ctx context.Context, sessionID string) ([]FormRequest, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("opencode list forms: sessionID is required")
	}
	var resp struct {
		Data []FormRequest `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID)+"/form", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// SubscribeEvents opens a Server-Sent Events stream against GET /api/event.
// The returned channel closes when ctx is canceled or the stream ends.
func (c *HTTPClient) SubscribeEvents(ctx context.Context, types []string) (<-chan SSEEvent, error) {
	ep, err := c.resolveEndpoint(ctx, false)
	if err != nil {
		return nil, err
	}

	fullURL := ep.URL + "/api/event"
	if len(types) > 0 {
		q := url.Values{}
		q.Set("types", strings.Join(types, ","))
		fullURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("opencode build sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if ep.Password != "" {
		user := ep.Username
		if user == "" {
			user = defaultUsername
		}
		req.SetBasicAuth(user, ep.Password)
	}

	// Use a client without a fixed request timeout for long-lived SSE streams.
	streamClient := &http.Client{
		Transport: c.httpClient.Transport,
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode subscribe events: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			RawBody:    string(body),
		}
		_ = json.Unmarshal(body, apiErr)
		return nil, apiErr
	}

	ch := make(chan SSEEvent, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Increase buffer to 1MB for large SSE payloads.
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		var eventName string
		var dataLines []string

		flush := func() bool {
			if len(dataLines) == 0 {
				eventName = ""
				return true
			}
			payload := strings.Join(dataLines, "\n")
			ev := SSEEvent{
				Event: eventName,
				Data:  json.RawMessage(payload),
			}
			eventName = ""
			dataLines = nil
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		_ = flush()
	}()

	return ch, nil
}
