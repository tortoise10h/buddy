package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type mockTerminalBackend struct {
	dispatched bool
}

func (m *mockTerminalBackend) Name() string { return "rex" }
func (m *mockTerminalBackend) IsAvailable(ctx context.Context) bool { return true }
func (m *mockTerminalBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	return backend.SessionInfo{SessionID: "sess_mock_1", Name: name, Path: cwd}, nil
}
func (m *mockTerminalBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	return []backend.SessionInfo{{SessionID: "sess_mock_1", Name: "test-proj"}}, nil
}
func (m *mockTerminalBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	return nil, nil
}
func (m *mockTerminalBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	return backend.WindowInfo{}, nil
}
func (m *mockTerminalBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	m.dispatched = true
	return "win_mock_1", "blk_mock_1", nil
}
func (m *mockTerminalBackend) Close(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockTerminalBackend) Focus(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockTerminalBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	return "agent ready ? for shortcuts", nil
}
func (m *mockTerminalBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	return nil
}
func (m *mockTerminalBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", nil
}
func (m *mockTerminalBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	return true, nil
}

func TestDispatch_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Create a dummy project in tmpDir
	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	// Create default config pointing to tmpDir
	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	// Register mock backend in defaultRegistry
	mockBackend := &mockTerminalBackend{}
	mockEngine := engine.NewTerminalEngine(mockBackend)
	reg := getRegistry()
	reg.Register("rex", mockEngine)

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"dispatch",
		"--project", "test-proj",
		"--task", "auth-flow",
		"--prompt", "implement OAuth login flow",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch execution failed: %v", err)
	}

	if !mockBackend.dispatched {
		t.Errorf("expected CreateTaskWindow to be invoked on backend")
	}

	// Verify state.json was written and contains the task
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state after dispatch: %v", err)
	}

	p, err := st.FindProject("test-proj")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	task, err := p.FindTask("auth-flow")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.WindowID != "win_mock_1" || task.AgentBlockID != "blk_mock_1" {
		t.Errorf("unexpected task recorded in state: %+v", task)
	}
	if task.Status != "running" {
		t.Errorf("expected status running, got %s", task.Status)
	}
}
