package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type AgentConfig struct {
	Command       []string `json:"command"`
	SubmitDelayMS int      `json:"submit_delay_ms"`
	SubmitKey     string   `json:"submit_key"`
}

type NotificationConfig struct {
	DesktopEnabled         bool   `json:"desktop_enabled"`
	Sound                  string `json:"sound"`
	AutoWatchdog           bool   `json:"auto_watchdog"`
	WatchdogMode           string `json:"watchdog_mode"`
	PollIntervalSeconds    int    `json:"poll_interval_seconds"`
	WatchdogTimeoutSeconds int    `json:"watchdog_timeout_seconds"`
}

type ProjectOverride struct {
	TerminalBackend string `json:"terminal_backend,omitempty"`
}

type Config struct {
	Version              int                        `json:"version"`
	BuddySessionName     string                     `json:"buddy_session_name"`
	TerminalBackend      string                     `json:"terminal_backend,omitempty"`
	OrchestratorAgent    string                     `json:"orchestrator_agent"`
	WorkerAgent          string                     `json:"worker_agent"`
	DefaultAgent         string                     `json:"default_agent"`
	ProjectsRoots        []string                   `json:"projects_roots"`
	UseWorktreeByDefault bool                       `json:"use_worktree_by_default"`
	CaptureLines         int                        `json:"capture_lines"`
	Notifications        NotificationConfig         `json:"notifications"`
	Agents               map[string]AgentConfig     `json:"agents"`
	ProjectOverrides     map[string]ProjectOverride `json:"project_overrides,omitempty"`
}

// GetBuddyDir returns ~/.buddy or the BUDDY_DIR env var override.
func GetBuddyDir() string {
	if val := os.Getenv("BUDDY_DIR"); val != "" {
		return val
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".buddy")
}

// DefaultConfig returns the standard default configuration matching Python buddy-ctl.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.Getenv("HOME")
	}
	defaultProjectsRoot := filepath.Join(home, "Dev", "Personal", "Projects")

	return Config{
		Version:              1,
		BuddySessionName:     "Buddy",
		OrchestratorAgent:    "agy",
		WorkerAgent:          "agy",
		DefaultAgent:         "agy",
		ProjectsRoots:        []string{defaultProjectsRoot},
		UseWorktreeByDefault: false,
		CaptureLines:         120,
		Notifications: NotificationConfig{
			DesktopEnabled:         true,
			Sound:                  "Glass",
			AutoWatchdog:           true,
			WatchdogMode:           "all",
			PollIntervalSeconds:    5,
			WatchdogTimeoutSeconds: 600,
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
			"claude": {
				Command:       []string{"claude"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"codex": {
				Command:       []string{"codex"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"cursor": {
				Command:       []string{"cursor"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"gemini": {
				Command:       []string{"gemini"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"agy-argon": {
				Command:       []string{"agy", "--model", "gemini-4-argon-high"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"agy-flash": {
				Command:       []string{"agy", "--model", "gemini-3.8-flash-medium"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
			"opencode-flash": {
				Command:       []string{"opencode"},
				SubmitDelayMS: 1500,
				SubmitKey:     "enter",
			},
		},
	}
}

// LoadConfig reads ~/.buddy/config.json or returns defaults.
func LoadConfig() Config {
	cfg := DefaultConfig()
	cfgPath := filepath.Join(GetBuddyDir(), "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return cfg
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg
	}

	// Unmarshal on top of defaults
	_ = json.Unmarshal(data, &cfg)

	// Handle legacy default_agent fallback
	if legacy, ok := raw["default_agent"].(string); ok && legacy != "" {
		if raw["orchestrator_agent"] == nil {
			cfg.OrchestratorAgent = legacy
		}
		if raw["worker_agent"] == nil {
			cfg.WorkerAgent = legacy
		}
	}

	// Merge any missing agents
	defaultAgents := DefaultConfig().Agents
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}
	for k, v := range defaultAgents {
		if _, exists := cfg.Agents[k]; !exists {
			cfg.Agents[k] = v
		}
	}

	return cfg
}

// LoadRawConfig loads ~/.buddy/config.json as map[string]any with defaults merged.
func LoadRawConfig() (map[string]any, error) {
	cfgPath := filepath.Join(GetBuddyDir(), "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		// Convert DefaultConfig to map
		defaultBytes, _ := json.Marshal(DefaultConfig())
		var raw map[string]any
		_ = json.Unmarshal(defaultBytes, &raw)
		return raw, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		defaultBytes, _ := json.Marshal(DefaultConfig())
		_ = json.Unmarshal(defaultBytes, &raw)
		return raw, nil
	}

	// Merge defaults for missing keys
	defaultBytes, _ := json.Marshal(DefaultConfig())
	var defaultMap map[string]any
	_ = json.Unmarshal(defaultBytes, &defaultMap)

	legacyDefault, _ := raw["default_agent"].(string)

	for k, v := range defaultMap {
		if _, exists := raw[k]; !exists {
			if (k == "orchestrator_agent" || k == "worker_agent") && legacyDefault != "" {
				raw[k] = legacyDefault
			} else {
				raw[k] = v
			}
		}
	}

	// Merge missing agents
	if defaultAgents, ok := defaultMap["agents"].(map[string]any); ok {
		rawAgents, ok := raw["agents"].(map[string]any)
		if !ok {
			rawAgents = make(map[string]any)
			raw["agents"] = rawAgents
		}
		for ak, av := range defaultAgents {
			if _, exists := rawAgents[ak]; !exists {
				rawAgents[ak] = av
			}
		}
	}

	return raw, nil
}

// SaveRawConfig atomically saves raw config to disk.
func SaveRawConfig(raw map[string]any) error {
	buddyDir := GetBuddyDir()
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := filepath.Join(buddyDir, ".config.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, filepath.Join(buddyDir, "config.json"))
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

func (c *Config) GetEffectiveWorkerAgent() string {
	if c.WorkerAgent != "" {
		return c.WorkerAgent
	}
	if c.DefaultAgent != "" {
		return c.DefaultAgent
	}
	return "agy"
}
