package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestModelsCmd_AgyPiOpenCodeAndQuery(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	origRun := runModelsCommandFn
	t.Cleanup(func() { runModelsCommandFn = origRun })
	runModelsCommandFn = func(_ context.Context, argv []string) (string, error) {
		joined := strings.Join(argv, " ")
		switch {
		case strings.Contains(joined, "agy models"):
			return "gemini-4-neon-high\tGemini 4 Neon (High)\ngemini-4-argon-medium\tGemini 4 Argon (Medium)\n", nil
		case strings.Contains(joined, "pi --list-models"):
			return "provider     model                context  max-out  thinking  images\nopencode-go  deepseek-v4-pro      1M       384K     yes       no\nopencode-go  claude-haiku-5-5     1M       128K     yes       yes\n", nil
		case strings.Contains(joined, "models"):
			return "openai/gpt-5.6-cyber\nopencode-go/deepseek-v4.1-flash\n", nil
		default:
			return "", nil
		}
	}

	// 1. agy JSON
	buf := new(bytes.Buffer)
	cmd := newModelsCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--agent", "agy"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("models --agent agy failed: %v", err)
	}
	var agyResp ModelsResponse
	if err := json.Unmarshal(buf.Bytes(), &agyResp); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, buf.String())
	}
	if agyResp.Agent != "agy" || len(agyResp.Models) != 2 {
		t.Fatalf("unexpected agy response: %+v", agyResp)
	}
	if agyResp.Models[0].ID != "gemini-4-neon-high" || agyResp.Models[0].Name != "Gemini 4 Neon (High)" {
		t.Fatalf("unexpected parsed agy model: %+v", agyResp.Models[0])
	}

	// 2. pi with --query filtering
	buf.Reset()
	cmdPi := newModelsCmd()
	cmdPi.SetOut(buf)
	cmdPi.SetErr(buf)
	cmdPi.SetArgs([]string{"--agent", "pi", "--query", "deepseek"})
	if err := cmdPi.Execute(); err != nil {
		t.Fatalf("models --agent pi failed: %v", err)
	}
	var piResp ModelsResponse
	if err := json.Unmarshal(buf.Bytes(), &piResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(piResp.Models) != 1 || piResp.Models[0].ID != "opencode-go/deepseek-v4-pro" || piResp.Models[0].Thinking != "yes" {
		t.Fatalf("unexpected pi response: %+v", piResp)
	}

	// 3. opencode engine with brief format
	buf.Reset()
	cmdOC := newModelsCmd()
	cmdOC.SetOut(buf)
	cmdOC.SetErr(buf)
	cmdOC.SetArgs([]string{"--engine", "opencode", "--format", "brief"})
	if err := cmdOC.Execute(); err != nil {
		t.Fatalf("models --engine opencode failed: %v", err)
	}
	if !strings.Contains(buf.String(), "openai/gpt-5.6-cyber") {
		t.Fatalf("expected opencode model in brief output, got:\n%s", buf.String())
	}
}

func TestModelsCmd_T3FallbackNote(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.Engines["t3"] = config.EngineConfig{
		Provider: "antigravity",
		Model:    "gemini-3.8-flash-medium",
		Models:   []string{"antigravity/gemini-4-neon-high", "antigravity/gemini-4-neon-medium"},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	buf := new(bytes.Buffer)
	cmd := newModelsCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--engine", "t3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("models --engine t3 failed: %v", err)
	}

	var resp ModelsResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Engine != "t3" || resp.DefaultModel != "antigravity/gemini-3.8-flash-medium" {
		t.Fatalf("unexpected t3 models response: %+v", resp)
	}
	if !strings.Contains(strings.ToLower(resp.Note), "live models") {
		t.Fatalf("expected fallback note for t3, got %q", resp.Note)
	}
	if len(resp.Models) != 3 {
		t.Fatalf("expected 3 deduplicated models, got %+v", resp.Models)
	}
}

