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

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(newTaskStartCmd())
	cmd.AddCommand(newTaskUpdateCmd())
	cmd.AddCommand(newTaskCloseCmd())

	return cmd
}

func newTaskStartCmd() *cobra.Command {
	var (
		projectName string
		taskSlug    string
		agentName   string
		prompt      string
		engineName  string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new feature task tab in project session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("missing required --project (-p) argument")
			}
			if taskSlug == "" {
				if prompt != "" {
					taskSlug = project.DeriveSlug("", prompt, projectName)
				} else {
					return fmt.Errorf("missing required --task (-t) argument")
				}
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, err := state.LoadState()
			if err != nil {
				return err
			}

			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				return err
			}

			slug := project.DeriveSlug(taskSlug, prompt, projName)

			resEngine := engine.ResolveEngine(engineName, os.Getenv, cfg)
			resEngine.WarnUnknownHost(os.Stderr)
			engineName = resEngine.EngineName
			eng, err := getEngine(engineName)
			if err != nil {
				return err
			}

			if agentName == "" {
				agentName = cfg.GetEffectiveWorkerAgent()
			}
			agentCmd, _, _ := cfg.GetAgentCommand(agentName)

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

			preview := "Started via CLI"
			if prompt != "" {
				preview = prompt
				if len(preview) > 60 {
					preview = preview[:60] + "..."
				}
			}

			var initialPrompt *string
			if prompt != "" {
				initialPrompt = &prompt
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
					Status:        "running",
					InitialPrompt: initialPrompt,
					LastSummary:   preview,
				})
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to record task in state: %w", err)
			}

			resp := map[string]any{
				"status":         "started",
				"project":        projName,
				"task":           slug,
				"picked_up":      res.PickedUp,
				"window_id":      res.WindowID,
				"agent_block_id": res.BlockID,
				"agent":          agentName,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or path")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug (e.g. feat-auth)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent CLI to launch")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Initial prompt for worker agent")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, wezterm)")

	return cmd
}

func newTaskUpdateCmd() *cobra.Command {
	var (
		project string
		task    string
		status  string
		summary string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update task status or summary in state cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" || task == "" {
				return fmt.Errorf("both --project (-p) and --task (-t) are required")
			}

			var updatedProj, updatedTask string
			err := state.MutateState(func(s *state.State) error {
				p, err := s.FindProject(project)
				if err != nil {
					return err
				}
				updatedProj = p.Name

				t, err := p.FindTask(task)
				if err != nil {
					return err
				}
				updatedTask = t.TaskSlug

				if status != "" {
					t.Status = status
				}
				if summary != "" {
					t.LastSummary = summary
				}

				p.Tasks[t.TaskSlug] = *t
				s.Projects[p.Name] = *p
				return nil
			})
			if err != nil {
				return err
			}

			resp := map[string]any{
				"status":  "ok",
				"project": updatedProj,
				"task":    updatedTask,
				"updated": map[string]any{
					"status":  status,
					"summary": summary,
				},
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&status, "status", "", "New task status")
	cmd.Flags().StringVar(&summary, "summary", "", "New task summary")

	return cmd
}

func newTaskCloseCmd() *cobra.Command {
	var (
		project string
		task    string
		window  string
		session string
	)

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close task tab in Rex and update state",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, winID, _, projName, taskSlug, engName, err := resolveTarget(project, task, session, window, "")
			if err != nil {
				return err
			}
			if sessID == "" || winID == "" {
				return fmt.Errorf("missing session_id or window_id for task '%s'", task)
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			if err := eng.Close(cmd.Context(), sessID, winID); err != nil {
				return err
			}

			_ = state.MutateState(func(s *state.State) error {
				return s.UpdateTask(projName, taskSlug, "closed", "Task closed via CLI")
			})

			resp := map[string]any{
				"status":    "closed",
				"project":   projName,
				"task":      taskSlug,
				"window_id": winID,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&window, "window", "w", "", "Direct window ID")
	cmd.Flags().StringVarP(&session, "session", "s", "", "Direct session ID")

	return cmd
}
