package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestProject_ResolveAndList(t *testing.T) {
	tmpRoot := t.TempDir()
	projADir := filepath.Join(tmpRoot, "alpha-app")
	projBDir := filepath.Join(tmpRoot, "beta-service")
	_ = os.MkdirAll(projADir, 0755)
	_ = os.MkdirAll(projBDir, 0755)

	cfg := &config.Config{
		ProjectsRoots: []string{tmpRoot},
	}

	// 1. Resolve exact slug
	name, path, err := ResolveProject("alpha-app", cfg, nil)
	if err != nil {
		t.Fatalf("ResolveProject failed: %v", err)
	}
	if name != "alpha-app" || path != projADir {
		t.Errorf("unexpected resolution: %s -> %s", name, path)
	}

	// 2. Resolve substring
	name, path, err = ResolveProject("beta", cfg, nil)
	if err != nil {
		t.Fatalf("ResolveProject substring failed: %v", err)
	}
	if name != "beta-service" || path != projBDir {
		t.Errorf("unexpected resolution: %s -> %s", name, path)
	}

	// 3. List
	projects, err := ListProjects(cfg.ProjectsRoots, "", nil)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}
