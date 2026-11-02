package t3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	// Non-existing project returns ErrNotImplementedPhase1
	_, err = backendImpl.EnsureProjectContainer(ctx, "New App", "/Users/test/Projects/new-app")
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for missing project creation, got: %v", err)
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

func TestT3Backend_Phase1DeferredMutations(t *testing.T) {
	b := NewT3Backend(WithServerURL("http://127.0.0.1:3773"))
	ctx := context.Background()
	handle := backend.TaskHandle{ThreadID: "th_123"}

	// DispatchTask
	_, err := b.DispatchTask(ctx, backend.DispatchSpec{TaskSlug: "my-task"})
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for DispatchTask, got: %v", err)
	}

	// CloseTask
	err = b.CloseTask(ctx, handle)
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for CloseTask, got: %v", err)
	}

	// SendMessage
	err = b.SendMessage(ctx, handle, "Hello")
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for SendMessage, got: %v", err)
	}

	// InterruptTask
	err = b.InterruptTask(ctx, handle)
	if !errors.Is(err, backend.ErrNotImplementedPhase1) {
		t.Errorf("expected ErrNotImplementedPhase1 for InterruptTask, got: %v", err)
	}
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
