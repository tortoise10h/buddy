package t3

import (
	"bufio"
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
	baseURL         string
	httpClient      *http.Client
	tokenProvider   MCPTokenProvider
	reqIDCounter    uint64
	timeout         time.Duration
	sessionID       string
	protocolVersion string
	initialized     bool
	mu              sync.RWMutex
	initMu          sync.Mutex
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

// WithMCPTimeout sets a custom default request timeout.
func WithMCPTimeout(d time.Duration) MCPOption {
	return func(c *MCPClient) {
		c.timeout = d
	}
}

// NewMCPClient creates a new MCP client.
func NewMCPClient(opts ...MCPOption) *MCPClient {
	c := &MCPClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		timeout:         30 * time.Second,
		protocolVersion: "2025-06-18",
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.tokenProvider == nil {
		c.tokenProvider = NewFileMCPTokenProvider("")
	}
	return c
}

// SetBaseURL updates the base URL and invalidates any open session if the URL changed.
func (c *MCPClient) SetBaseURL(url string) {
	trimmed := strings.TrimRight(url, "/")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseURL != trimmed {
		c.baseURL = trimmed
		c.initialized = false
		c.sessionID = ""
	}
}

// BaseURL returns the configured base URL.
func (c *MCPClient) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// SessionID returns the current MCP session ID if initialized.
func (c *MCPClient) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// InvalidateSession clears the initialized session state.
func (c *MCPClient) InvalidateSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = false
	c.sessionID = ""
}

