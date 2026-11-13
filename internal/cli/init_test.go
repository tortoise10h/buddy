package cli

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestInit_MultipleProjectsRoots(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	cmd := newInitCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--projects-root", "/tmp/proj-a",
		"--projects-root", "/tmp/proj-b",
		"--projects-root", "/tmp/Mobile Documents",
		"--projects-root", "/tmp/proj-a",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	want := []string{"/tmp/proj-a", "/tmp/proj-b", "/tmp/Mobile Documents"}
	if !reflect.DeepEqual(cfg.ProjectsRoots, want) {
		t.Fatalf("ProjectsRoots = %v, want %v", cfg.ProjectsRoots, want)
	}
}

func TestInit_ExistingConfigMergesProjectsRoots(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	initial := config.DefaultConfig()
	rexCfg := initial.Engines["rex"]
	rexCfg.SessionName = "CustomBuddy"
	initial.Engines["rex"] = rexCfg
	initial.ProjectsRoots = []string{"/tmp/existing-1", "/tmp/existing-2"}
	if err := config.SaveConfig(initial); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	cmd := newInitCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"--projects-root", "/tmp/existing-2",
		"--projects-root", "/tmp/new-3",
		"--default-engine", "t3",
		"--orchestrator-agent", "pi",
		"--worker-agent", "opencode",
		"--t3-provider", "antigravity",
		"--t3-model", "gemini-3.8-flash-medium",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	wantRoots := []string{"/tmp/existing-1", "/tmp/existing-2", "/tmp/new-3"}
	if !reflect.DeepEqual(cfg.ProjectsRoots, wantRoots) {
		t.Fatalf("ProjectsRoots = %v, want %v", cfg.ProjectsRoots, wantRoots)
	}
	if cfg.DefaultEngine != "t3" {
		t.Fatalf("DefaultEngine = %q, want %q", cfg.DefaultEngine, "t3")
	}
	if cfg.GetSessionName("rex") != "CustomBuddy" {
		t.Fatalf("rex SessionName = %q, want %q", cfg.GetSessionName("rex"), "CustomBuddy")
	}
	if cfg.GetEffectiveOrchestratorAgent("rex") != "pi" {
		t.Fatalf("rex OrchestratorAgent = %q, want %q", cfg.GetEffectiveOrchestratorAgent("rex"), "pi")
	}
	if cfg.GetEffectiveWorkerAgent("rex") != "opencode" {
		t.Fatalf("rex WorkerAgent = %q, want %q", cfg.GetEffectiveWorkerAgent("rex"), "opencode")
	}
	if cfg.GetEffectiveWorkerAgent("wezterm") != "opencode" {
		t.Fatalf("wezterm WorkerAgent = %q, want %q", cfg.GetEffectiveWorkerAgent("wezterm"), "opencode")
	}
	p, m := cfg.GetT3ProviderModel()
	if p != "antigravity" || m != "gemini-3.8-flash-medium" {
		t.Fatalf("T3 provider/model = %q/%q, want antigravity/gemini-3.8-flash-medium", p, m)
	}
}

func TestInit_ExistingConfigNoFlagsIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	initial := config.DefaultConfig()
	initial.ProjectsRoots = []string{filepath.Join(tmpDir, "kept")}
	initial.SetTerminalAgents("codex", "")
	if err := config.SaveConfig(initial); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	cmd := newInitCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !reflect.DeepEqual(cfg.ProjectsRoots, initial.ProjectsRoots) {
		t.Fatalf("ProjectsRoots = %v, want %v", cfg.ProjectsRoots, initial.ProjectsRoots)
	}
	if cfg.GetEffectiveOrchestratorAgent("rex") != "codex" {
		t.Fatalf("rex OrchestratorAgent = %q, want %q", cfg.GetEffectiveOrchestratorAgent("rex"), "codex")
	}
}
