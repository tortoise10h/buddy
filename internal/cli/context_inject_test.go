package cli

import (
	"bytes"
	"encoding/json"
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
