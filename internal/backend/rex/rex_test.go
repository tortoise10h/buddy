package rex

import (
	"context"
	"strings"
	"testing"
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
