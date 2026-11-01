package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestResolveProject_ByDirectPath(t *testing.T) {
	tempDir := t.TempDir()
	projDir := filepath.Join(tempDir, "my-app")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	name, path := ResolveProject(projDir, cfg)

	if name != "my-app" {
		t.Errorf("expected name 'my-app', got %q", name)
	}

	evalProj, _ := filepath.EvalSymlinks(projDir)
	if path != evalProj && path != projDir {
		t.Errorf("expected path %q, got %q", evalProj, path)
	}
}

func TestResolveProject_ByNameInRoots(t *testing.T) {
	tempDir := t.TempDir()
	root1 := filepath.Join(tempDir, "root1")
	root2 := filepath.Join(tempDir, "root2")

	app1 := filepath.Join(root1, "docnotate")
	app2 := filepath.Join(root2, "daily-catch")

	_ = os.MkdirAll(app1, 0755)
	_ = os.MkdirAll(app2, 0755)

	cfg := &config.Config{
		ProjectsRoots: []string{root1, root2},
	}

	name1, path1 := ResolveProject("docnotate", cfg)
	if name1 != "docnotate" || path1 != app1 {
		t.Errorf("expected docnotate at %q, got %q at %q", app1, name1, path1)
	}

	name2, path2 := ResolveProject("daily-catch", cfg)
	if name2 != "daily-catch" || path2 != app2 {
		t.Errorf("expected daily-catch at %q, got %q at %q", app2, name2, path2)
	}
}

func TestResolveProject_BySubstringCandidate(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "projects")
	app := filepath.Join(root, "service-authentication")
	_ = os.MkdirAll(app, 0755)

	cfg := &config.Config{
		ProjectsRoots: []string{root},
	}

	name, path := ResolveProject("auth", cfg)
	if name != "service-authentication" || path != app {
		t.Errorf("expected substring match service-authentication, got %q at %q", name, path)
	}
}

func TestResolveProject_Fallback(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "projects")
	_ = os.MkdirAll(root, 0755)

	cfg := &config.Config{
		ProjectsRoots: []string{root},
	}

	name, path := ResolveProject("unknown-project", cfg)
	if name != "unknown-project" {
		t.Errorf("expected clean query name 'unknown-project', got %q", name)
	}
	expectedPath := filepath.Join(root, "unknown-project")
	if path != expectedPath {
		t.Errorf("expected fallback path %q, got %q", expectedPath, path)
	}
}
