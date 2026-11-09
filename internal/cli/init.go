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
		orchestratorAgent string
		workerAgent       string
		workerBackend     string
		projectsRoot      string
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

			// 1. config.json
			configPath := config.GetConfigPath()
			_, err := os.Stat(configPath)
			if os.IsNotExist(err) || force {
				cfg := config.DefaultConfig()
				if orchestratorAgent != "" {
					cfg.OrchestratorAgent = orchestratorAgent
				}
				if workerAgent != "" {
					cfg.WorkerAgent = workerAgent
				}
				if workerBackend != "" {
					cfg.WorkerBackend = workerBackend
				}
				if projectsRoot != "" {
					cfg.ProjectsRoots = []string{projectsRoot}
				}
				if err := config.SaveConfig(cfg); err != nil {
					return err
				}
				created = append(created, "config.json")
			} else {
				// Update existing config with provided flags if any
				cfg, err := config.LoadConfig()
				if err == nil && cfg != nil {
					modified := false
					if orchestratorAgent != "" && cfg.OrchestratorAgent != orchestratorAgent {
						cfg.OrchestratorAgent = orchestratorAgent
						modified = true
					}
					if workerAgent != "" && cfg.WorkerAgent != workerAgent {
						cfg.WorkerAgent = workerAgent
						modified = true
					}
					if workerBackend != "" && cfg.WorkerBackend != workerBackend {
						cfg.WorkerBackend = workerBackend
						modified = true
					}
					if projectsRoot != "" {
						cfg.ProjectsRoots = []string{projectsRoot}
						modified = true
					}
					if modified {
						_ = config.SaveConfig(cfg)
					}
				}
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
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing config and state")
	cmd.Flags().StringVar(&orchestratorAgent, "orchestrator-agent", "", "Default orchestrator agent (e.g. agy, opencode)")
	cmd.Flags().StringVar(&workerAgent, "worker-agent", "", "Default worker agent (e.g. agy, pi)")
	cmd.Flags().StringVar(&workerBackend, "worker-backend", "", "Default worker backend (rex, herdr, t3code)")
	cmd.Flags().StringVar(&projectsRoot, "projects-root", "", "Default project root directory")

	return cmd
}
