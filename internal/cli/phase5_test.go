package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

type mockHostTerminalBackend struct {
	createdWindows []string
	closedWindows  []string
	focusedWindows []string
}

func (m *mockHostTerminalBackend) Name() string { return "rex" }
func (m *mockHostTerminalBackend) IsAvailable(ctx context.Context) bool { return true }
func (m *mockHostTerminalBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	return backend.SessionInfo{SessionID: "sess_host_1", Name: name, Path: cwd}, nil
}
func (m *mockHostTerminalBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	return []backend.SessionInfo{{SessionID: "sess_host_1", Name: "sample-proj"}}, nil
}
func (m *mockHostTerminalBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	return nil, nil
}
func (m *mockHostTerminalBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	for _, w := range m.createdWindows {
		if w == windowID {
			return backend.WindowInfo{WindowID: windowID}, nil
		}
	}
	return backend.WindowInfo{}, fmt.Errorf("window not found")
}
func (m *mockHostTerminalBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	winID := "host_win_" + taskSlug
	blkID := "host_blk_" + taskSlug
	m.createdWindows = append(m.createdWindows, winID)
	return winID, blkID, nil
}
func (m *mockHostTerminalBackend) Close(ctx context.Context, sessionID, windowID string) error {
	m.closedWindows = append(m.closedWindows, windowID)
	return nil
}
func (m *mockHostTerminalBackend) Focus(ctx context.Context, sessionID, windowID string) error {
	m.focusedWindows = append(m.focusedWindows, windowID)
	return nil
}
func (m *mockHostTerminalBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	return "", nil
}
func (m *mockHostTerminalBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	return nil
}
func (m *mockHostTerminalBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "split_blk_1", nil
}
func (m *mockHostTerminalBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	return true, nil
}

