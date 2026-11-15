package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type spyRexCloseBackend struct {
	mockTerminalBackend
	closeCalls []string
}

func (s *spyRexCloseBackend) Close(ctx context.Context, sessionID, windowID string) error {
	s.closeCalls = append(s.closeCalls, sessionID+":"+windowID)
	return nil
}

func seedCleanTestState(t *testing.T, engName, projName, slug, status, winID string) {
	t.Helper()
	if err := state.MutateStateForEngine(engName, func(s *state.State) error {
		s.RecordTask(projName, "/tmp/"+projName, "sess-"+projName, projName, state.TaskState{
			TaskSlug:     slug,
			Engine:       engName,
			WindowID:     winID,
			WindowLabel:  slug,
			AgentBlockID: winID,
			Status:       status,
			LastSummary:  "test summary",
		})
		return nil
	}); err != nil {
		t.Fatalf("seed state failed: %v", err)
	}
}

func TestTaskClean_ClosedTask_AppendsHistoryAndKeepsEmptyProject(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	seedCleanTestState(t, "t3", "buddy", "feat-auth", "closed", "mcp:th-1")

	// Seed a seen entry to verify cleanup.
	seenPath := state.GetStateSeenPathForEngine("t3")
	_ = os.WriteFile(seenPath, []byte(`{"buddy/feat-auth":{"status":"closed","updated_at":"2026-11-15T10:00:00Z"}}`), 0644)

	cmd := newTaskCleanCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-auth", "--reason", "merged PR #42", "--engine", "t3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("task clean failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v (%s)", err, out.String())
	}
	if resp["status"] != "cleaned" {
		t.Errorf("status = %v, want cleaned", resp["status"])
	}
	if resp["pruned_project"] != false {
		t.Errorf("pruned_project = %v, want false", resp["pruned_project"])
	}
	if resp["forced"] != false {
		t.Errorf("forced = %v, want false", resp["forced"])
	}

	// Verify task deleted but project kept.
	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine failed: %v", err)
	}
	proj, ok := st.Projects["buddy"]
	if !ok {
		t.Fatalf("expected empty project 'buddy' to be kept by default")
	}
	if len(proj.Tasks) != 0 {
		t.Fatalf("expected 0 tasks in buddy, got %d", len(proj.Tasks))
	}

	// Verify seen entry removed.
	seenData, _ := os.ReadFile(seenPath)
	if strings.Contains(string(seenData), "buddy/feat-auth") {
		t.Errorf("expected buddy/feat-auth to be removed from seen cache")
	}

	// Verify history record appended.
	rec, err := state.FindRecentHistoryRecord("buddy", "feat-auth", "t3")
	if err != nil || rec == nil {
		t.Fatalf("expected history record, got rec=%v err=%v", rec, err)
	}
	if rec.Reason != "merged PR #42" || rec.FinalStatus != "closed" || rec.Forced {
		t.Errorf("unexpected history record: %+v", rec)
	}
}

func TestTaskClean_PruneEmptyProject(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "rex")
	_ = config.SaveConfig(config.DefaultConfig())

	seedCleanTestState(t, "rex", "buddy", "feat-prune", "done", "win-1")

	cmd := newTaskCleanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-prune", "--prune-empty-project", "--engine", "rex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("task clean failed: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(out.Bytes(), &resp)
	if resp["pruned_project"] != true {
		t.Errorf("pruned_project = %v, want true", resp["pruned_project"])
	}

	st, err := state.LoadStateForEngine("rex")
	if err != nil {
		t.Fatalf("LoadStateForEngine failed: %v", err)
	}
	if _, ok := st.Projects["buddy"]; ok {
		t.Fatalf("expected empty project 'buddy' to be pruned with --prune-empty-project")
	}
}

func TestTaskClean_RefusesActiveWithoutForce(t *testing.T) {
	statuses := []string{"running", "planning", "waiting_for_input", "ready_for_demo", "error", "starting", "missing"}
	for _, stName := range statuses {
		t.Run(stName, func(t *testing.T) {
			clearAllHostSignals(t)
			t.Setenv("BUDDY_DIR", t.TempDir())
			t.Setenv("BUDDY_ENGINE", "t3")
			_ = config.SaveConfig(config.DefaultConfig())

			seedCleanTestState(t, "t3", "buddy", "feat-active", stName, "mcp:th-active")

			cmd := newTaskCleanCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-active", "--engine", "t3"})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected refusal for status %q", stName)
			}
			if !strings.Contains(err.Error(), "not closed") {
				t.Errorf("unexpected error message: %v", err)
			}

			// Verify state untouched and no history written.
			st, _ := state.LoadStateForEngine("t3")
			if _, ok := st.Projects["buddy"].Tasks["feat-active"]; !ok {
				t.Errorf("task should still be in state after refusal")
			}
			if _, statErr := os.Stat(state.GetHistoryPath()); !os.IsNotExist(statErr) {
				t.Errorf("history file should not exist after refusal")
			}
		})
	}
}

