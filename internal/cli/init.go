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
				if len(projectsRoots) > 0 {
					cfg.ProjectsRoots = mergeUniqueRoots(nil, projectsRoots)
				}
				if err := config.SaveConfig(cfg); err != nil {
					return err
				}
				created = append(created, "config.json")
			} else if err == nil && (len(projectsRoots) > 0 || orchestratorAgent != "" || workerAgent != "") {
				cfg, loadErr := config.LoadConfig()
				if loadErr != nil {
					return loadErr
				}
				if len(projectsRoots) > 0 {
					cfg.ProjectsRoots = mergeUniqueRoots(cfg.ProjectsRoots, projectsRoots)
				}
				if orchestratorAgent != "" {
					cfg.OrchestratorAgent = orchestratorAgent
				}
				if workerAgent != "" {
					cfg.WorkerAgent = workerAgent
				}
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
	cmd.Flags().StringVar(&orchestratorAgent, "orchestrator-agent", "", "Default orchestrator agent (e.g. agy, opencode)")
	cmd.Flags().StringVar(&workerAgent, "worker-agent", "", "Default worker agent (e.g. agy, pi)")
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
