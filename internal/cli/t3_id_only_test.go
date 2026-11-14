package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type cliMockT3Runner struct {
	showOut   *t3.ThreadShowOutput
	sentTexts []string
}

func (m *cliMockT3Runner) ProjectAdd(_ context.Context, _, _ string) error { return nil }
func (m *cliMockT3Runner) ThreadStart(_ context.Context, _ t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	return &t3.ThreadStartOutput{}, nil
}
func (m *cliMockT3Runner) ThreadShow(_ context.Context, _ string) (*t3.ThreadShowOutput, error) {
	return m.showOut, nil
}
func (m *cliMockT3Runner) ThreadSend(_ context.Context, _, message string) error {
	m.sentTexts = append(m.sentTexts, message)
	return nil
}

func TestT3_IDOnlyCaptureSendDiagnose(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	_ = config.SaveConfig(config.DefaultConfig())

	mockRunner := &cliMockT3Runner{
		showOut: &t3.ThreadShowOutput{
			Thread: t3.ThreadInfo{ThreadID: "th_only_1", Status: "idle"},
			Messages: []t3.MessageInfo{
				{Role: "assistant", Text: "error: sample trace line"},
			},
		},
	}
	reg := getRegistry()
	origT3, getErr := reg.Get("t3")
	reg.Register("t3", engine.NewT3EngineWithRunner(mockRunner))
	defer func() {
		if getErr == nil {
			reg.Register("t3", origT3)
		}
	}()

	// Seed a T3 task with empty RexSessionID (thread-id only in state).
	if err := state.MutateStateForEngine("t3", func(s *state.State) error {
		s.RecordTask("buddy", "/tmp/buddy", "", "buddy", state.TaskState{
			TaskSlug:     "feat-probe",
			Engine:       "t3",
			WindowID:     "th_only_1",
			AgentBlockID: "th_only_1",
			Status:       "running",
		})
		return nil
	}); err != nil {
		t.Fatalf("seed state failed: %v", err)
	}

	t.Run("capture with -b only (no -p, no -s)", func(t *testing.T) {
		cmd := newCaptureCmd()
		cmd.SetArgs([]string{"--engine", "t3", "-b", "th_only_1", "--raw"})
		stdout, _, err := captureOutput(t, func() error { return cmd.Execute() })
		if err != nil {
			t.Fatalf("capture -b only failed: %v", err)
		}
		if !strings.Contains(stdout, "error: sample trace line") {
			t.Errorf("unexpected capture output: %q", stdout)
		}
	})

	t.Run("send with -b only (no -p, no -s)", func(t *testing.T) {
		cmd := newSendCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--engine", "t3", "-b", "th_only_1", "--text", "continue"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("send -b only failed: %v", err)
		}
		if len(mockRunner.sentTexts) != 1 || mockRunner.sentTexts[0] != "continue" {
			t.Errorf("expected sent text 'continue', got %+v", mockRunner.sentTexts)
		}
	})

	t.Run("diagnose with -p/-t when session_id is empty", func(t *testing.T) {
		cmd := newDiagnoseCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--engine", "t3", "-p", "buddy", "-t", "feat-probe"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("diagnose failed: %v", err)
		}
		var resp map[string]any
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("parse diagnose JSON failed: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("expected status ok, got %v", resp["status"])
		}
	})

	t.Run("rex capture with -b only still requires session_id", func(t *testing.T) {
		cmd := newCaptureCmd()
		cmd.SetArgs([]string{"--engine", "rex", "-b", "blk_1"})
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected error for rex capture without -s or -p, got nil")
		}
	})
}
