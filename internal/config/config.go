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
	SubmitDelayMS int      `json:"submit_delay_ms"`
	SubmitKey     string   `json:"submit_key"`
}

type Config struct {
	Version              int                    `json:"version"`
	BuddySessionName     string                 `json:"buddy_session_name"`
	DefaultEngine        string                 `json:"default_engine,omitempty"`
	WorkerBackend        string                 `json:"worker_backend,omitempty"`
	TerminalBackend      string                 `json:"terminal_backend,omitempty"`
	OrchestratorAgent    string                 `json:"orchestrator_agent"`
	WorkerAgent          string                 `json:"worker_agent"`
	DefaultAgent         string                 `json:"default_agent"`
	ProjectsRoots        []string               `json:"projects_roots"`
	UseWorktreeByDefault bool                   `json:"use_worktree_by_default"`
	CaptureLines         int                    `json:"capture_lines"`
	Notifications        map[string]any         `json:"notifications"`
	Agents               map[string]AgentConfig `json:"agents"`
}

func GetConfigPath() string {
	return filepath.Join(state.GetBuddyDir(), "config.json")
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	defaultProjectRoot := filepath.Join(home, "Dev", "Personal", "Projects")

	return &Config{
		Version:              1,
		BuddySessionName:     "Buddy",
		DefaultEngine:        "rex",
		OrchestratorAgent:    "agy",
		WorkerAgent:          "agy",
		DefaultAgent:         "agy",
		ProjectsRoots:        []string{defaultProjectRoot},
		UseWorktreeByDefault: false,
		CaptureLines:         120,
		Notifications: map[string]any{
			"desktop_enabled":          true,
			"sound":                    "Glass",
			"auto_watchdog":            true,
			"watchdog_mode":            "all",
			"poll_interval_seconds":    5,
			"watchdog_timeout_seconds": 600,
		},
		Agents: map[string]AgentConfig{
			"agy": {
				Command:       []string{"agy"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"pi": {
				Command:       []string{"pi"},
				SubmitDelayMS: 1000,
				SubmitKey:     "enter",
			},
			"opencode": {
				Command:       []string{"opencode"},
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

	// Ensure agents map exists
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
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

func (c *Config) GetEffectiveWorkerAgent() string {
	if c.WorkerAgent != "" {
		return c.WorkerAgent
	}
	if c.DefaultAgent != "" {
		return c.DefaultAgent
	}
	return "agy"
}

func (c *Config) GetEffectiveWorkerBackend() string {
	if c.WorkerBackend != "" {
		return c.WorkerBackend
	}
	if c.DefaultEngine != "" {
		return c.DefaultEngine
	}
	return "rex"
}

func (c *Config) GetEffectiveTerminalBackend() string {
	if c.TerminalBackend != "" {
		return c.TerminalBackend
	}
	return "rex"
}

func (c *Config) GetEffectiveOrchestratorAgent() string {
	if c.OrchestratorAgent != "" {
		return c.OrchestratorAgent
	}
	if c.DefaultAgent != "" {
		return c.DefaultAgent
	}
	return "agy"
}

func (c *Config) GetAgentCommand(agentName string) ([]string, int, string) {
	if agentName == "" {
		agentName = c.GetEffectiveWorkerAgent()
	}

	if a, ok := c.Agents[agentName]; ok && len(a.Command) > 0 {
		delay := a.SubmitDelayMS
		if delay <= 0 {
			delay = 1500
		}
		key := a.SubmitKey
		if key == "" {
			key = "enter"
		}
		return a.Command, delay, key
	}

	// Fallback to calling agentName directly as command
	return []string{agentName}, 1500, "enter"
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
