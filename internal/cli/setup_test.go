package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/setup"
)

func TestSetupCmd_NonInteractiveJSON(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	cmd := newSetupCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--yes",
		"--json",
		"--projects-root", filepath.Join(tmpHome, "Projects"),
		"--default-engine", "rex",
		"--orchestrator-agent", "opencode",
		"--worker-agent", "agy",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup --yes --json failed: %v\noutput: %s", err, out.String())
	}

	var res setup.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, out.String())
	}
	if res.Status != "ok" || res.DefaultEngine != "rex" || res.OrchestratorAgent != "opencode" || res.WorkerAgent != "agy" {
		t.Fatalf("unexpected setup result: %+v", res)
	}
}

func TestDoctorCmd_JSON(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--json"})

	// Fresh empty temp dir will have failing checks (expected exit error), but must emit valid JSON
	_ = cmd.Execute()

	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("doctor --json did not emit valid JSON: %v\nraw: %s", err, out.String())
	}
	if _, ok := parsed["checks"]; !ok {
		t.Fatalf("expected 'checks' key in doctor JSON: %s", out.String())
	}
}

func TestRootCmd_SetupForceDoesNotCorruptSliceFlags(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)
	t.Setenv("BUDDY_NONINTERACTIVE", "1")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"setup", "--force", "--json"})
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	}()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd setup --force --json failed: %v\noutput: %s", err, out.String())
	}

	var res setup.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, out.String())
	}
	if res.Provision == nil || len(res.Provision.Actions) == 0 {
		t.Fatalf("expected assets to be provisioned (slice flag --only must not default to ['[]']), got: %+v", res)
	}
	for _, r := range res.ProjectsRoots {
		if filepath.Base(r) == "[]" {
			t.Fatalf("projects_roots polluted with literal '[]': %v", res.ProjectsRoots)
		}
	}
}
