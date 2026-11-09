package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newDispatchCmd() *cobra.Command {
	var (
		projectName   string
		taskSlug      string
		prompt        string
		agentName     string
		engineName    string
		workerBackend string
	)

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Fast-path atomic task dispatch",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("missing required --project (-p) argument")
			}
			if prompt == "" {
				return fmt.Errorf("missing required --prompt (-m) argument")
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, err := state.LoadState()
			if err != nil {
				return err
			}

			// 1. Resolve project
			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				return err
			}

			// 2. Resolve task slug
			slug := project.DeriveSlug(taskSlug, prompt, projName)

			// 3. Resolve engine
			engineName = resolveWorkerEngineName(engineName, workerBackend, cfg)
			eng, err := getEngine(engineName)
			if err != nil {
				return err
			}

			// 4. Resolve agent command / agent spec
			if agentName == "" {
				agentName = cfg.GetEffectiveWorkerAgent()
			}
			var agentCmd []string
			if engineName == "t3code" || engineName == "t3" {
				agentCmd = []string{agentName}
			} else {
				agentCmd, _, _ = cfg.GetAgentCommand(agentName)
			}

			// 5. Dispatch via engine
			res, err := eng.Dispatch(cmd.Context(), engine.DispatchRequest{
				ProjectName: projName,
				ProjectPath: projPath,
				TaskSlug:    slug,
				Prompt:      prompt,
				AgentCmd:    agentCmd,
			})
			if err != nil {
				return fmt.Errorf("dispatch failed: %w", err)
			}

			// 6. Atomically record in state
			preview := prompt
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}

			var worktreePath *string
			var branch *string
			if res.Extra != nil {
				if wp, ok := res.Extra["worktree_path"].(string); ok && wp != "" {
					worktreePath = &wp
				}
				if br, ok := res.Extra["branch"].(string); ok && br != "" {
					branch = &br
				}
			}

			err = state.MutateState(func(s *state.State) error {
				s.RecordTask(projName, projPath, res.SessionID, projName, state.TaskState{
					TaskSlug:      slug,
					Engine:        engineName,
					WindowID:      res.WindowID,
					WindowLabel:   slug,
					AgentBlockID:  res.BlockID,
					Agent:         agentName,
					Cwd:           projPath,
					WorktreePath:  worktreePath,
					Branch:        branch,
					Status:        "running",
					InitialPrompt: &prompt,
					LastSummary:   preview,
				})
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to record task in state: %w", err)
			}

			resp := map[string]any{
				"status":         "dispatched",
				"project":        projName,
				"task":           slug,
				"picked_up":      res.PickedUp,
				"preview":        preview,
				"window_id":      res.WindowID,
				"agent_block_id": res.BlockID,
				"agent":          agentName,
				"engine":         engineName,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or directory path")
	cmd.Flags().StringVarP(&prompt, "prompt", "m", "", "Task prompt to execute")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug (e.g. feat-auth)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent CLI to launch (agy, pi, opencode, or <provider>/<model>)")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3code)")
	cmd.Flags().StringVar(&workerBackend, "worker-backend", "", "Worker execution backend (alias for --engine)")

	return cmd
}
