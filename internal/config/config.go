package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tortoise10h/buddy/internal/state"
)

type AgentConfig struct {
	Command       []string `json:"command"`
	ModelArgs     []string `json:"model_args,omitempty"`    // e.g. ["--model", "{{model}}"]
	ModelCommand  []string `json:"model_command,omitempty"` // optional full command override when --model is set
	ModelsCmd     []string `json:"models_cmd,omitempty"`    // e.g. ["agy", "models"], ["pi", "--list-models"]
	Models        []string `json:"models,omitempty"`        // optional static model list fallback
	SubmitDelayMS int      `json:"submit_delay_ms"`
	SubmitKey     string   `json:"submit_key"`
}

// EngineConfig holds engine-specific settings.
// Terminal engines (rex, wezterm) use SessionName, CaptureLines, OrchestratorAgent, and WorkerAgent.
// API/thread engines (t3, opencode) use Provider, Model, and server/desktop fields.
type EngineConfig struct {
	SessionName       string `json:"session_name,omitempty"`
	CaptureLines      int    `json:"capture_lines,omitempty"`
	OrchestratorAgent string `json:"orchestrator_agent,omitempty"`
	WorkerAgent       string `json:"worker_agent,omitempty"`

	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
	Models   []string `json:"models,omitempty"` // optional static model list (useful for t3)

	ServerURL        string `json:"server_url,omitempty"`
	AutoStartService *bool  `json:"auto_start_service,omitempty"`
	DefaultAgent     string `json:"default_agent,omitempty"`
	FocusDesktop     *bool  `json:"focus_desktop,omitempty"`
}

type Config struct {
	Version              int                     `json:"version"`
	DefaultEngine        string                  `json:"default_engine,omitempty"`
	ProjectsRoots        []string                `json:"projects_roots"`
	UseWorktreeByDefault bool                    `json:"use_worktree_by_default"`
	Notifications        map[string]any          `json:"notifications"`
	Engines              map[string]EngineConfig `json:"engines"`
	Agents               map[string]AgentConfig  `json:"agents"`
}

