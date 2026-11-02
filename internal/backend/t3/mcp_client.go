package t3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
)

// MCPTokenProvider returns the active 30-day OAuth token for POST /mcp.
type MCPTokenProvider interface {
	GetMCPToken(ctx context.Context) (string, error)
}

// StaticMCPTokenProvider provides a fixed token.
type StaticMCPTokenProvider struct {
	Token string
}

func (s *StaticMCPTokenProvider) GetMCPToken(ctx context.Context) (string, error) {
	if s.Token == "" {
		return "", ErrMcpAuthRequired
	}
	return s.Token, nil
}

// MCPClient executes JSON-RPC 2.0 tool calls against the T3 Code Streamable HTTP /mcp endpoint.
type MCPClient struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider MCPTokenProvider
	reqIDCounter  uint64
	mu            sync.RWMutex
}

// MCPOption configures MCPClient.
type MCPOption func(*MCPClient)

// WithMCPBaseURL sets the base server URL for MCP requests.
func WithMCPBaseURL(url string) MCPOption {
	return func(c *MCPClient) {
		c.baseURL = strings.TrimRight(url, "/")
	}
}

// WithMCPHTTPClient sets a custom HTTP client.
func WithMCPHTTPClient(hc *http.Client) MCPOption {
	return func(c *MCPClient) {
		c.httpClient = hc
	}
}

// WithMCPTokenProvider sets the token provider.
func WithMCPTokenProvider(p MCPTokenProvider) MCPOption {
	return func(c *MCPClient) {
		c.tokenProvider = p
	}
}

// NewMCPClient creates a new MCP client.
func NewMCPClient(opts ...MCPOption) *MCPClient {
	c := &MCPClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetBaseURL updates the base URL.
func (c *MCPClient) SetBaseURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(url, "/")
}

// BaseURL returns the configured base URL.
func (c *MCPClient) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// CallTool executes a named tool over POST /mcp.
func (c *MCPClient) CallTool(ctx context.Context, toolName string, arguments any) (*McpToolResult, error) {
	base := c.BaseURL()
	if base == "" {
		return nil, fmt.Errorf("mcp base URL is not configured")
	}

	reqID := atomic.AddUint64(&c.reqIDCounter, 1)

	params := map[string]any{
		"name": toolName,
	}
	if arguments != nil {
		params["arguments"] = arguments
	} else {
		params["arguments"] = map[string]any{}
	}

	rpcReq := McpRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params:  params,
	}

	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("failed to encode mcp request: %w", err)
	}

	mcpURL := fmt.Sprintf("%s/mcp", base)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create mcp request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Inject bearer token
	if c.tokenProvider != nil {
		token, err := c.tokenProvider.GetMCPToken(ctx)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		authHeader := resp.Header.Get("WWW-Authenticate")
		bodyStr := string(body)
		if strings.Contains(bodyStr, "invalid_mcp_credential") || strings.Contains(authHeader, "invalid_token") {
			return nil, fmt.Errorf("%w: %s", ErrMcpAuthRequired, strings.TrimSpace(bodyStr))
		}
		return nil, fmt.Errorf("%w: %s", ErrAuthInvalid, strings.TrimSpace(bodyStr))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mcp request returned unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rpcResp McpResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode mcp json-rpc response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	if len(rpcResp.Result) == 0 {
		return &McpToolResult{}, nil
	}

	var toolResult McpToolResult
	if err := json.Unmarshal(rpcResp.Result, &toolResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mcp tool result: %w", err)
	}

	if toolResult.IsError {
		errMsg := "tool execution failed"
		if len(toolResult.Content) > 0 && toolResult.Content[0].Text != "" {
			errMsg = toolResult.Content[0].Text
		}
		return &toolResult, fmt.Errorf("mcp tool error: %s", errMsg)
	}

	return &toolResult, nil
}

// -------------------------------------------------------------------------
// Mutation Tool Shells (Structured for Phase 2 - return ErrNotImplementedPhase1)
// -------------------------------------------------------------------------

// LaunchThread params shell for t3_thread_launch in Phase 2.
type LaunchThreadParams struct {
	ProjectID         string         `json:"projectId"`
	Title             string         `json:"title"`
	Message           string         `json:"message"`
	RuntimeMode       string         `json:"runtimeMode,omitempty"` // "full-access"
	ModelSelection    map[string]any `json:"modelSelection,omitempty"`
	WorkspaceStrategy map[string]any `json:"workspaceStrategy,omitempty"`
}

// LaunchThread is structured for Phase 2 implementation.
func (c *MCPClient) LaunchThread(ctx context.Context, params LaunchThreadParams) (any, error) {
	return nil, fmt.Errorf("LaunchThread: %w", backend.ErrNotImplementedPhase1)
}

// RespondPendingRequestParams shell for t3_pending_request in Phase 2.
type RespondPendingRequestParams struct {
	ThreadID  string         `json:"threadId"`
	Action    string         `json:"action"` // "respond"
	RequestID string         `json:"requestId"`
	Answers   map[string]any `json:"answers"`
}

// RespondPendingRequest is structured for Phase 2 implementation.
func (c *MCPClient) RespondPendingRequest(ctx context.Context, params RespondPendingRequestParams) error {
	return fmt.Errorf("RespondPendingRequest: %w", backend.ErrNotImplementedPhase1)
}

// InterruptThread calls t3_thread_interrupt in Phase 2.
func (c *MCPClient) InterruptThread(ctx context.Context, threadID string) error {
	return fmt.Errorf("InterruptThread: %w", backend.ErrNotImplementedPhase1)
}

// ArchiveThread calls t3_thread_organize in Phase 2.
func (c *MCPClient) ArchiveThread(ctx context.Context, threadID string) error {
	return fmt.Errorf("ArchiveThread: %w", backend.ErrNotImplementedPhase1)
}
