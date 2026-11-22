package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProvisionAssets_EmbeddedIdempotentAndPreservesCustomEdits(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	skillDir := filepath.Join(tmpHome, ".agents", "skills", "buddy")
	fixedNow := func() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC) }

	// 1. First install creates all managed files
	rep, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		Version:         "v0.2.0",
		BuddyCtlPath:    "/usr/local/bin/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
		Now:             fixedNow,
	})
	if err != nil {
		t.Fatalf("ProvisionAssets failed: %v", err)
	}
	if rep.Mode != "embedded" {
		t.Fatalf("Mode = %q, want embedded", rep.Mode)
	}
	for _, a := range rep.Actions {
		if a.Status != "created" {
			t.Fatalf("expected created for %s, got %q", a.Path, a.Status)
		}
	}

	hooksData, err := os.ReadFile(filepath.Join(buddyDir, ".agents", "hooks.json"))
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}
	if !strings.Contains(string(hooksData), "/usr/local/bin/buddy-ctl context-inject") {
		t.Fatalf("hooks.json missing rendered buddy-ctl path: %s", string(hooksData))
	}

	// 2. Second run with same inputs is unchanged
	rep2, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		Version:         "v0.2.0",
		BuddyCtlPath:    "/usr/local/bin/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
		Now:             fixedNow,
	})
	if err != nil {
		t.Fatalf("second ProvisionAssets failed: %v", err)
	}
	for _, a := range rep2.Actions {
		if a.Status != "unchanged" {
			t.Fatalf("expected unchanged for %s, got %q", a.Path, a.Status)
		}
	}

	// 3. User customizes AGENTS.md -> preserved without --force
	agentsPath := filepath.Join(buddyDir, "AGENTS.md")
	customContent := []byte("# Custom User Rules\n")
	if err := os.WriteFile(agentsPath, customContent, 0644); err != nil {
		t.Fatalf("write custom AGENTS.md: %v", err)
	}

	rep3, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		Version:         "v0.2.1",
		BuddyCtlPath:    "/usr/local/bin/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
		Now:             fixedNow,
	})
	if err != nil {
		t.Fatalf("third ProvisionAssets failed: %v", err)
	}
	if rep3.Actions[0].Status != "preserved" {
		t.Fatalf("expected AGENTS.md preserved, got %q", rep3.Actions[0].Status)
	}
	gotAgents, _ := os.ReadFile(agentsPath)
	if string(gotAgents) != string(customContent) {
		t.Fatalf("custom AGENTS.md was overwritten without --force")
	}

	// 4. With --force -> backed up and overwritten
	rep4, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		Version:         "v0.2.1",
		BuddyCtlPath:    "/usr/local/bin/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
		Force:           true,
		Now:             fixedNow,
	})
	if err != nil {
		t.Fatalf("fourth ProvisionAssets failed: %v", err)
	}
	if rep4.Actions[0].Status != "backed_up" {
		t.Fatalf("expected AGENTS.md backed_up with --force, got %q", rep4.Actions[0].Status)
	}
	backupPath := agentsPath + ".bak-20261122100000"
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("expected backup file at %s: %v", backupPath, err)
	}
}

func TestProvisionAssets_DevLinkAndDryRun(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	skillDir := filepath.Join(tmpHome, ".agents", "skills", "buddy")

	// Create fake dev repo
	repoDir := filepath.Join(tmpHome, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "templates"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "skills", "buddy"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "templates", "orchestrator-AGENTS.md"), []byte("# Dev Agents\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "skills", "buddy", "SKILL.md"), []byte("# Dev Skill\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Dry run creates nothing
	dryRep, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		DevRepo:         repoDir,
		BuddyCtlPath:    "/tmp/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
		DryRun:          true,
	})
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !dryRep.DryRun {
		t.Fatalf("expected DryRun=true")
	}
	if _, err := os.Stat(filepath.Join(buddyDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create AGENTS.md")
	}

	// Real dev-link creates symlinks
	rep, err := ProvisionAssets(ProvisionOptions{
		BuddyDir:        buddyDir,
		HomeDir:         tmpHome,
		DevRepo:         repoDir,
		BuddyCtlPath:    "/tmp/buddy-ctl",
		SkillTargetDirs: []string{skillDir},
	})
	if err != nil {
		t.Fatalf("dev-link failed: %v", err)
	}
	if rep.Mode != "dev-link" {
		t.Fatalf("Mode = %q, want dev-link", rep.Mode)
	}
	target, err := os.Readlink(filepath.Join(buddyDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("expected AGENTS.md symlink: %v", err)
	}
	if target != filepath.Join(repoDir, "templates", "orchestrator-AGENTS.md") {
		t.Fatalf("AGENTS.md symlink target = %q", target)
	}
}
