package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

func TestT3Backend_CapabilitiesAndName(t *testing.T) {
	b := NewT3Backend(WithServerURL("http://127.0.0.1:3773"))

	if b.Name() != "t3code" {
		t.Errorf("expected name t3code, got %s", b.Name())
	}

	caps := b.Capabilities()
	if !caps.ManagesWorktrees {
		t.Errorf("expected ManagesWorktrees to be true")
	}
	if caps.SupportsFocus {
		t.Errorf("expected SupportsFocus to be false")
	}
	if caps.SupportsDemoSplit {
		t.Errorf("expected SupportsDemoSplit to be false")
	}
	if caps.SupportsRawKeys {
		t.Errorf("expected SupportsRawKeys to be false")
	}
}

func TestT3Backend_ReadOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/.well-known/t3/environment":
			_ = json.NewEncoder(w).Encode(EnvironmentDescriptor{
				EnvironmentID:                "env_test",
				ServerVersion:                "0.1.6",
				OrchestrationProtocolVersion: 2,
			})

		case "/api/projects":
			_ = json.NewEncoder(w).Encode(ProjectSnapshot{
				Projects: []T3Project{
					{
						ID:            "proj_alpha",
						Title:         "Docnotate",
						WorkspaceRoot: "/Users/test/Projects/docnotate",
					},
					{
						ID:            "proj_beta",
						Title:         "Daily Catch",
						WorkspaceRoot: "/Users/test/Projects/daily-catch",
					},
					{
						ID:            "proj_deleted",
						Title:         "Old Project",
						WorkspaceRoot: "/Users/test/Projects/old",
						DeletedAt:     strPtr("2026-10-01T00:00:00Z"),
					},
				},
			})

		case "/api/orchestration/shell":
			_ = json.NewEncoder(w).Encode(OrchestrationShellSnapshot{
				SchemaVersion: 1,
				Threads: []ThreadShell{
					{
						ID:                  "th_active",
						ProjectID:           "proj_alpha",
						Title:               "dark-mode",
						ProviderInstanceID:  "antigravity",
						Status:              "running",
						PendingRequestCount: 0,
					},
					{
						ID:                  "th_waiting",
						ProjectID:           "proj_alpha",
						Title:               "ask-auth",
						ProviderInstanceID:  "antigravity",
						Status:              "waiting",
						PendingRequestCount: 1,
						PendingRuntimeRequest: &PendingRuntimeRequestSummary{
							ID:   "req_999",
							Kind: "user_input",
						},
					},
				},
				ArchivedThreads: []ThreadShell{
					{
						ID:         "th_archived",
						ProjectID:  "proj_alpha",
						Title:      "old-task",
						Status:     "completed",
						ArchivedAt: strPtr("2026-10-15T00:00:00Z"),
					},
				},
			})

		case "/api/orchestration/threads/th_active/transcript":
			_ = json.NewEncoder(w).Encode(ThreadTranscript{
				ThreadID: "th_active",
				Title:    "dark-mode",
				Items: []TranscriptTurnItem{
					{
						Position: 1,
						Item: TurnItemPayload{
							Type: "user_message",
							Text: "Please implement dark mode.",
						},
					},
					{
						Position: 2,
						Item: TurnItemPayload{
							Type: "assistant_message",
							Text: "Dark mode implementation complete.",
						},
					},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenProvider := NewSessionTokenProvider(WithStaticSessionToken("test_token"))
	httpClient := NewHTTPClient(
		WithBaseURL(server.URL),
		WithHTTPTokenProvider(tokenProvider),
	)

	backendImpl := NewT3Backend(
		WithServerURL(server.URL),
		WithHTTPClient(httpClient),
		WithTokenProvider(tokenProvider),
	)

	ctx := context.Background()

	// 1. ListLiveProjects: excludes deleted project
	projects, err := backendImpl.ListLiveProjects(ctx)
	if err != nil {
		t.Fatalf("ListLiveProjects failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 live projects, got %d", len(projects))
	}
	if projects[0].Name != "Docnotate" || projects[1].Name != "Daily Catch" {
		t.Errorf("unexpected projects: %+v", projects)
	}

	// 2. EnsureProjectContainer: matches existing project by path or title
	projID, err := backendImpl.EnsureProjectContainer(ctx, "Docnotate", "/Users/test/Projects/docnotate")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}
	if projID != "proj_alpha" {
		t.Errorf("expected proj_alpha, got %s", projID)
	}

	// 3. IsTaskAlive
	alive, err := backendImpl.IsTaskAlive(ctx, backend.TaskHandle{ThreadID: "th_active"})
	if err != nil || !alive {
		t.Errorf("expected th_active to be alive, got alive=%v err=%v", alive, err)
	}

	alive, err = backendImpl.IsTaskAlive(ctx, backend.TaskHandle{ThreadID: "th_archived"})
	if err != nil || alive {
		t.Errorf("expected th_archived to not be alive, got alive=%v err=%v", alive, err)
	}

	alive, err = backendImpl.IsTaskAlive(ctx, backend.TaskHandle{ThreadID: "th_non_existent"})
	if err != nil || alive {
		t.Errorf("expected non-existent thread to not be alive, got alive=%v err=%v", alive, err)
	}

	// 4. GetAgentStatus
	statusActive, err := backendImpl.GetAgentStatus(ctx, backend.TaskHandle{ThreadID: "th_active"})
	if err != nil {
		t.Fatalf("GetAgentStatus active failed: %v", err)
	}
	if statusActive.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", statusActive.Status)
	}

	statusWaiting, err := backendImpl.GetAgentStatus(ctx, backend.TaskHandle{ThreadID: "th_waiting"})
	if err != nil {
		t.Fatalf("GetAgentStatus waiting failed: %v", err)
	}
	if statusWaiting.Status != "waiting_for_input" {
		t.Errorf("expected status 'waiting_for_input', got '%s'", statusWaiting.Status)
	}
	if statusWaiting.PendingRequestCount != 1 {
		t.Errorf("expected pendingRequestCount 1, got %d", statusWaiting.PendingRequestCount)
	}

	// 5. CaptureOutput
	transcriptOut, err := backendImpl.CaptureOutput(ctx, backend.TaskHandle{ThreadID: "th_active"}, 10)
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}
	if !strings.Contains(transcriptOut, "[User]: Please implement dark mode.") {
		t.Errorf("missing user message in transcript: %s", transcriptOut)
	}
	if !strings.Contains(transcriptOut, "[Assistant]: Dark mode implementation complete.") {
		t.Errorf("missing assistant message in transcript: %s", transcriptOut)
	}
}

