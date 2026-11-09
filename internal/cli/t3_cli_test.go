package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type mockT3Engine struct {
	dispatchedReq   *engine.DispatchRequest
	capturedLines   int
	capturedBlockID string
	sentInput       *backend.Input
	closedWindowID  string
	focusedWindowID string
}

func (m *mockT3Engine) Name() string { return "t3code" }

func (m *mockT3Engine) Dispatch(ctx context.Context, req engine.DispatchRequest) (*engine.DispatchResult, error) {
	m.dispatchedReq = &req
	return &engine.DispatchResult{
		SessionID: req.ProjectName,
		WindowID:  "thread_mock_999",
		BlockID:   "thread_mock_999",
		PickedUp:  true,
		Extra: map[string]any{
			"backend":       "t3code",
			"thread_id":     "thread_mock_999",
			"branch":        "task-t3-branch",
			"worktree_path": "/tmp/mock-worktree",
		},
	}, nil
}

func (m *mockT3Engine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	m.capturedBlockID = blockID
	m.capturedLines = lines
	return "Agent: Ready for review. All tests passed.\n", nil
}

func (m *mockT3Engine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	m.sentInput = &input
	return nil
}

func (m *mockT3Engine) Close(ctx context.Context, sessionID, windowID string) error {
	m.closedWindowID = windowID
	return nil
}

func (m *mockT3Engine) Focus(ctx context.Context, sessionID, windowID string) error {
	m.focusedWindowID = windowID
	return nil
}

func (m *mockT3Engine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", fmt.Errorf("t3code does not support dev split")
}

func setupTestEnvironment(t *testing.T) (*mockT3Engine, string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "sample-project")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	mockEng := &mockT3Engine{}
	reg := getRegistry()
	reg.Register("t3code", mockEng)
	reg.Register("t3", mockEng)

	return mockEng, tmpDir
}

func TestCLI_Dispatch_T3WorkerBackend(t *testing.T) {
	mockEng, _ := setupTestEnvironment(t)

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"dispatch",
		"--project", "sample-project",
		"--task", "t3-feature",
		"--prompt", "implement oauth in t3 code",
		"--worker-backend", "t3code",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch execution failed: %v", err)
	}

	if mockEng.dispatchedReq == nil {
		t.Fatalf("expected T3 engine Dispatch to be invoked")
	}
	if mockEng.dispatchedReq.TaskSlug != "t3-feature" {
		t.Errorf("expected task slug 't3-feature', got '%s'", mockEng.dispatchedReq.TaskSlug)
	}

	// Verify state contains worktree and branch
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	p, err := st.FindProject("sample-project")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	task, err := p.FindTask("t3-feature")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.Engine != "t3code" {
		t.Errorf("expected task engine 't3code', got '%s'", task.Engine)
	}
	if task.WindowID != "thread_mock_999" {
		t.Errorf("expected window_id 'thread_mock_999', got '%s'", task.WindowID)
	}
	if task.Branch == nil || *task.Branch != "task-t3-branch" {
		t.Errorf("expected branch 'task-t3-branch', got %v", task.Branch)
	}
	if task.WorktreePath == nil || *task.WorktreePath != "/tmp/mock-worktree" {
		t.Errorf("expected worktree_path '/tmp/mock-worktree', got %v", task.WorktreePath)
	}
}

func TestCLI_Send_RepeatableAnswers(t *testing.T) {
	mockEng, _ := setupTestEnvironment(t)

	// Pre-populate task in state
	branch := "feature-branch"
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("sample-project", "/tmp/sample-project", "sample-project", "sample-project", state.TaskState{
			TaskSlug:     "q-task",
			Engine:       "t3code",
			WindowID:     "thread_q_123",
			AgentBlockID: "thread_q_123",
			Status:       "waiting_for_input",
			Branch:       &branch,
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"send",
		"--project", "sample-project",
		"--task", "q-task",
		"--answer", "proceed with option A",
		"--answer", "environment=staging",
		"--answer", "retry=true",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("send execution failed: %v", err)
	}

	if mockEng.sentInput == nil {
		t.Fatalf("expected T3 engine Send to be invoked")
	}

	// Verify ordered answer
	if len(mockEng.sentInput.Answers) != 1 || mockEng.sentInput.Answers[0] != "proceed with option A" {
		t.Errorf("expected 1 ordered answer 'proceed with option A', got %v", mockEng.sentInput.Answers)
	}

	// Verify keyed answers
	if len(mockEng.sentInput.KeyedAnswers) != 2 {
		t.Errorf("expected 2 keyed answers, got %v", mockEng.sentInput.KeyedAnswers)
	}
	if val, ok := mockEng.sentInput.KeyedAnswers["environment"]; !ok || val != "staging" {
		t.Errorf("expected keyed answer environment=staging, got %v", val)
	}
	if val, ok := mockEng.sentInput.KeyedAnswers["retry"]; !ok || val != "true" {
		t.Errorf("expected keyed answer retry=true, got %v", val)
	}
}

func TestCLI_Capture_T3(t *testing.T) {
	mockEng, _ := setupTestEnvironment(t)

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("sample-project", "/tmp/sample-project", "sample-project", "sample-project", state.TaskState{
			TaskSlug:     "cap-task",
			Engine:       "t3code",
			WindowID:     "thread_cap_456",
			AgentBlockID: "thread_cap_456",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"capture",
		"--project", "sample-project",
		"--task", "cap-task",
		"--lines", "50",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("capture execution failed: %v", err)
	}

	if mockEng.capturedBlockID != "thread_cap_456" {
		t.Errorf("expected captured thread ID 'thread_cap_456', got '%s'", mockEng.capturedBlockID)
	}
	if mockEng.capturedLines != 50 {
		t.Errorf("expected 50 lines captured, got %d", mockEng.capturedLines)
	}
}

func TestCLI_TaskClose_T3(t *testing.T) {
	mockEng, _ := setupTestEnvironment(t)

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("sample-project", "/tmp/sample-project", "sample-project", "sample-project", state.TaskState{
			TaskSlug:     "close-task",
			Engine:       "t3code",
			WindowID:     "thread_close_789",
			AgentBlockID: "thread_close_789",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"task", "close",
		"--project", "sample-project",
		"--task", "close-task",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("task close execution failed: %v", err)
	}

	if mockEng.closedWindowID != "thread_close_789" {
		t.Errorf("expected closed thread ID 'thread_close_789', got '%s'", mockEng.closedWindowID)
	}

	// Verify state updated to closed
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	p, err := st.FindProject("sample-project")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	task, err := p.FindTask("close-task")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.Status != "closed" {
		t.Errorf("expected task status 'closed', got '%s'", task.Status)
	}
}

func TestCLI_Batch_T3WorkerBackend(t *testing.T) {
	mockEng, _ := setupTestEnvironment(t)

	batchJSON := `[
		{"project": "sample-project", "task": "batch-1", "prompt": "test batch 1"},
		{"project": "sample-project", "task": "batch-2", "prompt": "test batch 2"}
	]`

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"batch",
		"--worker-backend", "t3code",
		"--json", batchJSON,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("batch execution failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse batch output: %v, raw: %s", err, buf.String())
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
	if resp["dispatched_count"] != float64(2) {
		t.Errorf("expected 2 dispatched tasks, got %v", resp["dispatched_count"])
	}
	if mockEng.dispatchedReq == nil {
		t.Fatalf("expected T3 engine to receive dispatch requests")
	}
}
