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

func newFocusCmd() *cobra.Command {
	var (
		flagProject string
		flagTask    string
		flagWindow  string
		flagSession string
	)

	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Focus a project tab, task tab, or window in Rex",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadConfig()
			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			var (
				projName *string
				sessID   string
				winID    string
			)

			if flagProject != "" {
				name, _ := project.ResolveProject(flagProject, &cfg)
				projName = &name

				rawProjects, _ := rawState["projects"].(map[string]any)
				if projVal, exists := rawProjects[name]; exists {
					if projMap, ok := projVal.(map[string]any); ok {
						s, _ := projMap["rex_session_id"].(string)
						if s == "" {
							s, _ = projMap["session_id"].(string)
						}
						sessID = s

						if flagTask != "" {
							tasks, _ := projMap["tasks"].(map[string]any)
							if taskVal, ok := tasks[strings.TrimSpace(flagTask)]; ok {
								if taskMap, ok := taskVal.(map[string]any); ok {
									winID, _ = taskMap["window_id"].(string)
								}
							} else {
								errJSON, _ := formatters.FormatJSON(map[string]string{
									"error": fmt.Sprintf("Task '%s' not found in project '%s'", flagTask, name),
								})
								fmt.Fprintln(os.Stderr, errJSON)
								return &ExitError{Code: 1}
							}
						}
					}
				}
			} else if flagWindow != "" {
				winID = flagWindow
				sessID = flagSession
			}

			if sessID == "" && winID == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Must specify --project (and optionally --task), or --window",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			_ = rexBackend.FocusRexWindow(ctx, sessID, winID)

			var taskOut *string
			if flagTask != "" {
				t := strings.TrimSpace(flagTask)
				taskOut = &t
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":         "focused",
				"project":        projName,
				"task":           taskOut,
				"rex_session_id": sessID,
				"window_id":      winID,
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
	cmd.Flags().StringVarP(&flagWindow, "window", "w", "", "Direct Rex window ID or label")
	cmd.Flags().StringVarP(&flagSession, "session", "s", "", "Rex session ID (used with --window)")

	return cmd
}
