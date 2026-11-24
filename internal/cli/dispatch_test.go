package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type mockTerminalBackend struct {
	dispatched    bool
	lastAgentCmd  []string
	lastSentText  string
	staticCapture string
	windows       []backend.WindowInfo
	capturesByBlk map[string]string
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
	return m.windows, nil
}
func (m *mockTerminalBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	return backend.WindowInfo{}, nil
}
func (m *mockTerminalBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	m.dispatched = true
	m.lastAgentCmd = append([]string(nil), agentCmd...)
	return "win_mock_1", "blk_mock_1", nil
}
func (m *mockTerminalBackend) Close(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockTerminalBackend) Focus(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockTerminalBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	if m.capturesByBlk != nil {
		if out, ok := m.capturesByBlk[blockID]; ok {
			return out, nil
		}
	}
	if m.staticCapture != "" {
		return m.staticCapture, nil
	}
	if m.lastSentText != "" {
		return "> " + m.lastSentText + "\n⠋ Working... ? for shortcuts", nil
	}
	return "agent ready ? for shortcuts", nil
}
func (m *mockTerminalBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	if input.Text != "" {
		m.lastSentText = input.Text
	}
	return nil
}
func (m *mockTerminalBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", nil
}
func (m *mockTerminalBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	return true, nil
}

