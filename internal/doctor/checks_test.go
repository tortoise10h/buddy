package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestDoctor_DetectAndFix(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	// Create a valid project root with 1 repo
	projRoot := filepath.Join(tmpHome, "Projects")
	_ = os.MkdirAll(filepath.Join(projRoot, "demo-app"), 0755)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{projRoot}
	cfg.DefaultEngine = "rex"
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Create a fake executable for buddy-ctl
	fakeBin := filepath.Join(tmpHome, ".local", "bin")
	_ = os.MkdirAll(fakeBin, 0755)
	fakeBuddyCtl := filepath.Join(fakeBin, "buddy-ctl")
	_ = os.WriteFile(fakeBuddyCtl, []byte("#!/bin/sh\n"), 0755)

	// Create a broken AGENTS.md symlink and stale hooks.json
	_ = os.Symlink("/nonexistent/repo/AGENTS.md", filepath.Join(buddyDir, "AGENTS.md"))
	_ = os.MkdirAll(filepath.Join(buddyDir, ".agents"), 0755)
	_ = os.WriteFile(filepath.Join(buddyDir, ".agents", "hooks.json"), []byte(`{"buddy-state-reminder":{"PreInvocation":[{"type":"command","command":"/missing/buddy-ctl context-inject"}]}}`), 0644)

	lookPath := func(name string) (string, error) {
		switch name {
		case "buddy-ctl":
			return fakeBuddyCtl, nil
		case "buddy", "rex", "agy":
			return filepath.Join(fakeBin, name), nil
		default:
			return "", os.ErrNotExist
		}
	}

	// 1. Read-only doctor detects failures (broken AGENTS.md, stale hooks.json, missing skill)
	rep := Run(context.Background(), Options{
		HomeDir:  tmpHome,
		BuddyDir: buddyDir,
		Version:  "v0.2.0",
		Getenv:   func(string) string { return "" },
		LookPath: lookPath,
	})
	if rep.Summary.Fail < 3 {
		t.Fatalf("expected >=3 failures before --fix, got %+v", rep.Summary)
	}

	// 2. doctor --fix repairs AGENTS.md, hooks.json, and universal skill
	fixedRep := Run(context.Background(), Options{
		HomeDir:  tmpHome,
		BuddyDir: buddyDir,
		Version:  "v0.2.0",
		Fix:      true,
		Getenv:   func(string) string { return "" },
		LookPath: lookPath,
	})
	if fixedRep.Summary.Fail != 0 {
		var buf bytes.Buffer
		FormatHuman(&buf, fixedRep)
		t.Fatalf("expected 0 failures after --fix, got:\n%s", buf.String())
	}

	// Verify FormatHuman produces readable output
	var buf bytes.Buffer
	FormatHuman(&buf, fixedRep)
	if !strings.Contains(buf.String(), "[fixed]") {
		t.Fatalf("expected [fixed] markers in FormatHuman output:\n%s", buf.String())
	}
}
