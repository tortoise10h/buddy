package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/t3"
)

func TestT3Engine_Operations(t *testing.T) {
	var interrupted bool
	var archived bool
	var sentText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/projects" {
			_ = json.NewEncoder(w).Encode(t3.ProjectSnapshot{Projects: []t3.T3Project{
				{ID: "proj_engine_1", Title: "EngineProject", WorkspaceRoot: "/tmp/engine-proj"},
			}})
			return
		}

		if r.URL.Path == "/api/orchestration/threads/th_123/transcript" {
			_ = json.NewEncoder(w).Encode(t3.ThreadTranscript{
				ThreadID: "th_123",
				Title:    "feat-test",
				Items: []t3.TranscriptTurnItem{
					{Position: 1, Item: t3.TurnItemPayload{Type: "assistant_message", Text: "Hello from T3"}},
				},
			})
			return
		}

		if r.URL.Path == "/mcp" {
			var req t3.McpRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			if req.Method == "initialize" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"jsonrpc": "2.0", "id": 1, "result": {"protocolVersion": "2025-06-18"}}`))
				return
			}
			if req.Method == "notifications/initialized" {
				w.WriteHeader(http.StatusAccepted)
				return
			}

			if req.Method == "tools/call" {
				name, _ := req.Params["name"].(string)
				args, _ := req.Params["arguments"].(map[string]any)

				switch name {
				case "t3_thread_launch":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{
								{"type": "text", "text": `{"threadId": "th_123", "projectId": "proj_engine_1", "runId": "run_1"}`},
							},
						},
					})
					return

				case "t3_pending_request":
					// No pending requests
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{
								{"type": "text", "text": `{"requestIds": []}`},
							},
						},
					})
					return

				case "t3_thread_send":
					sentText, _ = args["message"].(string)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{
								{"type": "text", "text": `{"runId": "run_send_1"}`},
							},
						},
					})
					return

				case "t3_thread_interrupt":
					interrupted = true
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": `{"interrupted": true}`}},
						},
					})
					return

				case "t3_thread_organize":
					archived = true
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": `{"sequence": 1}`}},
						},
					})
					return
				}
			}
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	t3Backend := t3.NewT3Backend(
		t3.WithServerURL(server.URL),
		t3.WithTokenProvider(t3.NewSessionTokenProvider(t3.WithStaticSessionToken("token"))),
		t3.WithMCPClient(t3.NewMCPClient(
			t3.WithMCPBaseURL(server.URL),
			t3.WithMCPTokenProvider(&t3.StaticMCPTokenProvider{Token: "token"}),
		)),
	)

	eng := NewT3Engine(t3Backend)
	ctx := context.Background()

	if eng.Name() != "t3code" {
		t.Errorf("expected engine name t3code, got %s", eng.Name())
	}

	// 1. Dispatch
	res, err := eng.Dispatch(ctx, DispatchRequest{
		ProjectName: "EngineProject",
		ProjectPath: "/tmp/engine-proj",
		TaskSlug:    "feat-auth",
		Prompt:      "setup auth",
		AgentCmd:    []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if !res.PickedUp || res.WindowID != "th_123" {
		t.Errorf("unexpected dispatch result: %+v", res)
	}

	// 2. Capture
	out, err := eng.Capture(ctx, "proj_engine_1", "th_123", 10)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if out == "" {
		t.Errorf("expected capture output, got empty")
	}

	// 3. Send message
	err = eng.Send(ctx, "proj_engine_1", "th_123", backend.Input{Text: "continue work"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if sentText != "continue work" {
		t.Errorf("expected sentText 'continue work', got '%s'", sentText)
	}

	// 4. Send interrupt (ctrl+c)
	err = eng.Send(ctx, "proj_engine_1", "th_123", backend.Input{Key: "ctrl+c"})
	if err != nil {
		t.Fatalf("Send ctrl+c failed: %v", err)
	}
	if !interrupted {
		t.Errorf("expected interrupted to be true")
	}

	// 5. Close
	err = eng.Close(ctx, "proj_engine_1", "th_123")
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !archived {
		t.Errorf("expected archived to be true")
	}

	// 6. Focus (clean no-op)
	if err := eng.Focus(ctx, "proj_engine_1", "th_123"); err != nil {
		t.Errorf("Focus should be clean no-op, got: %v", err)
	}
}