func TestDispatch_EndToEnd(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Create a dummy project in tmpDir
	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	// Create default config pointing to tmpDir
	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	cfg.Engines["t3"] = config.EngineConfig{
		Provider: "antigravity",
		Model:    "gemini-3.8-flash-medium",
	}
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

	// Verify state file was written and contains the task
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

type mockCLIT3Runner struct {
	projectAdded []string
	started      []t3.ThreadStartOptions
	sent         []struct{ threadID, message string }
}

func (m *mockCLIT3Runner) ProjectAdd(_ context.Context, projectPath, _ string) error {
	m.projectAdded = append(m.projectAdded, projectPath)
	return nil
}

func (m *mockCLIT3Runner) ThreadStart(_ context.Context, opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	m.started = append(m.started, opts)
	return &t3.ThreadStartOutput{
		ThreadID:  "mcp:t3-thread-1",
		ProjectID: "t3-proj-1",
		RunID:     "run-1",
	}, nil
}

func (m *mockCLIT3Runner) ThreadShow(_ context.Context, threadID string) (*t3.ThreadShowOutput, error) {
	return &t3.ThreadShowOutput{
		Thread: t3.ThreadInfo{
			ThreadID: threadID,
			Status:   "completed",
			Model:    "gemini-3.8-flash-medium",
		},
		Messages: []t3.MessageInfo{
			{Role: "user", Text: "test prompt"},
			{Role: "assistant", Text: "Done!"},
		},
	}, nil
}

func (m *mockCLIT3Runner) ThreadSend(_ context.Context, threadID, message string) error {
	m.sent = append(m.sent, struct{ threadID, message string }{threadID, message})
	return nil
}

func (m *mockCLIT3Runner) ThreadList(_ context.Context, _ string) (*t3.ThreadListOutput, error) {
	return &t3.ThreadListOutput{}, nil
}

func TestDispatchAndSend_T3Routing_UsesT3Engine(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)
	t.Setenv("T3CODE_THREAD_ID", "th_test123")
	t.Setenv("REX_SESSION", "")
	t.Setenv("TERM_PROGRAM", "")

	// Create dummy project in tmpDir
	projDir := filepath.Join(tmpDir, "buddy")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	cfg.Engines["t3"] = config.EngineConfig{
		Provider: "antigravity",
		Model:    "gemini-3.8-flash-medium",
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	// Register mock backend for Rex to verify it is NOT touched, and mock T3Runner for t3
	mockBackend := &mockTerminalBackend{}
	mockEngine := engine.NewTerminalEngine(mockBackend)
	mockT3 := &mockCLIT3Runner{}
	reg := getRegistry()
	origT3, _ := reg.Get("t3")
	reg.Register("rex", mockEngine)
	reg.Register("t3", engine.NewT3EngineWithRunner(mockT3))
	t.Cleanup(func() {
		if origT3 != nil {
			reg.Register("t3", origT3)
		}
	})

	cmd := newDispatchCmd()
	cmd.SetArgs([]string{
		"-p", "buddy",
		"-t", "feat-t3",
		"-m", "test prompt",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected dispatch to succeed under T3 environment, got: %v", err)
	}

	if mockBackend.dispatched {
		t.Errorf("rex backend should NOT have been touched during T3 dispatch")
	}
	if len(mockT3.projectAdded) != 1 || mockT3.projectAdded[0] != projDir {
		t.Errorf("expected ProjectAdd(%q), got %+v", projDir, mockT3.projectAdded)
	}
	if len(mockT3.started) != 1 || mockT3.started[0].Prompt != "test prompt" {
		t.Errorf("expected ThreadStart with prompt, got %+v", mockT3.started)
	}

	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	proj, err := st.FindProject("buddy")
	if err != nil {
		t.Fatalf("expected buddy project in state: %v", err)
	}
	taskState, err := proj.FindTask("feat-t3")
	if err != nil {
		t.Fatalf("expected feat-t3 task in state: %v", err)
	}
	if taskState.GetEngine() != "t3" || taskState.AgentBlockID != "mcp:t3-thread-1" {
		t.Errorf("unexpected task state: %+v", taskState)
	}

	sendCmd := newSendCmd()
	sendCmd.SetArgs([]string{
		"-p", "buddy",
		"-t", "feat-t3",
		"--text", "follow-up answer",
	})
	if err := sendCmd.Execute(); err != nil {
		t.Fatalf("expected send to succeed under T3 environment, got: %v", err)
	}
	if len(mockT3.sent) != 1 || mockT3.sent[0].threadID != "mcp:t3-thread-1" || mockT3.sent[0].message != "follow-up answer" {
		t.Errorf("expected ThreadSend to mcp:t3-thread-1, got %+v", mockT3.sent)
	}

	captureCmd := newCaptureCmd()
	captureCmd.SetArgs([]string{
		"-p", "buddy",
		"-t", "feat-t3",
	})
	if err := captureCmd.Execute(); err != nil {
		t.Fatalf("expected capture to succeed under T3 environment, got: %v", err)
	}
}

func TestDispatch_T3_MissingModelFailsFast(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)
	t.Setenv("BUDDY_T3_MODEL", "")
	t.Setenv("T3CODE_THREAD_ID", "th_test123")
	t.Setenv("REX_SESSION", "")
	t.Setenv("TERM_PROGRAM", "")

	projDir := filepath.Join(tmpDir, "buddy")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	mockT3 := &mockCLIT3Runner{}
	reg := getRegistry()
	origT3, _ := reg.Get("t3")
	reg.Register("t3", engine.NewT3EngineWithRunner(mockT3))
	t.Cleanup(func() {
		if origT3 != nil {
			reg.Register("t3", origT3)
		}
	})

	cmd := newDispatchCmd()
	cmd.SetArgs([]string{
		"-p", "buddy",
		"-t", "feat-t3-missing",
		"-m", "test prompt",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when T3 provider/model is not configured")
	}
	if len(mockT3.started) != 0 {
		t.Errorf("expected fail-fast before ThreadStart, got %d calls", len(mockT3.started))
	}
}

func TestDispatch_CandidateDuplicateGuard_AndForceOverride(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	mockBackend := &mockTerminalBackend{}
	getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend, engine.WithReadinessTimeout(300*time.Millisecond)))

	// Seed an existing active task in test-proj
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("test-proj", projDir, "sess_mock_1", "test-proj", state.TaskState{
			TaskSlug:     "auth-flow",
			WindowID:     "win_existing",
			AgentBlockID: "blk_existing",
			Status:       "waiting_for_input",
			LastSummary:  "Which OAuth provider should we use?",
		})
		return nil
	})

	// 1. Exact slug match without --force must fail fast and surface candidate status + summary
	cmd1 := newDispatchCmd()
	cmd1.SetArgs([]string{
		"-p", "test-proj",
		"-t", "auth-flow",
		"-m", "implement Google OAuth",
	})
	err := cmd1.Execute()
	if err == nil {
		t.Fatalf("expected dispatch to fail when candidate task with same slug is already active")
	}
	if !strings.Contains(err.Error(), "auth-flow") || !strings.Contains(err.Error(), "waiting_for_input") || !strings.Contains(err.Error(), "Which OAuth provider") {
		t.Errorf("expected error to include candidate slug, status, and summary, got: %v", err)
	}

	// 2. Domain-token overlap ("auth-login") without --force must also surface candidate and block
	cmd2 := newDispatchCmd()
	cmd2.SetArgs([]string{
		"-p", "test-proj",
		"-t", "auth-login",
		"-m", "add login button",
	})
	err2 := cmd2.Execute()
	if err2 == nil {
		t.Fatalf("expected dispatch to fail when candidate task shares domain topic token 'auth'")
	}
	if !strings.Contains(err2.Error(), "--force") {
		t.Errorf("expected error to mention --force or buddy-ctl send, got: %v", err2)
	}

	// 3. Passing --force overrides the candidate guard and dispatches cleanly
	cmd3 := newDispatchCmd()
	cmd3.SetArgs([]string{
		"-p", "test-proj",
		"-t", "auth-login",
		"-m", "add login button",
		"--force",
	})
	if err := cmd3.Execute(); err != nil {
		t.Fatalf("expected dispatch with --force to succeed, got: %v", err)
	}
}

