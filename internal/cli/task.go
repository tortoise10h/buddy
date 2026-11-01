package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("[buddy-ctl] task update: skeleton called with args %v\n", args)
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
			sessID, winID, _, projName, taskSlug, err := resolveTarget(project, task, session, window, "")
			if err != nil {
				return err
			}
			if sessID == "" || winID == "" {
				return fmt.Errorf("missing session_id or window_id for task '%s'", task)
			}

			eng := getDefaultEngine()
			if err := eng.Close(cmd.Context(), sessID, winID); err != nil {
				return err
			}

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