func TestT3Backend_DispatchTask_EndToEnd(t *testing.T) {
	var mu sync.Mutex
	var launchedParams LaunchThreadParams
	var createdProjectTitle string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/projects":
			_ = json.NewEncoder(w).Encode(ProjectSnapshot{Projects: []T3Project{}})

		case "/mcp":
			var req McpRequest
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
				toolName, _ := req.Params["name"].(string)
				args, _ := req.Params["arguments"].(map[string]any)

				if toolName == "t3_project_create" {
					mu.Lock()
					createdProjectTitle = args["title"].(string)
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{
								{
									"type": "text",
									"text": `{"id": "proj_new_auto", "title": "NewProject", "workspaceRoot": "/tmp/new-project"}`,
								},
							},
						},
					})
					return
				}

				if toolName == "t3_thread_launch" {
					mu.Lock()
					launchedParams.ProjectID, _ = args["projectId"].(string)
					launchedParams.Title, _ = args["title"].(string)
					launchedParams.Message, _ = args["message"].(string)
					launchedParams.RuntimeMode, _ = args["runtimeMode"].(string)
					if ws, ok := args["workspaceStrategy"].(map[string]any); ok {
						launchedParams.WorkspaceStrategy = ws
					}
					mu.Unlock()

					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{
								{
									"type": "text",
									"text": `{"threadId": "th_dispatched_99", "projectId": "proj_new_auto", "runId": "run_initial_1", "status": "running"}`,
								},
							},
						},
					})
					return
				}
			}

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenProvider := NewSessionTokenProvider(WithStaticSessionToken("token"))
	httpClient := NewHTTPClient(
		WithBaseURL(server.URL),
		WithHTTPTokenProvider(tokenProvider),
	)
	mcpClient := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)

	b := NewT3Backend(
		WithServerURL(server.URL),
		WithHTTPClient(httpClient),
		WithMCPClient(mcpClient),
		WithTokenProvider(tokenProvider),
	)
	ctx := context.Background()

	spec := backend.DispatchSpec{
		ProjectName: "NewProject",
		ProjectPath: "/tmp/new-project",
		TaskSlug:    "feat-auth",
		Prompt:      "Implement OAuth login",
		AgentSpec:   "antigravity/gemini-4-argon-high",
		UseWorktree: true,
		BaseRef:     "main",
		Branch:      "feat-auth-branch",
	}

	result, err := b.DispatchTask(ctx, spec)
	if err != nil {
		t.Fatalf("DispatchTask failed: %v", err)
	}

	if !result.PickedUp {
		t.Errorf("expected PickedUp to be true")
	}
	if result.TaskHandle.ThreadID != "th_dispatched_99" {
		t.Errorf("expected threadId th_dispatched_99, got %s", result.TaskHandle.ThreadID)
	}
	if result.TaskHandle.ActiveRunID != "run_initial_1" {
		t.Errorf("expected runId run_initial_1, got %s", result.TaskHandle.ActiveRunID)
	}

	mu.Lock()
	defer mu.Unlock()
	if createdProjectTitle != "NewProject" {
		t.Errorf("expected project auto-registration title NewProject, got %s", createdProjectTitle)
	}
	if launchedParams.RuntimeMode != "full-access" {
		t.Errorf("expected runtimeMode full-access, got %s", launchedParams.RuntimeMode)
	}
	if launchedParams.WorkspaceStrategy["type"] != "worktree" || launchedParams.WorkspaceStrategy["baseRef"] != "main" {
		t.Errorf("unexpected workspaceStrategy: %+v", launchedParams.WorkspaceStrategy)
	}
}

