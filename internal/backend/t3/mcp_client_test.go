package t3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMCPServer is a test fixture providing standard MCP protocol behavior.
type fakeMCPServer struct {
	mu           sync.Mutex
	sessionID    string
	toolsHandler func(toolName string, args map[string]any) (any, bool, error)
	initCount    int32
	notifCount   int32
	callCount    int32
}

func newFakeMCPServer() *fakeMCPServer {
	return &fakeMCPServer{
		sessionID: "test-mcp-session-xyz",
	}
}

func (s *fakeMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}

	var req McpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "malformed json"}`))
		return
	}

	switch req.Method {
	case "initialize":
		atomic.AddInt32(&s.initCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("mcp-session-id", s.sessionID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": "buddy-init-1",
			"result": {
				"protocolVersion": "2025-06-18",
				"capabilities": {"tools": {}},
				"serverInfo": {"name": "T3 Code", "version": "0.1.6"}
			}
		}`))

	case "notifications/initialized":
		atomic.AddInt32(&s.notifCount, 1)
		w.Header().Set("mcp-session-id", s.sessionID)
		w.WriteHeader(http.StatusAccepted)

	case "tools/call":
		atomic.AddInt32(&s.callCount, 1)
		toolName, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)

		if s.toolsHandler != nil {
			resultData, isError, err := s.toolsHandler(toolName, args)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "%s"}`, err.Error())))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("mcp-session-id", s.sessionID)
			w.WriteHeader(http.StatusOK)

			var jsonContent string
			if str, ok := resultData.(string); ok {
				jsonContent = str
			} else {
				dataBytes, _ := json.Marshal(resultData)
				jsonContent = string(dataBytes)
			}

			respObj := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{
							"type": "text",
							"text": jsonContent,
						},
					},
					"isError": isError,
				},
			}
			_ = json.NewEncoder(w).Encode(respObj)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {"content": [{"type": "text", "text": "{}"}], "isError": false}
		}`))

	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "unknown method"}`))
	}
}

func TestMCPClient_InitializeHandshake(t *testing.T) {
	fake := newFakeMCPServer()
	server := httptest.NewServer(fake)
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "test-token"}),
	)

	// Explicit EnsureSession call
	if err := client.EnsureSession(context.Background()); err != nil {
		t.Fatalf("EnsureSession failed: %v", err)
	}

	if atomic.LoadInt32(&fake.initCount) != 1 {
		t.Errorf("expected 1 initialize call, got %d", atomic.LoadInt32(&fake.initCount))
	}
	if atomic.LoadInt32(&fake.notifCount) != 1 {
		t.Errorf("expected 1 notifications/initialized call, got %d", atomic.LoadInt32(&fake.notifCount))
	}
	if client.SessionID() != fake.sessionID {
		t.Errorf("expected sessionID %s, got %s", fake.sessionID, client.SessionID())
	}

	// Calling EnsureSession again should be a no-op (cached)
	if err := client.EnsureSession(context.Background()); err != nil {
		t.Fatalf("second EnsureSession failed: %v", err)
	}
	if atomic.LoadInt32(&fake.initCount) != 1 {
		t.Errorf("expected still 1 initialize call, got %d", atomic.LoadInt32(&fake.initCount))
	}
}

func TestMCPClient_SuccessfulToolCalls(t *testing.T) {
	fake := newFakeMCPServer()
	fake.toolsHandler = func(toolName string, args map[string]any) (any, bool, error) {
		switch toolName {
		case "t3_thread_launch":
			return map[string]any{
				"threadId":  "th_launch_123",
				"projectId": "proj_abc",
				"runId":     "run_999",
				"status":    "running",
			}, false, nil

		case "t3_project_create":
			return map[string]any{
				"id":            "proj_created_1",
				"title":         args["title"],
				"workspaceRoot": args["workspaceRoot"],
			}, false, nil

		case "t3_thread_send":
			return map[string]any{
				"threadId":  args["threadId"],
				"messageId": "msg_1",
				"runId":     "run_1001",
				"status":    "queued",
				"delivery":  "queued",
			}, false, nil

		case "t3_thread_interrupt":
			return map[string]any{
				"threadId":    args["threadId"],
				"interrupted": true,
			}, false, nil

		case "t3_thread_organize":
			return map[string]any{
				"sequence": 42,
			}, false, nil

		case "t3_pending_request":
			action := args["action"].(string)
			if action == "list" {
				return map[string]any{
					"requestIds": []string{"req_pending_1"},
				}, false, nil
			}
			if action == "read" {
				return map[string]any{
					"requestId": "req_pending_1",
					"questions": []map[string]any{
						{
							"id":       "q1",
							"header":   "Confirm Migration",
							"question": "Run db migration?",
							"options": []map[string]any{
								{"label": "Yes", "description": "Run it"},
								{"label": "No", "description": "Skip"},
							},
						},
					},
				}, false, nil
			}
			if action == "respond" {
				return map[string]any{
					"sequence": 43,
				}, false, nil
			}
		}
		return nil, true, fmt.Errorf("unknown tool %s", toolName)
	}

	server := httptest.NewServer(fake)
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "test-token"}),
	)
	ctx := context.Background()

	// 1. LaunchThread
	launchRes, err := client.LaunchThread(ctx, LaunchThreadParams{
		ProjectID: "proj_abc",
		Title:     "feat-dark-mode",
		Message:   "Implement dark mode",
	})
	if err != nil {
		t.Fatalf("LaunchThread failed: %v", err)
	}
	if launchRes.ThreadID != "th_launch_123" || *launchRes.RunID != "run_999" {
		t.Errorf("unexpected launch result: %+v", launchRes)
	}

	// 2. CreateProject
	projRes, err := client.CreateProject(ctx, "my-proj", "/tmp/my-proj")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if projRes.ID != "proj_created_1" || projRes.Title != "my-proj" {
		t.Errorf("unexpected create project result: %+v", projRes)
	}

	// 3. SendThread
	sendRes, err := client.SendThread(ctx, "th_launch_123", "Next turn message", "queue")
	if err != nil {
		t.Fatalf("SendThread failed: %v", err)
	}
	if sendRes.RunID != "run_1001" {
		t.Errorf("unexpected send result: %+v", sendRes)
	}

	// 4. InterruptThread
	if err := client.InterruptThread(ctx, "th_launch_123"); err != nil {
		t.Fatalf("InterruptThread failed: %v", err)
	}

	// 5. ArchiveThread
	if err := client.ArchiveThread(ctx, "th_launch_123"); err != nil {
		t.Fatalf("ArchiveThread failed: %v", err)
	}

	// 6. ListPendingRequests
	reqIDs, err := client.ListPendingRequests(ctx, "th_launch_123")
	if err != nil {
		t.Fatalf("ListPendingRequests failed: %v", err)
	}
	if len(reqIDs) != 1 || reqIDs[0] != "req_pending_1" {
		t.Errorf("unexpected pending requests: %v", reqIDs)
	}

	// 7. ReadPendingRequest
	detail, err := client.ReadPendingRequest(ctx, "th_launch_123", "req_pending_1")
	if err != nil {
		t.Fatalf("ReadPendingRequest failed: %v", err)
	}
	if len(detail.Questions) != 1 || detail.Questions[0].ID != "q1" {
		t.Errorf("unexpected question detail: %+v", detail)
	}

	// 8. RespondPendingRequest
	err = client.RespondPendingRequest(ctx, "th_launch_123", "req_pending_1", map[string]any{"q1": "Yes"})
	if err != nil {
		t.Fatalf("RespondPendingRequest failed: %v", err)
	}
}

func TestMCPClient_ToolErrors(t *testing.T) {
	fake := newFakeMCPServer()
	fake.toolsHandler = func(toolName string, args map[string]any) (any, bool, error) {
		if toolName == "tool_error" {
			return "Tool failed due to bad input", true, nil
		}
		if toolName == "tool_denied" {
			return map[string]any{
				"_tag":    "OrchestratorMcpFailure",
				"code":    "capability_denied",
				"message": "Full access permission is required",
			}, false, nil
		}
		return nil, false, nil
	}

	server := httptest.NewServer(fake)
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)
	ctx := context.Background()

	// 1. isError: true
	_, err := client.CallTool(ctx, "tool_error", nil)
	if err == nil || !strings.Contains(err.Error(), "Tool failed due to bad input") {
		t.Fatalf("expected tool error, got: %v", err)
	}

	// 2. capability_denied OrchestratorMcpFailure
	_, err = client.CallTool(ctx, "tool_denied", nil)
	if err == nil || !errors.Is(err, ErrMcpAuthRequired) || !strings.Contains(err.Error(), "capability denied") {
		t.Fatalf("expected ErrMcpAuthRequired capability denied, got: %v", err)
	}
}

func TestMCPClient_AuthExpiry(t *testing.T) {
	var authFailures int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&authFailures) > 0 {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "invalid_mcp_credential", "message": "Token expired"}`))
			return
		}

		var req McpRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": {"protocolVersion": "2025-06-18"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": {"content": [{"type": "text", "text": "{}"}]}}`))
	}))
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "test-token"}),
	)
	ctx := context.Background()

	// Initial call succeeds
	_, err := client.CallTool(ctx, "any_tool", nil)
	if err != nil {
		t.Fatalf("initial call failed: %v", err)
	}

	// Now simulate token expiry
	atomic.StoreInt32(&authFailures, 1)

	_, err = client.CallTool(ctx, "any_tool", nil)
	if err == nil {
		t.Fatalf("expected error on expired token, got nil")
	}
	if !errors.Is(err, ErrMcpAuthRequired) {
		t.Errorf("expected ErrMcpAuthRequired, got: %v", err)
	}
	if !strings.Contains(err.Error(), "buddy-ctl t3 auth login") {
		t.Errorf("expected message mentioning buddy-ctl t3 auth login, got: %v", err)
	}
}

func TestMCPClient_MalformedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json!!!`))
	}))
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)

	_, err := client.CallTool(context.Background(), "any_tool", nil)
	if err == nil {
		t.Fatalf("expected error for malformed response, got nil")
	}
}

func TestMCPClient_Timeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
		WithMCPTimeout(50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.CallTool(ctx, "any_tool", nil)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "context") {
		t.Logf("got timeout error: %v", err)
	}
}

func TestMCPClient_SSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req McpRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if req.Method == "initialize" {
			_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":\"buddy-init-1\",\"result\":{\"protocolVersion\":\"2025-06-18\"}}\n\n"))
			return
		}

		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"{\\\"status\\\":\\\"ok\\\"}\"}]}}\n\n"))
	}))
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)

	res, err := client.CallTool(context.Background(), "test_sse_tool", nil)
	if err != nil {
		t.Fatalf("SSE call failed: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != `{"status":"ok"}` {
		t.Errorf("unexpected SSE tool result: %+v", res)
	}
}
