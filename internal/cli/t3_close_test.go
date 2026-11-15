package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type closeTestT3Runner struct {
	settleCalls []string
	settleErr   error
}

func (r *closeTestT3Runner) ProjectAdd(_ context.Context, _, _ string) error { return nil }
func (r *closeTestT3Runner) ThreadStart(_ context.Context, _ t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	return &t3.ThreadStartOutput{}, nil
}
func (r *closeTestT3Runner) ThreadShow(_ context.Context, _ string) (*t3.ThreadShowOutput, error) {
	return &t3.ThreadShowOutput{}, nil
}
func (r *closeTestT3Runner) ThreadSend(_ context.Context, _, _ string) error { return nil }
func (r *closeTestT3Runner) ThreadList(_ context.Context, _ string) (*t3.ThreadListOutput, error) {
	return &t3.ThreadListOutput{}, nil
}
func (r *closeTestT3Runner) ThreadSettle(_ context.Context, threadID string) error {
	r.settleCalls = append(r.settleCalls, threadID)
	return r.settleErr
}

func setupT3CloseCLITest(t *testing.T, runner engine.T3Runner) {
	t.Helper()
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	oldReg := defaultRegistry
	t.Cleanup(func() { defaultRegistry = oldReg })
	reg := engine.NewRegistry("t3")
	reg.Register("t3", engine.NewT3EngineWithRunner(runner))
	defaultRegistry = reg
}

func TestTaskClose_T3(t *testing.T) {
	t.Run("settles once and is idempotent", func(t *testing.T) {
		runner := &closeTestT3Runner{}
		setupT3CloseCLITest(t, runner)
		seedCleanTestState(t, "t3", "buddy", "feat-settle", "ready_for_demo", "mcp:th-settle-1")

		for i := 0; i < 2; i++ {
			cmd := newTaskCloseCmd()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-settle", "--engine", "t3"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("close #%d failed: %v", i+1, err)
			}
			if stderr.Len() != 0 {
				t.Errorf("close #%d unexpected stderr: %q", i+1, stderr.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
				t.Fatalf("close #%d invalid JSON: %v", i+1, err)
			}
			if resp["status"] != "closed" || resp["thread_settled"] != true {
				t.Errorf("close #%d unexpected resp: %+v", i+1, resp)
			}
			if _, hasErr := resp["settle_error"]; hasErr {
				t.Errorf("close #%d unexpected settle_error: %+v", i+1, resp)
			}
		}

		if len(runner.settleCalls) != 2 {
			t.Fatalf("expected 2 settle calls, got %v", runner.settleCalls)
		}
		st, err := state.LoadStateForEngine("t3")
		if err != nil {
			t.Fatalf("load state: %v", err)
		}
		if st.Projects["buddy"].Tasks["feat-settle"].Status != "closed" {
			t.Errorf("expected task status closed, got %q", st.Projects["buddy"].Tasks["feat-settle"].Status)
		}
	})

	t.Run("missing thread reports thread_settled false without failing state cleanup", func(t *testing.T) {
		runner := &closeTestT3Runner{
			settleErr: fmt.Errorf("%w: The thread was not found.", t3.ErrThreadNotFound),
		}
		setupT3CloseCLITest(t, runner)
		seedCleanTestState(t, "t3", "buddy", "feat-missing", "running", "mcp:th-gone")

		cmd := newTaskCloseCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-missing", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected close to succeed best-effort, got err: %v", err)
		}

		if !strings.Contains(stderr.String(), "WARN:") || !strings.Contains(stderr.String(), "mcp:th-gone") {
			t.Errorf("expected one-line WARN on stderr, got %q", stderr.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if resp["status"] != "closed" || resp["thread_settled"] != false {
			t.Errorf("unexpected resp: %+v", resp)
		}
		if errStr, _ := resp["settle_error"].(string); !strings.Contains(errStr, "thread not found") {
			t.Errorf("expected settle_error mentioning thread not found, got %+v", resp)
		}

		st, err := state.LoadStateForEngine("t3")
		if err != nil {
			t.Fatalf("load state: %v", err)
		}
		if st.Projects["buddy"].Tasks["feat-missing"].Status != "closed" {
			t.Errorf("expected local state marked closed despite settle failure, got %q", st.Projects["buddy"].Tasks["feat-missing"].Status)
		}
	})

	t.Run("settles by windowID alone when sessionID is empty", func(t *testing.T) {
		runner := &closeTestT3Runner{}
		setupT3CloseCLITest(t, runner)

		cmd := newTaskCloseCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"-w", "mcp:th-direct", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("close -w failed: %v", err)
		}
		if len(runner.settleCalls) != 1 || runner.settleCalls[0] != "mcp:th-direct" {
			t.Errorf("expected settle call for mcp:th-direct, got %v", runner.settleCalls)
		}
	})
}

func TestTaskCleanClose_T3RecordsSettleOutcome(t *testing.T) {
	t.Run("records thread_settled true in history", func(t *testing.T) {
		runner := &closeTestT3Runner{}
		setupT3CloseCLITest(t, runner)
		seedCleanTestState(t, "t3", "buddy", "feat-clean-ok", "ready_for_demo", "mcp:th-clean-ok")

		cmd := newTaskCleanCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-clean-ok", "--close", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("clean --close failed: %v", err)
		}
		if stderr.Len() != 0 {
			t.Errorf("unexpected stderr: %q", stderr.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(stdout.Bytes(), &resp)
		if resp["status"] != "cleaned" || resp["thread_settled"] != true {
			t.Errorf("unexpected resp: %+v", resp)
		}

		rec, err := state.FindRecentHistoryRecord("buddy", "feat-clean-ok", "t3")
		if err != nil || rec == nil {
			t.Fatalf("expected history record, got rec=%v err=%v", rec, err)
		}
		if rec.CleanupVersion != 1 || rec.ThreadSettled == nil || !*rec.ThreadSettled || rec.SettleError != "" {
			t.Errorf("unexpected history record: %+v", rec)
		}
	})

	t.Run("records thread_settled false and settle_error when settle fails", func(t *testing.T) {
		runner := &closeTestT3Runner{settleErr: errors.New("t3 server unreachable")}
		setupT3CloseCLITest(t, runner)
		seedCleanTestState(t, "t3", "buddy", "feat-clean-fail", "running", "mcp:th-clean-fail")

		cmd := newTaskCleanCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-clean-fail", "--close", "--engine", "t3"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected clean --close to proceed despite settle failure, got: %v", err)
		}

		if !strings.Contains(stderr.String(), "WARN:") {
			t.Errorf("expected WARN on stderr, got %q", stderr.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(stdout.Bytes(), &resp)
		if resp["status"] != "cleaned" || resp["thread_settled"] != false {
			t.Errorf("unexpected resp: %+v", resp)
		}
		if errStr, _ := resp["settle_error"].(string); !strings.Contains(errStr, "t3 server unreachable") {
			t.Errorf("expected settle_error in resp, got %+v", resp)
		}

		rec, err := state.FindRecentHistoryRecord("buddy", "feat-clean-fail", "t3")
		if err != nil || rec == nil {
			t.Fatalf("expected history record, got rec=%v err=%v", rec, err)
		}
		if rec.CleanupVersion != 1 || rec.ThreadSettled == nil || *rec.ThreadSettled || !strings.Contains(rec.SettleError, "t3 server unreachable") {
			t.Errorf("unexpected history record: %+v", rec)
		}
	})
}