func setupPhase5Test(t *testing.T) (*mockT3Engine, string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	projDir := filepath.Join(tmpDir, "sample-proj")
	_ = os.MkdirAll(projDir, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	_ = config.SaveConfig(cfg)

	mockT3 := &mockT3Engine{}
	reg := getRegistry()
	reg.Register("t3code", mockT3)
	reg.Register("t3", mockT3)

	return mockT3, tmpDir
}

func TestCLI_Demo_T3NoCmd_Noop(t *testing.T) {
	_, _ = setupPhase5Test(t)

	wtPath := "/tmp/worktree-dir"
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("sample-proj", "/tmp/sample-proj", "sample-proj", "sample-proj", state.TaskState{
			TaskSlug:     "feat-t3",
			Engine:       "t3code",
			WindowID:     "thread_t3_001",
			AgentBlockID: "thread_t3_001",
			WorktreePath: &wtPath,
			Status:       "ready_for_demo",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"demo", "-p", "sample-proj", "-t", "feat-t3"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("demo command failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse json output: %v, raw: %s", err, buf.String())
	}

	if resp["status"] != "noop" {
		t.Errorf("expected status 'noop', got '%v'", resp["status"])
	}
	if resp["worker_backend"] != "t3code" {
		t.Errorf("expected worker_backend 't3code', got '%v'", resp["worker_backend"])
	}
	if resp["cwd"] != "/tmp/worktree-dir" {
		t.Errorf("expected cwd '/tmp/worktree-dir', got '%v'", resp["cwd"])
	}
	reason, _ := resp["reason"].(string)
	if !strings.Contains(reason, "focus is unsupported for t3code tasks") {
		t.Errorf("expected reason explaining focus unsupported, got: %s", reason)
	}
}

func TestCLI_Demo_T3WithCmd_CrossHostAndReaping(t *testing.T) {
	mockT3, _ := setupPhase5Test(t)

	wtPath := "/tmp/worktree-dir-cross"
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("sample-proj", "/tmp/sample-proj", "sample-proj", "sample-proj", state.TaskState{
			TaskSlug:     "feat-cross",
			Engine:       "t3code",
			WindowID:     "thread_t3_002",
			AgentBlockID: "thread_t3_002",
			WorktreePath: &wtPath,
			Status:       "ready_for_demo",
		})
		return nil
	})

	// 1. Run demo with --cmd "npm run dev"
	bufDemo := new(bytes.Buffer)
	rootCmd.SetOut(bufDemo)
	rootCmd.SetErr(bufDemo)
	rootCmd.SetArgs([]string{"demo", "-p", "sample-proj", "-t", "feat-cross", "--cmd", "npm run dev"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("demo with --cmd failed: %v", err)
	}

	var demoResp map[string]any
	if err := json.Unmarshal(bufDemo.Bytes(), &demoResp); err != nil {
		t.Fatalf("failed to decode demo response: %v, raw: %s", err, bufDemo.String())
	}

	if demoResp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%v'", demoResp["status"])
	}
	if demoResp["cwd"] != "/tmp/worktree-dir-cross" {
		t.Errorf("expected cwd '/tmp/worktree-dir-cross', got '%v'", demoResp["cwd"])
	}

	devHandleRaw, ok := demoResp["dev_host_handle"].(map[string]any)
	if !ok {
		t.Fatalf("expected dev_host_handle in response, got: %v", demoResp)
	}
	if devHandleRaw["backend"] != "rex" {
		t.Errorf("expected dev_host_handle backend 'rex', got: %v", devHandleRaw["backend"])
	}

	// Verify dev_host_handle was persisted in state.json
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	p, _ := st.FindProject("sample-proj")
	task, _ := p.FindTask("feat-cross")
	if task.DevHostHandle == nil {
		t.Fatalf("expected DevHostHandle in state, got nil")
	}
	if task.DevHostHandle.Backend != "rex" {
		t.Errorf("expected DevHostHandle backend rex, got %s", task.DevHostHandle.Backend)
	}
	devWinID := task.DevHostHandle.WindowID
	if devWinID == "" {
		t.Errorf("expected non-empty dev WindowID in DevHostHandle")
	}

	// 2. Reaping: Run task close on the task
	bufClose := new(bytes.Buffer)
	rootCmd.SetOut(bufClose)
	rootCmd.SetErr(bufClose)
	rootCmd.SetArgs([]string{"task", "close", "-p", "sample-proj", "-t", "feat-cross"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("task close failed: %v", err)
	}

	if mockT3.closedWindowID != "thread_t3_002" {
		t.Errorf("expected T3 thread 'thread_t3_002' to be closed, got '%s'", mockT3.closedWindowID)
	}

	// Verify state updated to closed and DevHostHandle cleared
	st2, _ := state.LoadState()
	p2, _ := st2.FindProject("sample-proj")
	task2, _ := p2.FindTask("feat-cross")
	if task2.Status != "closed" {
		t.Errorf("expected task status 'closed', got '%s'", task2.Status)
	}
	if task2.DevHostHandle != nil {
		t.Errorf("expected DevHostHandle to be reaped and cleared in state, got: %v", task2.DevHostHandle)
	}

	// 3. Idempotency test: Double-close must not error
	bufClose2 := new(bytes.Buffer)
	rootCmd.SetOut(bufClose2)
	rootCmd.SetErr(bufClose2)
	rootCmd.SetArgs([]string{"task", "close", "-p", "sample-proj", "-t", "feat-cross"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("second task close (double-close) failed: %v", err)
	}

	var close2Resp map[string]any
	if err := json.Unmarshal(bufClose2.Bytes(), &close2Resp); err != nil {
		t.Fatalf("failed to decode second close response: %v, raw: %s", err, bufClose2.String())
	}
	if close2Resp["status"] != "closed" {
		t.Errorf("expected double-close status 'closed', got '%v'", close2Resp["status"])
	}
}

func TestCLI_T3Snapshot_ZeroOAuth(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Mock T3 server responding to GET /api/orchestration/shell without OAuth
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/orchestration/shell" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schemaVersion":    2,
			"snapshotSequence": 105,
			"threads": []map[string]any{
				{
					"id":                 "thread_zero_oauth_1",
					"projectId":          "p-1",
					"title":              "feat/add-login",
					"providerInstanceId": "antigravity/gemini-4-argon-high",
					"status":             "running",
					"pendingRequestCount": 0,
				},
				{
					"id":                 "thread_zero_oauth_2",
					"projectId":          "p-1",
					"title":              "feat/auth-guard",
					"providerInstanceId": "antigravity/gemini-4-argon-high",
					"status":             "waiting",
					"pendingRequestCount": 1,
				},
			},
		})
	}))
	defer server.Close()

	t.Setenv("BUDDY_T3_SERVER_URL", server.URL)

	// 1. JSON format
	bufJSON := new(bytes.Buffer)
	rootCmd.SetOut(bufJSON)
	rootCmd.SetErr(bufJSON)
	rootCmd.SetArgs([]string{"t3", "snapshot", "--format", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("t3 snapshot failed: %v", err)
	}

	var snapshotResp map[string]any
	if err := json.Unmarshal(bufJSON.Bytes(), &snapshotResp); err != nil {
		t.Fatalf("failed to parse snapshot json: %v, raw: %s", err, bufJSON.String())
	}

	if snapshotResp["snapshotSequence"] != float64(105) {
		t.Errorf("expected snapshotSequence 105, got %v", snapshotResp["snapshotSequence"])
	}
	threads, ok := snapshotResp["threads"].([]any)
	if !ok || len(threads) != 2 {
		t.Fatalf("expected 2 threads in snapshot, got %v", snapshotResp["threads"])
	}

	// 2. Text / brief format
	bufText := new(bytes.Buffer)
	rootCmd.SetOut(bufText)
	rootCmd.SetErr(bufText)
	rootCmd.SetArgs([]string{"t3", "snapshot", "--format", "text"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("t3 snapshot text failed: %v", err)
	}

	textOut := bufText.String()
	if !strings.Contains(textOut, "T3 Code Orchestration Shell (Sequence 105, Threads: 2)") {
		t.Errorf("expected header in text output, got: %s", textOut)
	}
	if !strings.Contains(textOut, "thread_zero_oauth_1") || !strings.Contains(textOut, "thread_zero_oauth_2") {
		t.Errorf("expected thread IDs in text output, got: %s", textOut)
	}
}
