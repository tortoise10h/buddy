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
	initial.BuddySessionName = "CustomBuddy"
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
		"--orchestrator-agent", "pi",
		"--worker-agent", "opencode",
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
	if cfg.BuddySessionName != "CustomBuddy" {
		t.Fatalf("BuddySessionName = %q, want %q", cfg.BuddySessionName, "CustomBuddy")
	}
	if cfg.OrchestratorAgent != "pi" {
		t.Fatalf("OrchestratorAgent = %q, want %q", cfg.OrchestratorAgent, "pi")
	}
	if cfg.WorkerAgent != "opencode" {
		t.Fatalf("WorkerAgent = %q, want %q", cfg.WorkerAgent, "opencode")
	}
}

func TestInit_ExistingConfigNoFlagsIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	initial := config.DefaultConfig()
	initial.ProjectsRoots = []string{filepath.Join(tmpDir, "kept")}
	initial.OrchestratorAgent = "codex"
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
	if cfg.OrchestratorAgent != "codex" {
		t.Fatalf("OrchestratorAgent = %q, want %q", cfg.OrchestratorAgent, "codex")
	}
}
