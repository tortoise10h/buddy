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
				if projectsRoot != "" {
					cfg.ProjectsRoots = []string{projectsRoot}
				}
				if err := config.SaveConfig(cfg); err != nil {
					return err
				}
				created = append(created, "config.json")
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
	cmd.Flags().StringVar(&orchestratorAgent, "orchestrator-agent", "", "Default orchestrator agent (e.g. agy, opencode)")
	cmd.Flags().StringVar(&workerAgent, "worker-agent", "", "Default worker agent (e.g. agy, pi)")
	cmd.Flags().StringVar(&projectsRoot, "projects-root", "", "Default project root directory")

	return cmd
}
