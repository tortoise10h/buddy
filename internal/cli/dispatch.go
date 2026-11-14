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
		projectName string
		taskSlug    string
		prompt      string
		agentName   string
		engineName  string
		force       bool
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

			// 1. Resolve engine
			resEngine := engine.ResolveEngine(engineName, os.Getenv, cfg)
			resEngine.WarnUnknownHost(os.Stderr)
			engineName = resEngine.EngineName
			eng, err := getEngine(engineName)
			if err != nil {
				return err
			}

			if engineName == "rex" {
				reconcileRexIfStale(cmd.Context(), defaultSyncTTL)
			}

			st, err := state.LoadStateForEngine(engineName)
			if err != nil {
				return err
			}

			// 2. Resolve project
			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				return err
			}

			// 3. Resolve task slug
			slug := project.DeriveSlug(taskSlug, prompt, projName)

			// 4. Candidate collision guard (unless --force is passed)
			if !force {
				if candidates := findCandidateCollisions(st, projName, slug, prompt); len(candidates) > 0 {
					return formatCandidateCollisionError(projName, slug, candidates)
				}
			}

			// 5. Resolve worker agent / model for target engine
			agentName, agentCmd, err := resolveWorkerForEngine(engineName, agentName, cfg)
			if err != nil {
				return err
			}

			// 6. Dispatch via engine
			res, err := eng.Dispatch(cmd.Context(), engine.DispatchRequest{
				ProjectName: projName,
				ProjectPath: projPath,
				TaskSlug:    slug,
				Prompt:      prompt,
				AgentCmd:    agentCmd,
			})
			if err != nil {
				return err
			}

			// 7. Atomically record in engine-specific state
			preview := prompt
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}

			var updatedState *state.State
			err = state.MutateStateForEngine(engineName, func(s *state.State) error {
				s.RecordTask(projName, projPath, res.SessionID, projName, state.TaskState{
					TaskSlug:      slug,
					Engine:        engineName,
					WindowID:      res.WindowID,
					WindowLabel:   slug,
					AgentBlockID:  res.BlockID,
					Agent:         agentName,
					Cwd:           projPath,
					Status:        "running",
					Origin:        "buddy",
					InitialPrompt: &prompt,
					LastSummary:   preview,
				})
				updatedState = s
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to record task in state: %w", err)
			}

			recWatch := computeRecommendedWatch(updatedState)
			activeInProj := collectActiveTasksInProject(updatedState, projName)

			resp := map[string]any{
				"status":                  "dispatched",
				"project":                 projName,
				"task":                    slug,
				"picked_up":               res.PickedUp,
				"preview":                 preview,
				"window_id":               res.WindowID,
				"agent_block_id":          res.BlockID,
				"agent":                   agentName,
				"active_tasks_in_project": activeInProj,
				"recommended_watch":       recWatch,
			}
			if res.PickupReason != "" {
				resp["pickup_reason"] = res.PickupReason
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or directory path")
	cmd.Flags().StringVarP(&prompt, "prompt", "m", "", "Task prompt to execute")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug (e.g. feat-auth)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent CLI to launch (agy, pi, opencode, etc.)")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, wezterm)")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass candidate duplicate guard and start a new task tab")

	return cmd
}
