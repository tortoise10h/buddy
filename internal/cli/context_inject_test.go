package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/state"
)

func TestContextInject_FastExecution(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Create test state
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("my-app", "/path/to/app", "sess_app", "My App", state.TaskState{
			TaskSlug:    "feat-ui",
			Status:      "running",
			LastSummary: "building button",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"context-inject"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("context-inject failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON from context-inject, got: %s", buf.String())
	}

	injectSteps, ok := out["injectSteps"].([]any)
	if !ok || len(injectSteps) == 0 {
		t.Fatalf("expected injectSteps array, got %+v", out)
	}
}

func TestContextInject_FiltersDeadTasksAndShowsUntrackedAndWatch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-a", "/path/a", "sess_a", "proj-a", state.TaskState{
			TaskSlug:    "live-auth",
			Status:      "running",
			LastSummary: "implementing JWT",
		})
		s.RecordTask("proj-a", "/path/a", "sess_a", "proj-a", state.TaskState{
			TaskSlug:    "dead-missing",
			Status:      "missing",
			LastSummary: "Rex window closed",
		})
		s.RecordTask("proj-a", "/path/a", "sess_a", "proj-a", state.TaskState{
			TaskSlug:    "dead-closed",
			Status:      "closed",
			LastSummary: "closed earlier",
		})
		p := s.Projects["proj-a"]
		p.UntrackedWindows = []state.UntrackedWindow{
			{WindowID: "win_u1", Label: "manual-debug", BlockID: "blk_u1", DetectedStatus: "waiting_for_input", Summary: "Need DB password?"},
		}
		s.Projects["proj-a"] = p
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"context-inject"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("context-inject failed: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("expected valid JSON from context-inject, got: %s", buf.String())
	}

	steps := out["injectSteps"].([]any)
	msg := steps[0].(map[string]any)["ephemeralMessage"].(string)

	if !strings.Contains(msg, "proj-a/live-auth") || !strings.Contains(msg, "running: 'implementing JWT'") {
		t.Errorf("expected live task in context-inject output, got:\n%s", msg)
	}
	if strings.Contains(msg, "dead-missing") || strings.Contains(msg, "dead-closed") {
		t.Errorf("expected missing/closed tasks to be filtered out, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Untracked live tabs") || !strings.Contains(msg, "manual-debug") {
		t.Errorf("expected untracked windows to be surfaced in context-inject output, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Situational Watchdog: 'buddy-ctl watch --project proj-a --task live-auth") {
		t.Errorf("expected situational watchdog command in context-inject output, got:\n%s", msg)
	}
}