func GetConfigPath() string {
	return filepath.Join(state.GetBuddyDir(), "config.json")
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	defaultProjectRoot := filepath.Join(home, "Dev", "Personal", "Projects")

	return &Config{
		Version:              2,
		DefaultEngine:        "rex",
		ProjectsRoots:        []string{defaultProjectRoot},
		UseWorktreeByDefault: false,
		Notifications: map[string]any{
			"desktop_enabled":          true,
			"sound":                    "Glass",
			"auto_watchdog":            true,
			"watchdog_mode":            "all",
			"poll_interval_seconds":    5,
			"watchdog_timeout_seconds": 600,
		},
		Engines: map[string]EngineConfig{
			"rex": {
				SessionName:       "Buddy",
				CaptureLines:      120,
				OrchestratorAgent: "agy",
				WorkerAgent:       "agy",
			},
			"wezterm": {
				SessionName:       "Buddy",
				CaptureLines:      120,
				OrchestratorAgent: "agy",
				WorkerAgent:       "agy",
			},
			"t3": {
				Provider: "",
				Model:    "",
			},
			"opencode": {
				ServerURL:        "",
				AutoStartService: boolPtr(true),
				DefaultAgent:     "",
				Provider:         "",
				Model:            "",
				FocusDesktop:     boolPtr(true),
			},
		},
		Agents: map[string]AgentConfig{
			"agy": {
				Command:       []string{"agy"},
				ModelArgs:     []string{"--model", "{{model}}"},
				ModelsCmd:     []string{"agy", "models"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"pi": {
				Command:       []string{"pi"},
				ModelArgs:     []string{"--model", "{{model}}"},
				ModelsCmd:     []string{"pi", "--list-models"},
				SubmitDelayMS: 1000,
				SubmitKey:     "enter",
			},
			"opencode": {
				Command:       []string{"opencode"},
				ModelCommand:  []string{"opencode", "mini", "--model", "{{model}}"},
				ModelsCmd:     []string{"opencode", "models"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"claude": {
				Command:       []string{"claude"},
				ModelArgs:     []string{"--model", "{{model}}"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"codex": {
				Command:       []string{"codex"},
				ModelArgs:     []string{"--model", "{{model}}"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
		},
	}
}

// LoadConfig reads config.json or returns defaults merged with missing fields.
func LoadConfig() (*Config, error) {
	path := GetConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	if cfg.Engines == nil {
		cfg.Engines = make(map[string]EngineConfig)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}

	// Backfill built-in model metadata for existing config.json files that predate these fields.
	defaults := DefaultConfig()
	for name, defAgent := range defaults.Agents {
		if existing, ok := cfg.Agents[name]; ok {
			if len(existing.ModelArgs) == 0 && len(existing.ModelCommand) == 0 {
				if len(defAgent.ModelCommand) > 0 && len(existing.Command) == 1 && existing.Command[0] == defAgent.Command[0] {
					existing.ModelCommand = append([]string(nil), defAgent.ModelCommand...)
				} else if len(defAgent.ModelArgs) > 0 {
					existing.ModelArgs = append([]string(nil), defAgent.ModelArgs...)
				}
			}
			if len(existing.ModelsCmd) == 0 && len(defAgent.ModelsCmd) > 0 {
				existing.ModelsCmd = append([]string(nil), defAgent.ModelsCmd...)
			}
			cfg.Agents[name] = existing
		}
	}

	return cfg, nil
}

// SaveConfig atomically writes configuration to config.json.
func SaveConfig(cfg *Config) error {
	dir := state.GetBuddyDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := filepath.Join(dir, ".config.json.tmp")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, GetConfigPath())
}

// GetEngineConfig returns the configuration block for the given engine name.
func (c *Config) GetEngineConfig(engineName string) EngineConfig {
	if c == nil || c.Engines == nil {
		return EngineConfig{}
	}
	return c.Engines[engineName]
}

func boolPtr(v bool) *bool {
	return &v
}

// IsNonTerminalEngine returns true for API/desktop engines that do not run inside a terminal multiplexer.
func IsNonTerminalEngine(engineName string) bool {
	return engineName == "t3" || engineName == "opencode"
}

// GetEffectiveWorkerAgent returns the worker agent harness for a terminal engine.
// For non-terminal engines like "t3" or "opencode", it returns an empty string.
func (c *Config) GetEffectiveWorkerAgent(engineName string) string {
	if IsNonTerminalEngine(engineName) {
		return ""
	}
	if ec, ok := c.Engines[engineName]; ok && ec.WorkerAgent != "" {
		return ec.WorkerAgent
	}
	if engineName != "" && engineName != c.DefaultEngine && c.DefaultEngine != "" && !IsNonTerminalEngine(c.DefaultEngine) {
		if ec, ok := c.Engines[c.DefaultEngine]; ok && ec.WorkerAgent != "" {
			return ec.WorkerAgent
		}
	}
	return "agy"
}

// GetEffectiveOrchestratorAgent returns the orchestrator agent harness for a terminal engine.
// For non-terminal engines ("t3", "opencode"), the user interacts directly with the app, so it returns "".
func (c *Config) GetEffectiveOrchestratorAgent(engineName string) string {
	if IsNonTerminalEngine(engineName) {
		return ""
	}
	if ec, ok := c.Engines[engineName]; ok && ec.OrchestratorAgent != "" {
		return ec.OrchestratorAgent
	}
	if engineName != "" && engineName != c.DefaultEngine && c.DefaultEngine != "" && !IsNonTerminalEngine(c.DefaultEngine) {
		if ec, ok := c.Engines[c.DefaultEngine]; ok && ec.OrchestratorAgent != "" {
			return ec.OrchestratorAgent
		}
	}
	return "agy"
}

// GetOpenCodeConfig returns resolved OpenCode engine settings with sensible defaults.
func (c *Config) GetOpenCodeConfig() (serverURL string, autoStart bool, defaultAgent string, defaultModel string, focusDesktop bool) {
	autoStart = true
	focusDesktop = true
	if c == nil || c.Engines == nil {
		return
	}
	ec, ok := c.Engines["opencode"]
	if !ok {
		return
	}
	serverURL = strings.TrimSpace(ec.ServerURL)
	if ec.AutoStartService != nil {
		autoStart = *ec.AutoStartService
	}
	defaultAgent = strings.TrimSpace(ec.DefaultAgent)
	p := strings.TrimSpace(ec.Provider)
	m := strings.TrimSpace(ec.Model)
	if p != "" && m != "" {
		defaultModel = p + "/" + m
	} else if m != "" && strings.Contains(m, "/") {
		defaultModel = m
	}
	if ec.FocusDesktop != nil {
		focusDesktop = *ec.FocusDesktop
	}
	return
}

// GetT3ProviderModel returns the configured provider and model for the T3 engine.
func (c *Config) GetT3ProviderModel() (string, string) {
	if c == nil || c.Engines == nil {
		return "", ""
	}
	ec := c.Engines["t3"]
	return strings.TrimSpace(ec.Provider), strings.TrimSpace(ec.Model)
}

// GetSessionName returns the Buddy session name for a terminal engine.
func (c *Config) GetSessionName(engineName string) string {
	if ec, ok := c.Engines[engineName]; ok && ec.SessionName != "" {
		return ec.SessionName
	}
	return "Buddy"
}

// GetCaptureLines returns the capture line count for a terminal engine.
func (c *Config) GetCaptureLines(engineName string) int {
	if ec, ok := c.Engines[engineName]; ok && ec.CaptureLines > 0 {
		return ec.CaptureLines
	}
	return 120
}

// SetTerminalAgents updates orchestrator_agent and/or worker_agent across terminal engines (rex, wezterm).
func (c *Config) SetTerminalAgents(orchestratorAgent, workerAgent string) {
	if c.Engines == nil {
		c.Engines = make(map[string]EngineConfig)
	}
	for _, name := range []string{"rex", "wezterm"} {
		ec := c.Engines[name]
		if ec.SessionName == "" {
			ec.SessionName = "Buddy"
		}
		if ec.CaptureLines <= 0 {
			ec.CaptureLines = 120
		}
		if orchestratorAgent != "" {
			ec.OrchestratorAgent = orchestratorAgent
		}
		if workerAgent != "" {
			ec.WorkerAgent = workerAgent
		}
		c.Engines[name] = ec
	}
}

func (c *Config) GetAgentCommand(agentName string) ([]string, int, string) {
	if agentName == "" {
		agentName = c.GetEffectiveWorkerAgent(c.DefaultEngine)
	}
	if agentName == "" {
		agentName = "agy"
	}

	if c != nil {
		if a, ok := c.Agents[agentName]; ok && len(a.Command) > 0 {
			delay := a.SubmitDelayMS
			if delay <= 0 {
				delay = 1500
			}
			key := a.SubmitKey
			if key == "" {
				key = "enter"
			}
			return cloneStrings(a.Command), delay, key
		}
	}

	// Fallback to calling agentName directly as command
	return []string{agentName}, 1500, "enter"
}

// BuildAgentCommand constructs the argv slice for a terminal worker agent, optionally
// injecting a custom model override while preserving defaults when model is empty.
func (c *Config) BuildAgentCommand(agentName, model string) ([]string, int, string, error) {
	if agentName == "" {
		agentName = c.GetEffectiveWorkerAgent(c.DefaultEngine)
	}
	if agentName == "" {
		agentName = "agy"
	}

	baseCmd, delay, key := c.GetAgentCommand(agentName)
	model = strings.TrimSpace(model)
	if model == "" {
		return baseCmd, delay, key, nil
	}

	var agentCfg AgentConfig
	var hasCfg bool
	if c != nil && c.Agents != nil {
		agentCfg, hasCfg = c.Agents[agentName]
	}

	// 1. Explicit full-command override (e.g. opencode -> ["opencode", "mini", "--model", "{{model}}"]).
	if hasCfg && len(agentCfg.ModelCommand) > 0 {
		return substituteModelPlaceholder(agentCfg.ModelCommand, model), delay, key, nil
	}

	// 2. If base command already pins a model (e.g. variant ["agy", "--model", "flash"]),
	// replace its value in-place rather than emitting duplicate flags.
	if replaced, ok := replaceExistingModelFlag(baseCmd, model); ok {
		return replaced, delay, key, nil
	}

	// 3. Inject ModelArgs (defaulting to ["--model", "{{model}}"]).
	modelArgs := agentCfg.ModelArgs
	if len(modelArgs) == 0 {
		modelArgs = []string{"--model", "{{model}}"}
	}
	injected := substituteModelPlaceholder(modelArgs, model)
	out := make([]string, 0, len(baseCmd)+len(injected))
	out = append(out, baseCmd...)
	out = append(out, injected...)
	return out, delay, key, nil
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func substituteModelPlaceholder(tokens []string, model string) []string {
	out := make([]string, len(tokens))
	replaced := false
	for i, tok := range tokens {
		if strings.Contains(tok, "{{model}}") {
			out[i] = strings.ReplaceAll(tok, "{{model}}", model)
			replaced = true
		} else {
			out[i] = tok
		}
	}
	if !replaced {
		out = append(out, model)
	}
	return out
}

func replaceExistingModelFlag(cmd []string, model string) ([]string, bool) {
	out := cloneStrings(cmd)
	for i := 1; i < len(out); i++ {
		tok := out[i]
		if tok == "--model" || tok == "-m" {
			if i+1 < len(out) {
				out[i+1] = model
				return out, true
			}
			out = append(out, model)
			return out, true
		}
		if strings.HasPrefix(tok, "--model=") {
			out[i] = "--model=" + model
			return out, true
		}
		if strings.HasPrefix(tok, "-m=") {
			out[i] = "-m=" + model
			return out, true
		}
	}
	return nil, false
}

// GetPath retrieves a value by dot-separated path from the config.
func (c *Config) GetPath(path string) (any, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	parts := strings.Split(path, ".")
	var curr any = m
	for _, part := range parts {
		asMap, ok := curr.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field '%s' is not an object", part)
		}
		val, exists := asMap[part]
		if !exists {
			return nil, fmt.Errorf("key '%s' not found", path)
		}
		curr = val
	}
	return curr, nil
}

// SetPath sets a value by dot-separated path in config and saves it.
func (c *Config) SetPath(path string, val any) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}

	parts := strings.Split(path, ".")
	curr := m
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		next, ok := curr[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			curr[part] = next
		}
		curr = next
	}
	curr[parts[len(parts)-1]] = val

	updatedData, err := json.Marshal(m)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(updatedData, c); err != nil {
		return err
	}

	return SaveConfig(c)
}
