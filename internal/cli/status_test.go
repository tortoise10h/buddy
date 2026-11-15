package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

func TestStatusCmd_HostPayload(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Set simulated Rex environment
	t.Setenv("REX_SESSION", "session:01a18feb-18dd-795a-9fb9-4c959fd018f3")
	t.Setenv("TERM_PROGRAM", "rex")

	cfg := config.DefaultConfig()
	_ = config.SaveConfig(cfg)

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("test-proj", "/tmp/test", "sess_1", "test-proj", state.TaskState{
			TaskSlug: "feat-status",
			Engine:   "rex",
			Status:   "running",
		})
		return nil
	})

	// 1. Test JSON format
	cmdJSON := newStatusCmd()
	cmdJSON.SetArgs([]string{"--format", "json"})
	stdoutJSON, _, err := captureOutput(t, func() error {
		return cmdJSON.Execute()
	})
	if err != nil {
		t.Fatalf("status --format json failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdoutJSON), &payload); err != nil {
		t.Fatalf("failed to parse json: %v, output was: %q", err, stdoutJSON)
	}

	hostObj, ok := payload["host"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'host' object in status payload, got %v", payload["host"])
	}
	if hostObj["detected"] != "rex" {
		t.Errorf("expected host.detected = 'rex', got %v", hostObj["detected"])
	}
	if hostObj["engine"] != "rex" {
		t.Errorf("expected host.engine = 'rex', got %v", hostObj["engine"])
	}
	if hostObj["source"] != "host_detection" {
		t.Errorf("expected host.source = 'host_detection', got %v", hostObj["source"])
	}
	signals, ok := hostObj["signals"].(map[string]any)
	if !ok || signals["REX_SESSION"] != "session:01a18feb-18dd-795a-9fb9-4c959fd018f3" {
		t.Errorf("expected signals to contain REX_SESSION, got %v", hostObj["signals"])
	}

	// 2. Test brief format
	cmdBrief := newStatusCmd()
	cmdBrief.SetArgs([]string{"--format", "brief"})
	stdoutBrief, _, err := captureOutput(t, func() error {
		return cmdBrief.Execute()
	})
	if err != nil {
		t.Fatalf("status --format brief failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdoutBrief), "Host: rex | Engine: rex (source: host_detection)") {
		t.Errorf("expected brief format header line 'Host: rex | Engine: rex (source: host_detection)', got: %q", stdoutBrief)
	}

	// 3. Test table format
	cmdTable := newStatusCmd()
	cmdTable.SetArgs([]string{"--format", "table"})
	stdoutTable, _, err := captureOutput(t, func() error {
		return cmdTable.Execute()
	})
	if err != nil {
		t.Fatalf("status --format table failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdoutTable), "Host: rex | Engine: rex (source: host_detection)") {
		t.Errorf("expected table format header line 'Host: rex | Engine: rex (source: host_detection)', got: %q", stdoutTable)
	}
	if !strings.Contains(stdoutTable, "ENGINE") {
		t.Errorf("expected table header to contain 'ENGINE', got: %q", stdoutTable)
	}
}

func TestStatusCmd_EngineStateIsolation(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)
	t.Setenv("T3CODE_THREAD_ID", "th_123")

	cfg := config.DefaultConfig()
	_ = config.SaveConfig(cfg)

	_ = state.MutateStateForEngine("rex", func(s *state.State) error {
		s.RecordTask("rex-proj", "/tmp/rex", "session:1", "rex-proj", state.TaskState{
			TaskSlug: "rex-task",
			Engine:   "rex",
			Status:   "running",
		})
		return nil
	})
	_ = state.MutateStateForEngine("t3", func(s *state.State) error {
		s.RecordTask("t3-proj", "/tmp/t3", "proj-1", "t3-proj", state.TaskState{
			TaskSlug: "t3-task",
			Engine:   "t3",
			Status:   "running",
		})
		return nil
	})

	// Under T3 host, status should only show T3 state by default.
	cmdT3 := newStatusCmd()
	cmdT3.SetArgs([]string{"--format", "json"})
	outT3, _, err := captureOutput(t, func() error { return cmdT3.Execute() })
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if strings.Contains(outT3, "rex-proj") || !strings.Contains(outT3, "t3-proj") {
		t.Errorf("expected only t3-proj in t3 status, got: %s", outT3)
	}

	// --all-engines should show both.
	cmdAll := newStatusCmd()
	cmdAll.SetArgs([]string{"--format", "json", "--all-engines"})
	outAll, _, err := captureOutput(t, func() error { return cmdAll.Execute() })
	if err != nil {
		t.Fatalf("status --all-engines failed: %v", err)
	}
	if !strings.Contains(outAll, "rex-proj") || !strings.Contains(outAll, "t3-proj") {
		t.Errorf("expected both rex-proj and t3-proj with --all-engines, got: %s", outAll)
	}
}

type syncTestT3Runner struct {
	showByID    map[string]*t3.ThreadShowOutput
	showErrByID map[string]error
	listByPath  map[string]*t3.ThreadListOutput
}

func (r *syncTestT3Runner) ProjectAdd(_ context.Context, _, _ string) error { return nil }
func (r *syncTestT3Runner) ThreadStart(_ context.Context, _ t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	return &t3.ThreadStartOutput{}, nil
}
func (r *syncTestT3Runner) ThreadSend(_ context.Context, _, _ string) error { return nil }
func (r *syncTestT3Runner) ThreadShow(_ context.Context, threadID string) (*t3.ThreadShowOutput, error) {
	if err, ok := r.showErrByID[threadID]; ok {
		return nil, err
	}
	if out, ok := r.showByID[threadID]; ok {
		return out, nil
	}
	return nil, fmt.Errorf("thread not found: %s", threadID)
}
func (r *syncTestT3Runner) ThreadList(_ context.Context, projectPath string) (*t3.ThreadListOutput, error) {
	if out, ok := r.listByPath[projectPath]; ok {
		return out, nil
	}
	return &t3.ThreadListOutput{}, nil
}
func (r *syncTestT3Runner) ThreadSettle(_ context.Context, _ string) error { return nil }

func TestStatusCmd_Sync_T3_ReconcilesTasksAndDiscoversUntracked(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")

	cfg := config.DefaultConfig()
	_ = config.SaveConfig(cfg)

	runner := &syncTestT3Runner{
		showByID: map[string]*t3.ThreadShowOutput{
			"th_live": {
				Thread: t3.ThreadInfo{
					ThreadID: "th_live",
					Status:   "completed",
					Model:    "gemini-4-hydrogen-medium",
				},
				Messages: []t3.MessageInfo{
					{Role: "assistant", Text: "Implemented auth"},
				},
			},
		},
		showErrByID: map[string]error{
			"th_gone": fmt.Errorf("t3 command failed (exit 1): thread not found: th_gone"),
		},
		listByPath: map[string]*t3.ThreadListOutput{
			"/tmp/buddy": {
				Threads: []t3.ThreadSummary{
					{ThreadID: "th_live", ProjectID: "proj-t3-1", Title: "Tracked live", Status: "completed", Settled: false},
					{ThreadID: "th_settled", ProjectID: "proj-t3-1", Title: "Old settled", Status: "completed", Settled: true},
					{ThreadID: "th_untracked", ProjectID: "proj-t3-1", Title: "Fix login race condition", Status: "waiting_for_input", Settled: false},
				},
			},
		},
	}

	reg := getRegistry()
	origT3, getErr := reg.Get("t3")
	t3Eng := engine.NewT3EngineWithRunner(runner)
	reg.Register("t3", t3Eng)
	origSyncT3 := syncT3Fn
	syncT3Fn = func(ctx context.Context) error {
		return reconcileT3WithEngine(ctx, t3Eng)
	}
	t.Cleanup(func() {
		syncT3Fn = origSyncT3
		if getErr == nil && origT3 != nil {
			reg.Register("t3", origT3)
		}
	})

	if err := state.MutateStateForEngine("t3", func(s *state.State) error {
		s.RecordTask("buddy", "/tmp/buddy", "proj-t3-1", "buddy", state.TaskState{
			TaskSlug:     "feat-live",
			Engine:       "t3",
			WindowID:     "th_live",
			AgentBlockID: "th_live",
			Status:       "running",
			LastSummary:  "Working...",
		})
		s.RecordTask("buddy", "/tmp/buddy", "proj-t3-1", "buddy", state.TaskState{
			TaskSlug:     "feat-gone",
			Engine:       "t3",
			WindowID:     "th_gone",
			AgentBlockID: "th_gone",
			Status:       "running",
			LastSummary:  "Still running",
		})
		return nil
	}); err != nil {
		t.Fatalf("seed state failed: %v", err)
	}

	cmd := newStatusCmd()
	cmd.SetArgs([]string{"--sync", "--engine", "t3", "--format", "json"})
	outJSON, _, err := captureOutput(t, func() error { return cmd.Execute() })
	if err != nil {
		t.Fatalf("status --sync --engine t3 failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(outJSON), &payload); err != nil {
		t.Fatalf("failed to parse status JSON: %v (output: %q)", err, outJSON)
	}

	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
	}
	proj := st.Projects["buddy"]
	liveTask := proj.Tasks["feat-live"]
	if liveTask.Status != "ready_for_demo" || liveTask.LastSummary != "Implemented auth" || liveTask.LastVerifiedAt == "" {
		t.Errorf("expected feat-live reconciled to ready_for_demo/'Implemented auth', got %+v", liveTask)
	}
	goneTask := proj.Tasks["feat-gone"]
	if goneTask.Status != "missing" || !strings.Contains(goneTask.LastSummary, "not found") {
		t.Errorf("expected feat-gone marked missing, got %+v", goneTask)
	}
	if len(proj.UntrackedWindows) != 1 {
		t.Fatalf("expected 1 untracked window (settled & tracked skipped), got %+v", proj.UntrackedWindows)
	}
	uw := proj.UntrackedWindows[0]
	if uw.WindowID != "th_untracked" || uw.DetectedStatus != "waiting_for_input" || uw.Label != "Fix login race condition" {
		t.Errorf("unexpected untracked window: %+v", uw)
	}

	// Verify table format prints "t3" (not hard-coded "rex") for untracked windows.
	cmdTable := newStatusCmd()
	cmdTable.SetArgs([]string{"--no-sync", "--engine", "t3", "--format", "table"})
	outTable, _, err := captureOutput(t, func() error { return cmdTable.Execute() })
	if err != nil {
		t.Fatalf("status --format table failed: %v", err)
	}
	if !strings.Contains(outTable, "t3") || strings.Contains(outTable, " rex ") {
		t.Errorf("expected table output to print 't3' engine for untracked thread, got:\n%s", outTable)
	}
}
