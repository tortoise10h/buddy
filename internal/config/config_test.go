package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_DefaultsAndDotNotation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.GetEffectiveWorkerAgent() != "agy" {
		t.Errorf("expected default worker agent agy, got %s", cfg.GetEffectiveWorkerAgent())
	}

	// Test SetPath and GetPath
	if err := cfg.SetPath("worker_agent", "pi"); err != nil {
		t.Fatalf("SetPath failed: %v", err)
	}

	val, err := cfg.GetPath("worker_agent")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if val != "pi" {
		t.Errorf("expected worker_agent pi, got %v", val)
	}

	// Verify file was saved in tmpDir
	savedData, err := os.ReadFile(filepath.Join(tmpDir, "config.json"))
	if err != nil {
		t.Fatalf("failed to read saved config file: %v", err)
	}
	if len(savedData) == 0 {
		t.Fatalf("saved config file is empty")
	}
}
