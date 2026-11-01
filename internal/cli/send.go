package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newSendCmd() *cobra.Command {
	var (
		flagProject string
		flagTask    string
		flagText    string
		flagNoEnter bool
		flagKey     string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send text or keys to worker agent pane",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagTask == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --task argument")}
			}

			cfg := config.LoadConfig()
			projectName, _ := project.ResolveProject(flagProject, &cfg)
			taskSlug := strings.TrimSpace(flagTask)

			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			projVal, ok := rawProjects[projectName]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Project '%s' not found", projectName),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			projMap, _ := projVal.(map[string]any)
			projTasks, _ := projMap["tasks"].(map[string]any)
			taskVal, ok := projTasks[taskSlug]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Task '%s' not found", taskSlug),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			taskMap, _ := taskVal.(map[string]any)
			sessID, _ := projMap["rex_session_id"].(string)
			if sessID == "" {
				sessID, _ = projMap["session_id"].(string)
			}
			blockID, _ := taskMap["agent_block_id"].(string)

			if sessID == "" || blockID == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Missing session_id or block_id",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			if cmd.Flags().Changed("text") {
				if err := rexBackend.SendText(ctx, sessID, blockID, flagText); err != nil {
					return err
				}
				if !flagNoEnter {
					_ = rexBackend.SendKey(ctx, sessID, blockID, "enter")
				}
			}

			if flagKey != "" {
				if err := rexBackend.SendKey(ctx, sessID, blockID, flagKey); err != nil {
					return err
				}
			}

			taskMap["updated_at"] = state.NowISO()
			_ = state.SaveRawState(rawState)

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":  "sent",
				"project": projectName,
				"task":    taskSlug,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&flagText, "text", "", "Text to send to the pane")
	cmd.Flags().BoolVar(&flagNoEnter, "no-enter", false, "Do not press enter after typing text")
	cmd.Flags().StringVar(&flagKey, "key", "", "Key or shortcut to send (e.g. enter, ctrl+c, esc)")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}