// EnsureSession performs the MCP initialize handshake and notifications/initialized
// exchange with the T3 Code MCP server if not already established.
func (c *MCPClient) EnsureSession(ctx context.Context) error {
	c.mu.RLock()
	if c.initialized {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.initMu.Lock()
	defer c.initMu.Unlock()

	// Double check under lock
	c.mu.RLock()
	if c.initialized {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	base := c.BaseURL()
	if base == "" {
		return fmt.Errorf("mcp base URL is not configured")
	}

	initReq := McpRequest{
		JSONRPC: "2.0",
		ID:      "buddy-init-1",
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "buddy-ctl",
				"version": "0.1.0",
			},
		},
	}

	reqBody, err := json.Marshal(initReq)
	if err != nil {
		return fmt.Errorf("failed to encode mcp initialize request: %w", err)
	}

	mcpURL := fmt.Sprintf("%s/mcp", base)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create mcp initialize request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	if c.tokenProvider != nil {
		token, err := c.tokenProvider.GetMCPToken(ctx)
		if err != nil {
			return err
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("mcp initialize request failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		authHeader := resp.Header.Get("WWW-Authenticate")
		bodyStr := string(body)
		if strings.Contains(bodyStr, "invalid_mcp_credential") || strings.Contains(authHeader, "invalid_token") {
			return fmt.Errorf("%w: %s. Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired, strings.TrimSpace(bodyStr))
		}
		return fmt.Errorf("%w: %s", ErrAuthInvalid, strings.TrimSpace(bodyStr))
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("mcp initialize returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	sessionID := resp.Header.Get("mcp-session-id")

	rpcResp, err := parseMCPResponse(resp)
	if err != nil {
		return fmt.Errorf("failed to parse mcp initialize response: %w", err)
	}

	if rpcResp.Error != nil {
		return rpcResp.Error
	}

	protoVersion := "2025-06-18"
	if len(rpcResp.Result) > 0 {
		var initRes struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(rpcResp.Result, &initRes); err == nil && initRes.ProtocolVersion != "" {
			protoVersion = initRes.ProtocolVersion
		}
	}

	// Step 2: send notifications/initialized
	notifReq := McpRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	notifBody, err := json.Marshal(notifReq)
	if err == nil {
		notifHttpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(notifBody))
		if err == nil {
			notifHttpReq.Header.Set("Content-Type", "application/json")
			notifHttpReq.Header.Set("Accept", "application/json, text/event-stream")
			if c.tokenProvider != nil {
				if token, err := c.tokenProvider.GetMCPToken(ctx); err == nil {
					notifHttpReq.Header.Set("Authorization", "Bearer "+token)
				}
			}
			if sessionID != "" {
				notifHttpReq.Header.Set("mcp-session-id", sessionID)
			}
			notifHttpReq.Header.Set("mcp-protocol-version", protoVersion)

			notifResp, err := c.httpClient.Do(notifHttpReq)
			if err == nil {
				if sid := notifResp.Header.Get("mcp-session-id"); sid != "" {
					sessionID = sid
				}
				io.Copy(io.Discard, notifResp.Body)
				notifResp.Body.Close()
			}
		}
	}

	c.mu.Lock()
	c.sessionID = sessionID
	c.protocolVersion = protoVersion
	c.initialized = true
	c.mu.Unlock()

	return nil
}

// CallTool executes a named tool over POST /mcp.
func (c *MCPClient) CallTool(ctx context.Context, toolName string, arguments any) (*McpToolResult, error) {
	if err := c.EnsureSession(ctx); err != nil {
		return nil, err
	}

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

	c.mu.RLock()
	sessionID := c.sessionID
	protoVersion := c.protocolVersion
	c.mu.RUnlock()

	if sessionID != "" {
		httpReq.Header.Set("mcp-session-id", sessionID)
	}
	if protoVersion != "" {
		httpReq.Header.Set("mcp-protocol-version", protoVersion)
	}

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

	if resp.StatusCode == http.StatusUnauthorized {
		c.InvalidateSession()
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		authHeader := resp.Header.Get("WWW-Authenticate")
		bodyStr := string(body)
		if strings.Contains(bodyStr, "invalid_mcp_credential") || strings.Contains(authHeader, "invalid_token") {
			return nil, fmt.Errorf("%w: %s. Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired, strings.TrimSpace(bodyStr))
		}
		return nil, fmt.Errorf("%w: %s", ErrAuthInvalid, strings.TrimSpace(bodyStr))
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("mcp request returned unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Update session ID if header changed
	if newSessionID := resp.Header.Get("mcp-session-id"); newSessionID != "" {
		c.mu.Lock()
		c.sessionID = newSessionID
		c.mu.Unlock()
	}

	rpcResp, err := parseMCPResponse(resp)
	if err != nil {
		return nil, err
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

	// Check if toolResult indicates error
	if toolResult.IsError {
		errMsg := "tool execution failed"
		if len(toolResult.Content) > 0 && toolResult.Content[0].Text != "" {
			errMsg = toolResult.Content[0].Text
		}
		return &toolResult, fmt.Errorf("mcp tool error: %s", errMsg)
	}

	// Check for OrchestratorMcpFailure in content or result
	if err := checkMcpFailure(&toolResult, rpcResp.Result); err != nil {
		return &toolResult, err
	}

	return &toolResult, nil
}

// parseMCPResponse decodes either a standard JSON McpResponse or text/event-stream SSE events.
func parseMCPResponse(resp *http.Response) (*McpResponse, error) {
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		var eventData strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteString("\n")
				}
				eventData.WriteString(data)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read sse event stream: %w", err)
		}
		if eventData.Len() == 0 {
			return &McpResponse{}, nil
		}
		var rpcResp McpResponse
		if err := json.Unmarshal([]byte(eventData.String()), &rpcResp); err != nil {
			return nil, fmt.Errorf("failed to decode sse json-rpc response: %w", err)
		}
		return &rpcResp, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read mcp response body: %w", err)
	}
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return &McpResponse{}, nil
	}

	var rpcResp McpResponse
	if err := json.Unmarshal(bodyBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode mcp json-rpc response: %w", err)
	}
	return &rpcResp, nil
}

// checkMcpFailure inspects tool result payload for OrchestratorMcpFailure payloads.
func checkMcpFailure(res *McpToolResult, rawResult json.RawMessage) error {
	type failureShape struct {
		Tag     string `json:"_tag"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}

	checkBytes := func(data []byte) error {
		var f failureShape
		if err := json.Unmarshal(data, &f); err == nil && (f.Tag == "OrchestratorMcpFailure" || f.Code != "") {
			if f.Code == "capability_denied" {
				return fmt.Errorf("%w: capability denied: %s. Run 'buddy-ctl t3 auth login' with Full access", ErrMcpAuthRequired, f.Message)
			}
			return fmt.Errorf("mcp tool failure [%s]: %s", f.Code, f.Message)
		}
		return nil
	}

	if len(res.Content) > 0 && res.Content[0].Text != "" {
		if err := checkBytes([]byte(res.Content[0].Text)); err != nil {
			return err
		}
	}
	if len(rawResult) > 0 {
		if err := checkBytes(rawResult); err != nil {
			return err
		}
	}
	return nil
}

// extractToolPayload decodes tool result content (either wrapped in McpContent.Text or directly).
func extractToolPayload(res *McpToolResult, target any) error {
	if res == nil {
		return fmt.Errorf("nil tool result")
	}
	if len(res.Content) > 0 && res.Content[0].Text != "" {
		if err := json.Unmarshal([]byte(res.Content[0].Text), target); err == nil {
			return nil
		}
	}
	// Fallback to re-encoding content
	if data, err := json.Marshal(res); err == nil {
		if err := json.Unmarshal(data, target); err == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to decode tool payload from result")
}

// -------------------------------------------------------------------------
// Phase 2 Mutation Tool Implementations
// -------------------------------------------------------------------------

// LaunchThread creates a new thread in T3 Code via MCP tool t3_thread_launch.
func (c *MCPClient) LaunchThread(ctx context.Context, params LaunchThreadParams) (*ThreadLaunchResult, error) {
	if params.Title == "" {
		return nil, fmt.Errorf("thread title cannot be empty")
	}

	args := map[string]any{
		"title": params.Title,
	}
	if params.ProjectID != "" {
		args["projectId"] = params.ProjectID
	}
	if params.Message != "" {
		args["message"] = params.Message
	}
	if params.RuntimeMode != "" {
		args["runtimeMode"] = params.RuntimeMode
	} else {
		args["runtimeMode"] = "full-access"
	}
	if params.ModelSelection != nil {
		args["modelSelection"] = params.ModelSelection
	}
	if params.WorkspaceStrategy != nil {
		args["workspaceStrategy"] = params.WorkspaceStrategy
	}

	res, err := c.CallTool(ctx, "t3_thread_launch", args)
	if err != nil {
		return nil, fmt.Errorf("t3_thread_launch failed: %w", err)
	}

	var result ThreadLaunchResult
	if err := extractToolPayload(res, &result); err != nil {
		return nil, fmt.Errorf("failed to parse t3_thread_launch result: %w", err)
	}

	if result.ThreadID == "" {
		return nil, fmt.Errorf("t3_thread_launch returned empty threadId")
	}

	return &result, nil
}

// CreateProject registers a project directory in T3 Code via MCP tool t3_project_create.
func (c *MCPClient) CreateProject(ctx context.Context, title, workspaceRoot string) (*T3Project, error) {
	if title == "" || workspaceRoot == "" {
		return nil, fmt.Errorf("title and workspaceRoot must not be empty")
	}

	args := map[string]any{
		"title":         title,
		"workspaceRoot": workspaceRoot,
	}

	res, err := c.CallTool(ctx, "t3_project_create", args)
	if err != nil {
		return nil, fmt.Errorf("t3_project_create failed: %w", err)
	}

	var proj T3Project
	if err := extractToolPayload(res, &proj); err != nil {
		return nil, fmt.Errorf("failed to parse t3_project_create result: %w", err)
	}

	if proj.ID == "" {
		return nil, fmt.Errorf("t3_project_create returned empty project ID")
	}

	return &proj, nil
}

// SendThread queues a new turn or message on a thread via MCP tool t3_thread_send.
func (c *MCPClient) SendThread(ctx context.Context, threadID, message, mode string) (*McpThreadSendResult, error) {
	if threadID == "" {
		return nil, fmt.Errorf("threadID cannot be empty")
	}
	if message == "" {
		return nil, fmt.Errorf("message cannot be empty")
	}

	if mode == "" {
		mode = "auto"
	}

	args := map[string]any{
		"threadId": threadID,
		"message":  message,
		"mode":     mode,
	}

	res, err := c.CallTool(ctx, "t3_thread_send", args)
	if err != nil {
		return nil, fmt.Errorf("t3_thread_send failed: %w", err)
	}

	var result McpThreadSendResult
	if err := extractToolPayload(res, &result); err != nil {
		return nil, fmt.Errorf("failed to parse t3_thread_send result: %w", err)
	}

	return &result, nil
}

// InterruptThread interrupts an active run on a thread via MCP tool t3_thread_interrupt.
func (c *MCPClient) InterruptThread(ctx context.Context, threadID string) error {
	if threadID == "" {
		return fmt.Errorf("threadID cannot be empty")
	}

	args := map[string]any{
		"threadId": threadID,
		"stop":     true,
	}

	_, err := c.CallTool(ctx, "t3_thread_interrupt", args)
	if err != nil {
		// Ignore if thread is not currently running / interruptible
		if strings.Contains(err.Error(), "thread_not_interruptible") || strings.Contains(err.Error(), "run_not_found") {
			return nil
		}
		return fmt.Errorf("t3_thread_interrupt failed: %w", err)
	}

	return nil
}

// ArchiveThread archives a thread via MCP tool t3_thread_organize.
func (c *MCPClient) ArchiveThread(ctx context.Context, threadID string) error {
	if threadID == "" {
		return fmt.Errorf("threadID cannot be empty")
	}

	args := map[string]any{
		"threadId": threadID,
		"action":   "archive",
	}

	_, err := c.CallTool(ctx, "t3_thread_organize", args)
	if err != nil {
		return fmt.Errorf("t3_thread_organize archive failed: %w", err)
	}

	return nil
}

// ListPendingRequests lists pending user_input question IDs on a thread via t3_pending_request action="list".
func (c *MCPClient) ListPendingRequests(ctx context.Context, threadID string) ([]string, error) {
	args := map[string]any{
		"action": "list",
	}
	if threadID != "" {
		args["threadId"] = threadID
	}

	res, err := c.CallTool(ctx, "t3_pending_request", args)
	if err != nil {
		return nil, fmt.Errorf("t3_pending_request list failed: %w", err)
	}

	var list PendingQuestionList
	if err := extractToolPayload(res, &list); err != nil {
		return nil, fmt.Errorf("failed to parse t3_pending_request list result: %w", err)
	}

	return list.RequestIDs, nil
}

// ReadPendingRequest reads the structured questions for a pending request via t3_pending_request action="read".
func (c *MCPClient) ReadPendingRequest(ctx context.Context, threadID, requestID string) (*PendingQuestionDetail, error) {
	if requestID == "" {
		return nil, fmt.Errorf("requestID cannot be empty")
	}

	args := map[string]any{
		"action":    "read",
		"requestId": requestID,
	}
	if threadID != "" {
		args["threadId"] = threadID
	}

	res, err := c.CallTool(ctx, "t3_pending_request", args)
	if err != nil {
		return nil, fmt.Errorf("t3_pending_request read failed: %w", err)
	}

	var detail PendingQuestionDetail
	if err := extractToolPayload(res, &detail); err != nil {
		return nil, fmt.Errorf("failed to parse t3_pending_request read result: %w", err)
	}

	return &detail, nil
}

// RespondPendingRequest submits structured answers for a pending request via t3_pending_request action="respond".
func (c *MCPClient) RespondPendingRequest(ctx context.Context, threadID, requestID string, answers map[string]any) error {
	if requestID == "" {
		return fmt.Errorf("requestID cannot be empty")
	}
	if len(answers) == 0 {
		return fmt.Errorf("answers cannot be empty")
	}

	args := map[string]any{
		"action":    "respond",
		"requestId": requestID,
		"answers":   answers,
	}
	if threadID != "" {
		args["threadId"] = threadID
	}

	_, err := c.CallTool(ctx, "t3_pending_request", args)
	if err != nil {
		return fmt.Errorf("t3_pending_request respond failed: %w", err)
	}

	return nil
}