func TestDispatch_PickupFalse_WhenOnboardingSwallowsPrompt(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	mockBackend := &mockTerminalBackend{
		staticCapture: "Auto mode: edits & safe commands auto-approved\nesc to dismiss\n? for shortcuts",
	}
	getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend, engine.WithReadinessTimeout(300*time.Millisecond)))

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"dispatch",
		"--project", "test-proj",
		"--task", "swallowed-task",
		"--prompt", "build new payment module",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch command failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json output: %v\nraw: %s", err, buf.String())
	}

	if resp["picked_up"] != false {
		t.Errorf("expected picked_up=false when onboarding splash swallowed prompt, got %v", resp["picked_up"])
	}
	reason, _ := resp["pickup_reason"].(string)
	if !strings.Contains(strings.ToLower(reason), "onboarding") {
		t.Errorf("expected pickup_reason to mention onboarding splash, got %q", reason)
	}
}

func TestDispatchAndSend_SituationalRecommendedWatch(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projA := filepath.Join(tmpDir, "proj-a")
	projB := filepath.Join(tmpDir, "proj-b")
	_ = os.MkdirAll(projA, 0755)
	_ = os.MkdirAll(projB, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	_ = config.SaveConfig(cfg)

	mockBackend := &mockTerminalBackend{}
	getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend, engine.WithReadinessTimeout(200*time.Millisecond)))

	// 1st task in proj-a -> recommended_watch should be single-task scope
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"dispatch", "-p", "proj-a", "-t", "task-one", "-m", "work one"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch 1 failed: %v", err)
	}
	var resp1 map[string]any
	_ = json.Unmarshal(buf.Bytes(), &resp1)
	if !strings.HasPrefix(fmt.Sprint(resp1["recommended_watch"]), "buddy-ctl watch --project proj-a --task task-one") {
		t.Errorf("got recommended_watch = %q, want single-task scope", resp1["recommended_watch"])
	}

	// 2nd distinct task in proj-a -> recommended_watch should be project scope
	buf.Reset()
	rootCmd.SetArgs([]string{"dispatch", "-p", "proj-a", "-t", "billing-ui", "-m", "work two"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch 2 failed: %v", err)
	}
	var resp2 map[string]any
	_ = json.Unmarshal(buf.Bytes(), &resp2)
	if !strings.HasPrefix(fmt.Sprint(resp2["recommended_watch"]), "buddy-ctl watch --project proj-a --interval") {
		t.Errorf("got recommended_watch = %q, want project scope 'buddy-ctl watch --project proj-a ...'", resp2["recommended_watch"])
	}

	// 3rd task in proj-b -> recommended_watch should be --all
	buf.Reset()
	rootCmd.SetArgs([]string{"dispatch", "-p", "proj-b", "-t", "api-v2", "-m", "work three"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dispatch 3 failed: %v", err)
	}
	var resp3 map[string]any
	_ = json.Unmarshal(buf.Bytes(), &resp3)
	if !strings.HasPrefix(fmt.Sprint(resp3["recommended_watch"]), "buddy-ctl watch --all") {
		t.Errorf("got recommended_watch = %q, want 'buddy-ctl watch --all ...'", resp3["recommended_watch"])
	}

	// Test buddy-ctl send transitions waiting_for_input -> running and returns recommended_watch
	_ = state.MutateState(func(s *state.State) error {
		return s.UpdateTask("proj-a", "task-one", "waiting_for_input", "Need confirmation")
	})
	buf.Reset()
	rootCmd.SetArgs([]string{"send", "-p", "proj-a", "-t", "task-one", "--text", "yes proceed"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	var sendResp map[string]any
	_ = json.Unmarshal(buf.Bytes(), &sendResp)
	if !strings.HasPrefix(fmt.Sprint(sendResp["recommended_watch"]), "buddy-ctl watch --all") {
		t.Errorf("send recommended_watch = %q, want 'buddy-ctl watch --all ...'", sendResp["recommended_watch"])
	}
	st, _ := state.LoadState()
	pa, _ := st.FindProject("proj-a")
	tOne, _ := pa.FindTask("task-one")
	if tOne.Status != "running" {
		t.Errorf("expected task status to transition back to running after send, got %q", tOne.Status)
	}
}

func TestTaskAdopt_RegistersUntrackedWindow(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	mockBackend := &mockTerminalBackend{
		windows: []backend.WindowInfo{
			{
				WindowID:  "win_manual_42",
				Label:     "manual-refactor",
				SessionID: "sess_mock_1",
				BlockIDs:  []string{"blk_manual_42"},
			},
		},
		capturesByBlk: map[string]string{
			"blk_manual_42": "Completed manual refactor of router.\n> \n? for shortcuts",
		},
	}
	origRex := defaultRexBackend
	defaultRexBackend = func() backend.TerminalBackend { return mockBackend }
	defer func() { defaultRexBackend = origRex }()

	// Seed project with an untracked window
	_ = state.MutateState(func(s *state.State) error {
		s.Projects["test-proj"] = state.ProjectState{
			Name:             "test-proj",
			ProjectPath:      projDir,
			RexSessionID:     "sess_mock_1",
			RexSessionLabel:  "test-proj",
			Tasks:            map[string]state.TaskState{},
			UntrackedWindows: []state.UntrackedWindow{{WindowID: "win_manual_42", Label: "manual-refactor", BlockID: "blk_manual_42"}},
		}
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"task", "adopt",
		"--project", "test-proj",
		"--window", "manual-refactor",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("task adopt failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json from task adopt: %v\nraw: %s", err, buf.String())
	}
	if resp["origin"] != "user" {
		t.Errorf("expected adopted task origin='user', got %v", resp["origin"])
	}
	if resp["status"] != "adopted" || resp["task_status"] != "ready_for_demo" {
		t.Errorf("expected status='adopted' and task_status='ready_for_demo', got %+v", resp)
	}

	st, _ := state.LoadState()
	p, _ := st.FindProject("test-proj")
	adopted, err := p.FindTask("manual-refactor")
	if err != nil {
		t.Fatalf("expected adopted task in state: %v", err)
	}
	if adopted.Origin != "user" || adopted.WindowID != "win_manual_42" || adopted.AgentBlockID != "blk_manual_42" {
		t.Errorf("unexpected adopted task fields: %+v", adopted)
	}
	if len(p.UntrackedWindows) != 0 {
		t.Errorf("expected adopted window to be removed from UntrackedWindows, got %+v", p.UntrackedWindows)
	}
}

func TestDispatchAndTaskStart_CustomModelSelection(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)
	t.Setenv("BUDDY_MODEL", "")

	projDir := filepath.Join(tmpDir, "test-proj")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	cfg.Engines["t3"] = config.EngineConfig{
		Provider: "antigravity",
		Model:    "gemini-3.8-flash-medium",
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	mockBackend := &mockTerminalBackend{}
	mockT3 := &mockCLIT3Runner{}
	reg := getRegistry()
	origT3, _ := reg.Get("t3")
	reg.Register("rex", engine.NewTerminalEngine(mockBackend, engine.WithReadinessTimeout(200*time.Millisecond)))
	reg.Register("t3", engine.NewT3EngineWithRunner(mockT3))
	t.Cleanup(func() {
		if origT3 != nil {
			reg.Register("t3", origT3)
		}
	})

	// 1. Rex dispatch without --model preserves default command ["agy"] and omits model in state/JSON.
	buf := new(bytes.Buffer)
	cmd1 := newDispatchCmd()
	cmd1.SetOut(buf)
	cmd1.SetErr(buf)
	cmd1.SetArgs([]string{"-p", "test-proj", "-t", "default-task", "-m", "routine fix"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("default dispatch failed: %v", err)
	}
	if len(mockBackend.lastAgentCmd) != 1 || mockBackend.lastAgentCmd[0] != "agy" {
		t.Fatalf("expected untouched default [agy], got %v", mockBackend.lastAgentCmd)
	}
	var resp1 map[string]any
	_ = json.Unmarshal(buf.Bytes(), &resp1)
	if _, hasModel := resp1["model"]; hasModel {
		t.Fatalf("expected model to be omitted from JSON when default, got %v", resp1["model"])
	}

	// 2. Rex dispatch with --model injects --model flag and persists TaskState.Model.
	buf.Reset()
	cmd2 := newDispatchCmd()
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"-p", "test-proj", "-t", "complex-refactor", "-m", "refactor state lock", "--model", "gemini-4-neon-high"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("dispatch --model failed: %v", err)
	}
	if len(mockBackend.lastAgentCmd) != 3 || mockBackend.lastAgentCmd[0] != "agy" || mockBackend.lastAgentCmd[1] != "--model" || mockBackend.lastAgentCmd[2] != "gemini-4-neon-high" {
		t.Fatalf("expected [agy --model gemini-4-neon-high], got %v", mockBackend.lastAgentCmd)
	}
	var resp2 map[string]any
	_ = json.Unmarshal(buf.Bytes(), &resp2)
	if resp2["model"] != "gemini-4-neon-high" {
		t.Fatalf("expected model in JSON response, got %v", resp2["model"])
	}

	st, _ := state.LoadState()
	p, _ := st.FindProject("test-proj")
	task2, _ := p.FindTask("complex-refactor")
	if task2.Model != "gemini-4-neon-high" {
		t.Fatalf("expected TaskState.Model='gemini-4-neon-high', got %q", task2.Model)
	}
	histRec := state.NewHistoryRecord(*p, *task2, "verified", false)
	if histRec.Model != "gemini-4-neon-high" {
		t.Fatalf("expected HistoryRecord.Model='gemini-4-neon-high', got %q", histRec.Model)
	}

	// 3. task start --model works on rex with opencode ModelCommand.
	buf.Reset()
	cmd3 := newTaskStartCmd()
	cmd3.SetOut(buf)
	cmd3.SetErr(buf)
	cmd3.SetArgs([]string{"-p", "test-proj", "-t", "oc-tui-task", "--agent", "opencode", "--model", "openai/gpt-5.6-cyber", "--prompt", "build parser"})
	if err := cmd3.Execute(); err != nil {
		t.Fatalf("task start --model failed: %v", err)
	}
	if len(mockBackend.lastAgentCmd) != 4 || mockBackend.lastAgentCmd[0] != "opencode" || mockBackend.lastAgentCmd[1] != "mini" || mockBackend.lastAgentCmd[3] != "openai/gpt-5.6-cyber" {
		t.Fatalf("expected opencode mini --model command, got %v", mockBackend.lastAgentCmd)
	}

	// 4. T3 dispatch with bare --model combines with engines.t3.provider.
	buf.Reset()
	cmdT3 := newDispatchCmd()
	cmdT3.SetOut(buf)
	cmdT3.SetErr(buf)
	cmdT3.SetArgs([]string{"-p", "test-proj", "-t", "t3-bare", "-m", "deep fix", "--engine", "t3", "--model", "gemini-4-neon-high"})
	if err := cmdT3.Execute(); err != nil {
		t.Fatalf("t3 dispatch bare --model failed: %v", err)
	}
	if len(mockT3.started) != 1 || mockT3.started[0].Provider != "antigravity" || mockT3.started[0].Model != "gemini-4-neon-high" {
		t.Fatalf("expected antigravity/gemini-4-neon-high, got %+v", mockT3.started)
	}

	// 5. T3 conflicting --agent and --model returns an explicit error.
	cmdConflict := newDispatchCmd()
	cmdConflict.SetArgs([]string{"-p", "test-proj", "-t", "t3-conflict", "-m", "deep fix", "--engine", "t3", "--agent", "antigravity/model-a", "--model", "antigravity/model-b"})
	if err := cmdConflict.Execute(); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("expected conflicting --agent/--model error, got: %v", err)
	}
}

func TestDispatch_OpenCodeUnsubmittedPaste_ReportsPickupFalseAndWaitingForInput(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "board-resolution")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	cfg.SetTerminalAgents("agy", "opencode")
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save test config: %v", err)
	}

	openCodeSplash := strings.Join([]string{
		"█▀▀█ █▀▀█ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█ █▀▀█ █▀▀█",
		"█  █ █  █ █▀▀▀ █  █ █    █  █ █  █ █▀▀▀",
		"▀▀▀▀ █▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀",
		"/update to install v2.4.1",
		"┃",
		"┃  [Pasted ~19 lines]",
		"┃",
		"┃  Build · MiMo-V2.6-Flash OpenCode Go",
		"╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀",
		"…/board-resolution  shift+tab agents  ctrl+p commands",
		"⊙ 2 MCP /mcps                                   2.3.2",
	}, "\n")

	mockBackend := &mockTerminalBackend{
		staticCapture: openCodeSplash,
	}
	getRegistry().Register("rex", engine.NewTerminalEngine(
		mockBackend,
		engine.WithReadinessTimeout(200*time.Millisecond),
		engine.WithPickupVerifyTimeout(80*time.Millisecond, 15*time.Millisecond),
	))

	buf := new(bytes.Buffer)
	cmd := newDispatchCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--project", "board-resolution",
		"--task", "owned-note-reconcile",
		"--prompt", "Reconcile localStorage owned note IDs with the server\nStep 1\nStep 2",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dispatch command failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json output: %v\nraw: %s", err, buf.String())
	}

	if resp["picked_up"] != false {
		t.Errorf("expected picked_up=false when OpenCode shows unsubmitted [Pasted ~19 lines], got %v", resp["picked_up"])
	}
	reason, _ := resp["pickup_reason"].(string)
	if !strings.Contains(reason, "[Pasted ~19 lines]") {
		t.Errorf("expected pickup_reason to mention [Pasted ~19 lines], got %q", reason)
	}
	if recWatch, _ := resp["recommended_watch"].(string); recWatch != "" {
		t.Errorf("expected empty recommended_watch when picked_up=false, got %q", recWatch)
	}

	st, err := state.LoadStateForEngine("rex")
	if err != nil {
		t.Fatalf("LoadStateForEngine failed: %v", err)
	}
	proj, err := st.FindProject("board-resolution")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	task, err := proj.FindTask("owned-note-reconcile")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.Status != "waiting_for_input" {
		t.Errorf("persisted task status = %q, want waiting_for_input", task.Status)
	}
	if !strings.Contains(task.LastSummary, "[Pasted ~19 lines]") {
		t.Errorf("persisted task LastSummary = %q, want containing [Pasted ~19 lines]", task.LastSummary)
	}
}
