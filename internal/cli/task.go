package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
		project string
		task    string
		agent   string
		prompt  string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new feature task tab in project session",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] task start: skeleton called with args %v\n", args)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent to run (agy, pi, opencode)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Initial prompt for worker agent")

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