func TestT3Backend_ProjectAutoRegistration_CLIFallback(t *testing.T) {
	// Fake server where MCP t3_project_create returns 401 unauthorized (auth unavailable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects" {
			_ = json.NewEncoder(w).Encode(ProjectSnapshot{Projects: []T3Project{}})
			return
		}
		if r.URL.Path == "/mcp" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "invalid_mcp_credential"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Mock command runner for 't3 project add'
	var executedCmd string
	mockRunner := func(ctx context.Context, name string, args ...string) (string, error) {
		executedCmd = fmt.Sprintf("%s %s", name, strings.Join(args, " "))
		return `{"id": "proj_cli_fallback_1", "title": "FallbackProject", "workspaceRoot": "/tmp/fallback"}`, nil
	}

	b := NewT3Backend(
		WithServerURL(server.URL),
		WithBackendBinPath("/usr/local/bin/t3"),
		WithBackendCommandRunner(mockRunner),
		WithTokenProvider(NewSessionTokenProvider(WithStaticSessionToken("token"))),
	)

	projID, err := b.EnsureProjectContainer(context.Background(), "FallbackProject", "/tmp/fallback")
	if err != nil {
		t.Fatalf("EnsureProjectContainer with CLI fallback failed: %v", err)
	}
	if projID != "proj_cli_fallback_1" {
		t.Errorf("expected proj_cli_fallback_1, got %s", projID)
	}
	if !strings.Contains(executedCmd, "project add /tmp/fallback --title FallbackProject --json") {
		t.Errorf("unexpected cli execution: %s", executedCmd)
	}
}