func TestTaskClean_ForceAndCloseFlags(t *testing.T) {
	t.Run("force prints stderr warning and sets forced=true", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "t3")
		_ = config.SaveConfig(config.DefaultConfig())

		seedCleanTestState(t, "t3", "buddy", "feat-orphan", "running", "mcp:th-orphan")

		cmd := newTaskCleanCmd()
		var out, errBuf bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-orphan", "--force", "--reason", "user agreed to drop", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("force clean failed: %v", err)
		}

		if !strings.Contains(errBuf.String(), "WARN:") || !strings.Contains(errBuf.String(), "buddy/feat-orphan") {
			t.Errorf("expected stderr warning naming task, got: %q", errBuf.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(out.Bytes(), &resp)
		if resp["forced"] != true {
			t.Errorf("forced = %v, want true", resp["forced"])
		}

		rec, _ := state.FindRecentHistoryRecord("buddy", "feat-orphan", "t3")
		if rec == nil || !rec.Forced || rec.FinalStatus != "running" {
			t.Errorf("unexpected forced history record: %+v", rec)
		}
	})

	t.Run("force does not call eng.Close on rex", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "rex")
		_ = config.SaveConfig(config.DefaultConfig())

		spy := &spyRexCloseBackend{}
		oldReg := defaultRegistry
		t.Cleanup(func() { defaultRegistry = oldReg })
		reg := engine.NewRegistry("rex")
		reg.Register("rex", engine.NewTerminalEngine(spy))
		defaultRegistry = reg

		seedCleanTestState(t, "rex", "buddy", "feat-rex-force", "running", "win-force")

		cmd := newTaskCleanCmd()
		var out, errBuf bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-rex-force", "--force", "--reason", "user agreed", "--engine", "rex"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("force clean failed: %v", err)
		}
		if len(spy.closeCalls) != 0 {
			t.Errorf("expected eng.Close NOT to be called on --force, got calls: %v", spy.closeCalls)
		}
	})

	t.Run("close flag chains engine.Close then cleans on rex", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "rex")
		_ = config.SaveConfig(config.DefaultConfig())

		spy := &spyRexCloseBackend{}
		oldReg := defaultRegistry
		t.Cleanup(func() { defaultRegistry = oldReg })
		reg := engine.NewRegistry("rex")
		reg.Register("rex", engine.NewTerminalEngine(spy))
		defaultRegistry = reg

		seedCleanTestState(t, "rex", "buddy", "feat-rex-close", "running", "win-close")

		cmd := newTaskCleanCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-rex-close", "--close", "--reason", "verified", "--engine", "rex"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("--close clean failed: %v", err)
		}
		if len(spy.closeCalls) != 1 || spy.closeCalls[0] != "sess-buddy:win-close" {
			t.Errorf("expected 1 eng.Close call for sess-buddy:win-close, got: %v", spy.closeCalls)
		}
	})

	t.Run("close flag chains engine.Close then cleans", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "t3")
		_ = config.SaveConfig(config.DefaultConfig())

		oldReg := defaultRegistry
		t.Cleanup(func() { defaultRegistry = oldReg })
		reg := engine.NewRegistry("t3")
		reg.Register("t3", engine.NewT3Engine())
		defaultRegistry = reg

		seedCleanTestState(t, "t3", "buddy", "feat-chain", "ready_for_demo", "mcp:th-chain")

		cmd := newTaskCleanCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-chain", "--close", "--reason", "verified", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("--close clean failed: %v", err)
		}

		var resp map[string]any
		_ = json.Unmarshal(out.Bytes(), &resp)
		if resp["status"] != "cleaned" || resp["final_status"] != "closed" || resp["forced"] != false {
			t.Errorf("unexpected response: %+v", resp)
		}
	})
}