func TestModelsCmd_ClaudeAndCodexSupport(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// 1. Default claude returns built-in aliases with anthropic provider.
	buf := new(bytes.Buffer)
	cmdClaude := newModelsCmd()
	cmdClaude.SetOut(buf)
	cmdClaude.SetErr(buf)
	cmdClaude.SetArgs([]string{"--agent", "claude"})
	if err := cmdClaude.Execute(); err != nil {
		t.Fatalf("models --agent claude failed: %v", err)
	}
	var claudeResp ModelsResponse
	if err := json.Unmarshal(buf.Bytes(), &claudeResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if claudeResp.Source != "builtin" || len(claudeResp.Models) != 3 || claudeResp.Models[0].Provider != "anthropic" {
		t.Fatalf("unexpected claude builtin response: %+v", claudeResp)
	}

	// 2. Default codex returns clear config fallback note.
	buf.Reset()
	cmdCodex := newModelsCmd()
	cmdCodex.SetOut(buf)
	cmdCodex.SetErr(buf)
	cmdCodex.SetArgs([]string{"--agent", "codex"})
	if err := cmdCodex.Execute(); err != nil {
		t.Fatalf("models --agent codex failed: %v", err)
	}
	var codexResp ModelsResponse
	if err := json.Unmarshal(buf.Bytes(), &codexResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !strings.Contains(strings.ToLower(codexResp.Note), "codex") {
		t.Fatalf("expected codex fallback note, got %+v", codexResp)
	}

	// 3. Custom models_cmd outputs parsed via parseClaudeModelsOutput & parseCodexModelsOutput.
	claudeCfg := cfg.Agents["claude"]
	claudeCfg.ModelsCmd = []string{"claude-models-mock"}
	cfg.Agents["claude"] = claudeCfg
	codexCfg := cfg.Agents["codex"]
	codexCfg.ModelsCmd = []string{"codex-models-mock"}
	cfg.Agents["codex"] = codexCfg
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	origRun := runModelsCommandFn
	t.Cleanup(func() { runModelsCommandFn = origRun })
	runModelsCommandFn = func(_ context.Context, argv []string) (string, error) {
		switch argv[0] {
		case "claude-models-mock":
			return "claude-opus-4-6\tClaude Opus 4.6\n- claude-sonnet-4-6 (Claude Sonnet 4.6)\n", nil
		case "codex-models-mock":
			return `{"models":[{"slug":"gpt-5.3-codex","display_name":"GPT-5.3 Codex"},{"slug":"o3","display_name":"o3"}]}`, nil
		default:
			return "", nil
		}
	}

	buf.Reset()
	cmdClaudeLive := newModelsCmd()
	cmdClaudeLive.SetOut(buf)
	cmdClaudeLive.SetErr(buf)
	cmdClaudeLive.SetArgs([]string{"--agent", "claude"})
	if err := cmdClaudeLive.Execute(); err != nil {
		t.Fatalf("models --agent claude (custom cmd) failed: %v", err)
	}
	var claudeLive ModelsResponse
	_ = json.Unmarshal(buf.Bytes(), &claudeLive)
	if len(claudeLive.Models) != 2 || claudeLive.Models[0].ID != "claude-opus-4-6" || claudeLive.Models[0].Provider != "anthropic" || claudeLive.Models[1].Name != "Claude Sonnet 4.6" {
		t.Fatalf("unexpected parsed claude models: %+v", claudeLive.Models)
	}

	buf.Reset()
	cmdCodexLive := newModelsCmd()
	cmdCodexLive.SetOut(buf)
	cmdCodexLive.SetErr(buf)
	cmdCodexLive.SetArgs([]string{"--agent", "codex"})
	if err := cmdCodexLive.Execute(); err != nil {
		t.Fatalf("models --agent codex (custom cmd) failed: %v", err)
	}
	var codexLive ModelsResponse
	_ = json.Unmarshal(buf.Bytes(), &codexLive)
	if len(codexLive.Models) != 2 || codexLive.Models[0].ID != "gpt-5.3-codex" || codexLive.Models[0].Provider != "openai" || codexLive.Models[0].Name != "GPT-5.3 Codex" {
		t.Fatalf("unexpected parsed codex models: %+v", codexLive.Models)
	}
}
