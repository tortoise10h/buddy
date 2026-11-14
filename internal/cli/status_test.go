package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
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
