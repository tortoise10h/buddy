package t3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

func TestMCPClient_CallTool_Success(t *testing.T) {
	expectedToken := "mcp_oauth_secret_token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+expectedToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		var req McpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.JSONRPC != "2.0" {
			t.Errorf("expected jsonrpc 2.0, got %s", req.JSONRPC)
		}
		if req.Method != "tools/call" {
			t.Errorf("expected method tools/call, got %s", req.Method)
		}
		if req.Params["name"] != "t3_project_list" {
			t.Errorf("expected tool name t3_project_list, got %v", req.Params["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"content": [
					{"type": "text", "text": "{\"projects\":[]}"}
				],
				"isError": false
			}
		}`))
	}))
	defer server.Close()

	client := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: expectedToken}),
	)

	result, err := client.CallTool(context.Background(), "t3_project_list", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != `{"projects":[]}` {
		t.Errorf("unexpected tool result: %+v", result)
	}
}

func TestMCPClient_CallTool_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		errChecker func(error) bool
	}{
		{
			name: "401 invalid_mcp_credential returns ErrMcpAuthRequired",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="...", error="invalid_token"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "invalid_mcp_credential", "message": "A valid T3 Code MCP credential is required."}`))
			},
			errChecker: func(err error) bool {
				return errors.Is(err, ErrMcpAuthRequired)
			},
		},
		{
			name: "401 standard returns ErrAuthInvalid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
			},
			errChecker: func(err error) bool {
				return errors.Is(err, ErrAuthInvalid)
			},
		},
		{
			name: "JSON-RPC error payload",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"jsonrpc": "2.0",
					"id": 1,
					"error": {
						"code": -32601,
						"message": "Tool not found"
					}
				}`))
			},
			errChecker: func(err error) bool {
				var mcpErr *McpError
				return errors.As(err, &mcpErr) && mcpErr.Code == -32601
			},
		},
		{
			name: "tool execution error (isError = true)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"jsonrpc": "2.0",
					"id": 1,
					"result": {
						"content": [
							{"type": "text", "text": "Project does not exist"}
						],
						"isError": true
					}
				}`))
			},
			errChecker: func(err error) bool {
				return err != nil && errors.New(err.Error()).Error() == "mcp tool error: Project does not exist"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewMCPClient(
				WithMCPBaseURL(server.URL),
				WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
			)

			_, err := client.CallTool(context.Background(), "any_tool", nil)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.errChecker(err) {
				t.Fatalf("error check failed: %v", err)
			}
		})
	}
}

func TestMCPClient_Phase2DeferredMethods(t *testing.T) {
	client := NewMCPClient(WithMCPBaseURL("http://127.0.0.1:3773"))
	ctx := context.Background()

	// LaunchThread
	_, err := client.LaunchThread(ctx, LaunchThreadParams{})
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for LaunchThread, got: %v", err)
	}

	// RespondPendingRequest
	err = client.RespondPendingRequest(ctx, RespondPendingRequestParams{})
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for RespondPendingRequest, got: %v", err)
	}

	// InterruptThread
	err = client.InterruptThread(ctx, "th_123")
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for InterruptThread, got: %v", err)
	}

	// ArchiveThread
	err = client.ArchiveThread(ctx, "th_123")
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for ArchiveThread, got: %v", err)
	}
}