func TestT3Backend_SendMessage_PendingRequest_BothShapes(t *testing.T) {
	var mu sync.Mutex
	var respondedAnswers map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mcp" {
			var req McpRequest
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
				toolName, _ := req.Params["name"].(string)
				args, _ := req.Params["arguments"].(map[string]any)

				if toolName == "t3_pending_request" {
					action, _ := args["action"].(string)
					if action == "list" {
						_ = json.NewEncoder(w).Encode(map[string]any{
							"jsonrpc": "2.0",
							"id":      req.ID,
							"result": map[string]any{
								"content": []map[string]any{
									{"type": "text", "text": `{"requestIds": ["req_q2"]}`},
								},
							},
						})
						return
					}
					if action == "read" {
						_ = json.NewEncoder(w).Encode(map[string]any{
							"jsonrpc": "2.0",
							"id":      req.ID,
							"result": map[string]any{
								"content": []map[string]any{
									{
										"type": "text",
										"text": `{"requestId": "req_q2", "questions": [
											{"id": "q_env", "header": "Environment", "question": "Target env?"},
											{"id": "q_migrate", "header": "Migrate", "question": "Run migrations?"}
										]}`,
									},
								},
							},
						})
						return
					}
					if action == "respond" {
						mu.Lock()
						respondedAnswers, _ = args["answers"].(map[string]any)
						mu.Unlock()

						_ = json.NewEncoder(w).Encode(map[string]any{
							"jsonrpc": "2.0",
							"id":      req.ID,
							"result": map[string]any{
								"content": []map[string]any{
									{"type": "text", "text": `{"sequence": 1}`},
								},
							},
						})
						return
					}
				}
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mcpClient := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)

	b := NewT3Backend(
		WithServerURL(server.URL),
		WithMCPClient(mcpClient),
		WithTokenProvider(NewSessionTokenProvider(WithStaticSessionToken("token"))),
	)
	ctx := context.Background()
	handle := backend.TaskHandle{ThreadID: "th_pending_test"}

	// 1. Attempting single text string on multi-question prompt fails clearly
	err := b.SendMessage(ctx, handle, backend.TextMessage("production"))
	if err == nil || !strings.Contains(err.Error(), "repeatable answers required") {
		t.Fatalf("expected error requiring repeatable answers, got: %v", err)
	}

	// 2. Submitting ordered answers maps structurally without bare string concat (Decision 2)
	err = b.SendMessage(ctx, handle, backend.OrderedAnswers("staging", "yes"))
	if err != nil {
		t.Fatalf("SendMessage with OrderedAnswers failed: %v", err)
	}

	mu.Lock()
	if respondedAnswers["q_env"] != "staging" || respondedAnswers["q_migrate"] != "yes" {
		t.Errorf("unexpected mapped answers: %+v", respondedAnswers)
	}
	mu.Unlock()

	// 3. Submitting keyed answers directly
	err = b.SendMessage(ctx, handle, backend.KeyedAnswers(map[string]any{
		"q_env":     "production",
		"q_migrate": "no",
	}))
	if err != nil {
		t.Fatalf("SendMessage with KeyedAnswers failed: %v", err)
	}

	mu.Lock()
	if respondedAnswers["q_env"] != "production" || respondedAnswers["q_migrate"] != "no" {
		t.Errorf("unexpected mapped answers: %+v", respondedAnswers)
	}
	mu.Unlock()
}

func TestT3Backend_InterruptAndClose(t *testing.T) {
	var mu sync.Mutex
	var interrupted bool
	var archived bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/mcp" {
			var req McpRequest
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
				toolName, _ := req.Params["name"].(string)

				if toolName == "t3_thread_interrupt" {
					mu.Lock()
					interrupted = true
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": `{"interrupted": true}`}},
						},
					})
					return
				}

				if toolName == "t3_thread_organize" {
					mu.Lock()
					archived = true
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": `{"sequence": 5}`}},
						},
					})
					return
				}
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	mcpClient := NewMCPClient(
		WithMCPBaseURL(server.URL),
		WithMCPTokenProvider(&StaticMCPTokenProvider{Token: "token"}),
	)

	b := NewT3Backend(
		WithServerURL(server.URL),
		WithMCPClient(mcpClient),
		WithTokenProvider(NewSessionTokenProvider(WithStaticSessionToken("token"))),
	)
	ctx := context.Background()
	handle := backend.TaskHandle{ThreadID: "th_test_close"}

	// 1. InterruptTask
	if err := b.InterruptTask(ctx, handle); err != nil {
		t.Fatalf("InterruptTask failed: %v", err)
	}

	mu.Lock()
	if !interrupted {
		t.Errorf("expected interrupted to be true")
	}
	mu.Unlock()

	// 2. CloseTask
	if err := b.CloseTask(ctx, handle); err != nil {
		t.Fatalf("CloseTask failed: %v", err)
	}

	mu.Lock()
	if !archived {
		t.Errorf("expected archived to be true")
	}
	mu.Unlock()
}

func TestT3Backend_LiveServer(t *testing.T) {
	if os.Getenv("BUDDY_TEST_LIVE_T3") == "" {
		t.Skip("skipping live T3 server test; set BUDDY_TEST_LIVE_T3=1 to enable")
	}

	b := NewT3Backend()
	ctx := context.Background()

	projects, err := b.ListLiveProjects(ctx)
	if err != nil {
		t.Fatalf("live ListLiveProjects failed: %v", err)
	}
	t.Logf("Successfully connected to live T3 Code server! Found %d projects:", len(projects))
	for _, p := range projects {
		t.Logf("  - %s (ID: %s, Path: %s)", p.Name, p.ID, p.Path)
	}
}
