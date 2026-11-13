package cli

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

func newInitCmd() *cobra.Command {
	var (
		force             bool
		defaultEngine     string
		orchestratorAgent string
		workerAgent       string
		t3Provider        string
		t3Model           string
		projectsRoots     []string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.buddy control tower directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := state.GetBuddyDir()
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}

			var created []string

			applyFlags := func(cfg *config.Config) {
				if defaultEngine != "" {
					cfg.DefaultEngine = defaultEngine
				}
				if orchestratorAgent != "" || workerAgent != "" {
					cfg.SetTerminalAgents(orchestratorAgent, workerAgent)
				}
				if t3Provider != "" || t3Model != "" {
					if cfg.Engines == nil {
						cfg.Engines = make(map[string]config.EngineConfig)
					}
					t3Cfg := cfg.Engines["t3"]
					if t3Provider != "" {
						t3Cfg.Provider = t3Provider
					}
					if t3Model != "" {
						t3Cfg.Model = t3Model
					}
					cfg.Engines["t3"] = t3Cfg
				}
			}

			// 1. config.json
			configPath := config.GetConfigPath()
			_, err := os.Stat(configPath)
			if os.IsNotExist(err) || force {
				cfg := config.DefaultConfig()
				applyFlags(cfg)
				if len(projectsRoots) > 0 {
					cfg.ProjectsRoots = mergeUniqueRoots(nil, projectsRoots)
				}
				if err := config.SaveConfig(cfg); err != nil {
					return err
				}
				created = append(created, "config.json")
			} else if err == nil && (len(projectsRoots) > 0 || defaultEngine != "" || orchestratorAgent != "" || workerAgent != "" || t3Provider != "" || t3Model != "") {
				cfg, loadErr := config.LoadConfig()
				if loadErr != nil {
					return loadErr
				}
				if len(projectsRoots) > 0 {
					cfg.ProjectsRoots = mergeUniqueRoots(cfg.ProjectsRoots, projectsRoots)
				}
				applyFlags(cfg)
				if err := config.SaveConfig(cfg); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}

			// 2. state.json
			statePath := state.GetStatePath()
			_, err = os.Stat(statePath)
			if os.IsNotExist(err) || force {
				st := &state.State{
					Version:  1,
					Projects: make(map[string]state.ProjectState),
				}
				if err := state.SaveState(st); err != nil {
					return err
				}
				created = append(created, "state.json")
			}

			resp := map[string]any{
				"status":    "ok",
				"directory": dir,
				"created":   created,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing config and state")
	cmd.Flags().StringVar(&defaultEngine, "default-engine", "", "Default execution engine (rex, wezterm, t3)")
	cmd.Flags().StringVar(&orchestratorAgent, "orchestrator-agent", "", "Default terminal orchestrator agent (e.g. agy, opencode)")
	cmd.Flags().StringVar(&workerAgent, "worker-agent", "", "Default terminal worker agent (e.g. agy, pi)")
	cmd.Flags().StringVar(&t3Provider, "t3-provider", "", "Default T3 provider (e.g. antigravity, anthropic)")
	cmd.Flags().StringVar(&t3Model, "t3-model", "", "Default T3 model (e.g. gemini-3.8-flash-medium)")
	cmd.Flags().StringArrayVar(&projectsRoots, "projects-root", nil, "Default project root directory (can be specified multiple times)")

	return cmd
}

func mergeUniqueRoots(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, list := range [][]string{existing, incoming} {
		for _, r := range list {
			if r == "" {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			merged = append(merged, r)
		}
	}
	return merged
}
