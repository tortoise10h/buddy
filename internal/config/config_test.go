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

	if cfg.GetEffectiveWorkerAgent("rex") != "agy" {
		t.Errorf("expected default rex worker agent agy, got %s", cfg.GetEffectiveWorkerAgent("rex"))
	}
	if cfg.GetEffectiveOrchestratorAgent("t3") != "" {
		t.Errorf("expected empty orchestrator agent for t3, got %s", cfg.GetEffectiveOrchestratorAgent("t3"))
	}

	p, m := cfg.GetT3ProviderModel()
	if p != "" || m != "" {
		t.Errorf("expected empty default t3 provider/model, got %q/%q", p, m)
	}

	// Test SetPath and GetPath with per-engine config
	if err := cfg.SetPath("engines.rex.worker_agent", "pi"); err != nil {
		t.Fatalf("SetPath failed: %v", err)
	}

	val, err := cfg.GetPath("engines.rex.worker_agent")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if val != "pi" {
		t.Errorf("expected engines.rex.worker_agent pi, got %v", val)
	}

	if err := cfg.SetPath("engines.t3.provider", "antigravity"); err != nil {
		t.Fatalf("SetPath t3 provider failed: %v", err)
	}
	if err := cfg.SetPath("engines.t3.model", "gemini-3.8-flash-medium"); err != nil {
		t.Fatalf("SetPath t3 model failed: %v", err)
	}

	p, m = cfg.GetT3ProviderModel()
	if p != "antigravity" || m != "gemini-3.8-flash-medium" {
		t.Errorf("expected antigravity/gemini-3.8-flash-medium, got %q/%q", p, m)
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
