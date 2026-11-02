package rex

import (
	"context"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

func TestRexBackend_Name(t *testing.T) {
	b := NewRexBackend()
	if b.Name() != "rex" {
		t.Fatalf("expected rex, got %s", b.Name())
	}
}

func TestRexBackend_ListSessions(t *testing.T) {
	mockJSON := `{"sessions":[{"session_id":"s_123","label":"my-project","cwd":"/path/to/project"}]}`
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		if len(args) == 2 && args[0] == "ls" && args[1] == "--json" {
			return mockJSON, nil
		}
		t.Fatalf("unexpected call: %v", args)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "s_123" || sessions[0].Name != "my-project" {
		t.Fatalf("unexpected session info: %+v", sessions[0])
	}
}

func TestRexBackend_CreateTaskWindow(t *testing.T) {
	windowNewJSON := `{"window_id":"w_456","block_ids":["b_789"],"label":"test-task"}`

	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.HasPrefix(cmdStr, "window new") {
			return windowNewJSON, nil
		}
		t.Fatalf("unexpected command: %s", cmdStr)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	winID, blockID, err := b.CreateTaskWindow(context.Background(), "s_123", "test-task", "/path/to/project", []string{"agy"})
	if err != nil {
		t.Fatalf("CreateTaskWindow failed: %v", err)
	}
	if winID != "w_456" {
		t.Errorf("expected window w_456, got %s", winID)
	}
	if blockID != "b_789" {
		t.Errorf("expected block b_789, got %s", blockID)
	}
}

func TestRexBackend_WorkerBackend(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(cmdStr, "ls --json"):
			return `{"sessions":[{"session_id":"s_1","label":"docnotate","cwd":"/path/to/docnotate"}]}`, nil
		case strings.HasPrefix(cmdStr, "window new"):
			return `{"window_id":"w_1","block_ids":["b_1"],"label":"task-1"}`, nil
		case strings.HasPrefix(cmdStr, "block inspect b_1"):
			return `{"block_id":"b_1","is_alive":true}`, nil
		case strings.HasPrefix(cmdStr, "capture"):
			return "Line 1\nLine 2\nLine 3\n", nil
		case strings.HasPrefix(cmdStr, "send"):
			return "", nil
		case strings.HasPrefix(cmdStr, "window close"):
			return "", nil
		}
		t.Fatalf("unexpected command: %s", cmdStr)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	ctx := context.Background()

	// Capabilities
	caps := b.Capabilities()
	if caps.ManagesWorktrees {
		t.Errorf("expected ManagesWorktrees to be false for Rex")
	}
	if !caps.SupportsFocus || !caps.SupportsDemoSplit || !caps.SupportsRawKeys {
		t.Errorf("expected SupportsFocus, SupportsDemoSplit, SupportsRawKeys to be true for Rex")
	}

	// EnsureProjectContainer
	sessID, err := b.EnsureProjectContainer(ctx, "docnotate", "/path/to/docnotate")
	if err != nil {
		t.Fatalf("EnsureProjectContainer failed: %v", err)
	}
	if sessID != "s_1" {
		t.Errorf("expected s_1, got %s", sessID)
	}

	// ListLiveProjects
	projects, err := b.ListLiveProjects(ctx)
	if err != nil {
		t.Fatalf("ListLiveProjects failed: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "docnotate" {
		t.Errorf("unexpected projects: %+v", projects)
	}

	// DispatchTask
	dispatchRes, err := b.DispatchTask(ctx, backend.DispatchSpec{
		ProjectName: "docnotate",
		ProjectPath: "/path/to/docnotate",
		TaskSlug:    "task-1",
		AgentCmd:    []string{"agy"},
	})
	if err != nil {
		t.Fatalf("DispatchTask failed: %v", err)
	}
	if dispatchRes.TaskHandle.WindowID != "w_1" || dispatchRes.TaskHandle.BlockID != "b_1" {
		t.Errorf("unexpected dispatch handle: %+v", dispatchRes.TaskHandle)
	}

	handle := dispatchRes.TaskHandle

	// IsTaskAlive
	alive, err := b.IsTaskAlive(ctx, handle)
	if err != nil || !alive {
		t.Errorf("expected alive=true, got alive=%v, err=%v", alive, err)
	}

	// SendMessage
	if err := b.SendMessage(ctx, handle, backend.TextMessage("test message")); err != nil {
		t.Errorf("SendMessage failed: %v", err)
	}

	// InterruptTask
	if err := b.InterruptTask(ctx, handle); err != nil {
		t.Errorf("InterruptTask failed: %v", err)
	}

	// CaptureOutput
	capText, err := b.CaptureOutput(ctx, handle, 2)
	if err != nil {
		t.Errorf("CaptureOutput failed: %v", err)
	}
	if !strings.Contains(capText, "Line 2") {
		t.Errorf("unexpected capture text: %s", capText)
	}

	// GetAgentStatus
	status, err := b.GetAgentStatus(ctx, handle)
	if err != nil {
		t.Errorf("GetAgentStatus failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected running, got %s", status.Status)
	}

	// CloseTask
	if err := b.CloseTask(ctx, handle); err != nil {
		t.Errorf("CloseTask failed: %v", err)
	}
}

func TestRexBackend_Live_ListSessions(t *testing.T) {
	b := NewRexBackend()
	if !b.IsAvailable(context.Background()) {
		t.Skip("rex is not available on this machine")
	}
	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("live ListSessions failed: %v", err)
	}
	if len(sessions) == 0 {
		t.Log("rex is available but has 0 active sessions")
		return
	}
	t.Logf("Successfully connected to live Rex! Found %d active sessions:", len(sessions))
	for _, s := range sessions {
		t.Logf("  - %s (ID: %s)", s.Name, s.SessionID)
	}
}