func TestTaskClean_IdempotencyAndNotFound(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	seedCleanTestState(t, "t3", "buddy", "feat-idem", "closed", "mcp:th-idem")

	// First clean
	cmd1 := newTaskCleanCmd()
	var out1 bytes.Buffer
	cmd1.SetOut(&out1)
	cmd1.SetArgs([]string{"-p", "buddy", "-t", "feat-idem", "--engine", "t3"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first clean failed: %v", err)
	}

	// Second clean (idempotent)
	cmd2 := newTaskCleanCmd()
	var out2 bytes.Buffer
	cmd2.SetOut(&out2)
	cmd2.SetArgs([]string{"-p", "buddy", "-t", "feat-idem", "--engine", "t3"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second clean should be idempotent, got err: %v", err)
	}
	var resp2 map[string]any
	_ = json.Unmarshal(out2.Bytes(), &resp2)
	if resp2["status"] != "already_cleaned" {
		t.Errorf("second clean status = %v, want already_cleaned", resp2["status"])
	}

	// History should still have only 1 line.
	raw, _ := os.ReadFile(state.GetHistoryPath())
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 history line after idempotent clean, got %d", len(lines))
	}

	// Never-seen slug should fail.
	cmd3 := newTaskCleanCmd()
	var out3 bytes.Buffer
	cmd3.SetOut(&out3)
	cmd3.SetErr(&out3)
	cmd3.SetArgs([]string{"-p", "buddy", "-t", "never-existed", "--engine", "t3"})
	if err := cmd3.Execute(); err == nil {
		t.Fatalf("expected error for never-existed task")
	}
}

func TestTaskClean_DryRunAndHelpText(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	seedCleanTestState(t, "t3", "buddy", "feat-dry", "closed", "mcp:th-dry")

	cmd := newTaskCleanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-dry", "--dry-run", "--engine", "t3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(out.Bytes(), &resp)
	if resp["status"] != "dry_run" || resp["dry_run"] != true {
		t.Errorf("unexpected dry-run response: %+v", resp)
	}

	// State must still have the task; history must not exist.
	st, _ := state.LoadStateForEngine("t3")
	if _, ok := st.Projects["buddy"].Tasks["feat-dry"]; !ok {
		t.Errorf("dry-run mutated state")
	}
	if _, err := os.Stat(state.GetHistoryPath()); !os.IsNotExist(err) {
		t.Errorf("dry-run created history file")
	}

	// Verify --help text contains explicit user consent wording for --force.
	helpCmd := newTaskCleanCmd()
	var helpOut bytes.Buffer
	helpCmd.SetOut(&helpOut)
	helpCmd.SetArgs([]string{"--help"})
	_ = helpCmd.Execute()
	if !strings.Contains(helpOut.String(), "requires explicit user consent from the user in the current conversation") {
		t.Errorf("--help text missing explicit user consent wording: %s", helpOut.String())
	}
}

func TestTaskClean_AllClosed(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	seedCleanTestState(t, "t3", "alpha", "closed-a", "closed", "th-a")
	seedCleanTestState(t, "t3", "alpha", "running-a", "running", "th-r")
	seedCleanTestState(t, "t3", "beta", "done-b", "done", "th-b")

	// Dry-run scoped to alpha should preview only closed-a.
	dryCmd := newTaskCleanCmd()
	var dryOut bytes.Buffer
	dryCmd.SetOut(&dryOut)
	dryCmd.SetArgs([]string{"-p", "alpha", "--all-closed", "--dry-run", "--engine", "t3"})
	if err := dryCmd.Execute(); err != nil {
		t.Fatalf("all-closed dry-run failed: %v", err)
	}
	var dryResp map[string]any
	_ = json.Unmarshal(dryOut.Bytes(), &dryResp)
	if dryResp["count"] != float64(1) {
		t.Fatalf("dry-run count = %v, want 1", dryResp["count"])
	}

	// Execute --all-closed across all projects in t3 engine.
	cmd := newTaskCleanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--all-closed", "--reason", "eod cleanup", "--engine", "t3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("all-closed failed: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(out.Bytes(), &resp)
	if resp["status"] != "cleaned" || resp["count"] != float64(2) {
		t.Fatalf("unexpected all-closed response: %+v", resp)
	}

	st, _ := state.LoadStateForEngine("t3")
	if _, ok := st.Projects["alpha"].Tasks["closed-a"]; ok {
		t.Errorf("closed-a should be removed")
	}
	if _, ok := st.Projects["alpha"].Tasks["running-a"]; !ok {
		t.Errorf("running-a should remain")
	}
	if _, ok := st.Projects["beta"]; !ok {
		t.Errorf("empty project beta should be kept by default")
	}
	if len(st.Projects["beta"].Tasks) != 0 {
		t.Errorf("done-b should be removed from beta")
	}

	hist, err := state.QueryHistory(state.HistoryFilter{Engine: "t3", Limit: 10})
	if err != nil || len(hist) != 2 {
		t.Fatalf("expected 2 history records, got %d (err=%v)", len(hist), err)
	}
}
