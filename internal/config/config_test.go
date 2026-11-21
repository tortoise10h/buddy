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

func TestBuildAgentCommand_ModelInjectionAndDefaults(t *testing.T) {
	cfg := DefaultConfig()

	// 1. Empty model returns verbatim command and does not share backing array.
	cmd, _, _, err := cfg.BuildAgentCommand("agy", "")
	if err != nil {
		t.Fatalf("BuildAgentCommand(agy, empty) failed: %v", err)
	}
	if len(cmd) != 1 || cmd[0] != "agy" {
		t.Fatalf("expected [agy], got %v", cmd)
	}
	cmd[0] = "mutated"
	if cfg.Agents["agy"].Command[0] != "agy" {
		t.Fatalf("expected DefaultConfig Command slice to remain immutable")
	}

	// 2. agy and pi inject --model <id>.
	agyCmd, _, _, err := cfg.BuildAgentCommand("agy", "gemini-4-neon-high")
	if err != nil {
		t.Fatalf("BuildAgentCommand(agy, model) failed: %v", err)
	}
	if got := agyCmd; len(got) != 3 || got[0] != "agy" || got[1] != "--model" || got[2] != "gemini-4-neon-high" {
		t.Fatalf("unexpected agy model cmd: %v", got)
	}

	piCmd, _, _, err := cfg.BuildAgentCommand("pi", "opencode-go/deepseek-v4-pro")
	if err != nil {
		t.Fatalf("BuildAgentCommand(pi, model) failed: %v", err)
	}
	if got := piCmd; len(got) != 3 || got[0] != "pi" || got[1] != "--model" || got[2] != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("unexpected pi model cmd: %v", got)
	}

	claudeCmd, _, _, err := cfg.BuildAgentCommand("claude", "opus")
	if err != nil {
		t.Fatalf("BuildAgentCommand(claude, model) failed: %v", err)
	}
	if len(claudeCmd) != 3 || claudeCmd[0] != "claude" || claudeCmd[1] != "--model" || claudeCmd[2] != "opus" {
		t.Fatalf("unexpected claude model cmd: %v", claudeCmd)
	}

	codexCmd, _, _, err := cfg.BuildAgentCommand("codex", "gpt-5-codex")
	if err != nil {
		t.Fatalf("BuildAgentCommand(codex, model) failed: %v", err)
	}
	if len(codexCmd) != 3 || codexCmd[0] != "codex" || codexCmd[1] != "--model" || codexCmd[2] != "gpt-5-codex" {
		t.Fatalf("unexpected codex model cmd: %v", codexCmd)
	}

	// 3. opencode uses ModelCommand when --model is set, and verbatim ["opencode"] when empty.
	ocDefault, _, _, err := cfg.BuildAgentCommand("opencode", "")
	if err != nil {
		t.Fatalf("BuildAgentCommand(opencode, empty) failed: %v", err)
	}
	if len(ocDefault) != 1 || ocDefault[0] != "opencode" {
		t.Fatalf("expected [opencode] when model empty, got %v", ocDefault)
	}
	ocModel, _, _, err := cfg.BuildAgentCommand("opencode", "openai/gpt-5.6-cyber")
	if err != nil {
		t.Fatalf("BuildAgentCommand(opencode, model) failed: %v", err)
	}
	if len(ocModel) != 4 || ocModel[0] != "opencode" || ocModel[1] != "mini" || ocModel[2] != "--model" || ocModel[3] != "openai/gpt-5.6-cyber" {
		t.Fatalf("unexpected opencode model cmd: %v", ocModel)
	}

	// 4. Pre-pinned variant replaces existing --model / --model= rather than duplicating.
	cfg.Agents["agy-flash"] = AgentConfig{
		Command: []string{"agy", "--model", "gemini-3.8-flash-medium"},
	}
	replaced, _, _, err := cfg.BuildAgentCommand("agy-flash", "gemini-4-neon-high")
	if err != nil {
		t.Fatalf("BuildAgentCommand(agy-flash, override) failed: %v", err)
	}
	if len(replaced) != 3 || replaced[0] != "agy" || replaced[1] != "--model" || replaced[2] != "gemini-4-neon-high" {
		t.Fatalf("expected replaced --model flag, got %v", replaced)
	}

	cfg.Agents["agy-eq"] = AgentConfig{
		Command: []string{"agy", "--model=gemini-3.8-flash-medium"},
	}
	replacedEq, _, _, err := cfg.BuildAgentCommand("agy-eq", "gemini-4-neon-high")
	if err != nil {
		t.Fatalf("BuildAgentCommand(agy-eq, override) failed: %v", err)
	}
	if len(replacedEq) != 2 || replacedEq[0] != "agy" || replacedEq[1] != "--model=gemini-4-neon-high" {
		t.Fatalf("expected replaced --model= flag, got %v", replacedEq)
	}
}

func TestLoadConfig_BackfillsBuiltInModelMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	legacyJSON := `{
  "version": 2,
  "default_engine": "rex",
  "agents": {
    "agy": {"command": ["agy"], "submit_delay_ms": 1500, "submit_key": "enter"},
    "opencode": {"command": ["opencode"], "submit_delay_ms": 1500, "submit_key": "enter"}
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Agents["agy"].ModelsCmd) == 0 || cfg.Agents["agy"].ModelsCmd[0] != "agy" {
		t.Fatalf("expected agy models_cmd to be backfilled, got %+v", cfg.Agents["agy"])
	}
	if len(cfg.Agents["opencode"].ModelCommand) != 4 || cfg.Agents["opencode"].ModelCommand[1] != "mini" {
		t.Fatalf("expected opencode model_command to be backfilled, got %+v", cfg.Agents["opencode"])
	}
}
