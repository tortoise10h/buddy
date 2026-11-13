package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

func TestBatch_Execution(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	proj1 := filepath.Join(tmpDir, "batch-proj-1")
	proj2 := filepath.Join(tmpDir, "batch-proj-2")
	_ = os.MkdirAll(proj1, 0755)
	_ = os.MkdirAll(proj2, 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	_ = config.SaveConfig(cfg)

	mockBackend := &mockTerminalBackend{}
	mockEngine := engine.NewTerminalEngine(mockBackend)
	reg := getRegistry()
	reg.Register("rex", mockEngine)

	batchPayload := `[
		{"project": "batch-proj-1", "task": "task-a", "prompt": "do task a"},
		{"project": "batch-proj-2", "task": "task-b", "prompt": "do task b"}
	]`

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"batch",
		"--json", batchPayload,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("batch execution failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response from batch: %s", buf.String())
	}

	if resp["dispatched_count"].(float64) != 2 {
		t.Errorf("expected 2 dispatched tasks, got %v", resp["dispatched_count"])
	}

	// Verify both were recorded in state
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	p1, err := st.FindProject("batch-proj-1")
	if err != nil {
		t.Fatalf("project 1 missing in state: %v", err)
	}
	if _, err := p1.FindTask("task-a"); err != nil {
		t.Errorf("task-a missing in state: %v", err)
	}

	p2, err := st.FindProject("batch-proj-2")
	if err != nil {
		t.Fatalf("project 2 missing in state: %v", err)
	}
	if _, err := p2.FindTask("task-b"); err != nil {
		t.Errorf("task-b missing in state: %v", err)
	}
}
