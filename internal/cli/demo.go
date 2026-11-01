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

func newDemoCmd() *cobra.Command {
	var (
		flagProject   string
		flagTask      string
		flagCmd       string
		flagFocusOnly bool
	)

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Focus task tab in Rex and optionally launch dev server split",
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
			winID, _ := taskMap["window_id"].(string)
			agentBlockID, _ := taskMap["agent_block_id"].(string)

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			// If --cmd is provided and dev split not running, create split
			if flagCmd != "" && agentBlockID != "" && !flagFocusOnly {
				devBlockID, _ := taskMap["dev_block_id"].(string)
				needsSplit := true
				if devBlockID != "" {
					_, errChk := rexBackend.InspectBlock(ctx, devBlockID)
					if errChk == nil {
						needsSplit = false
					}
				}

				if needsSplit {
					cwd, _ := taskMap["cwd"].(string)
					if cwd == "" {
						cwd, _ = projMap["project_path"].(string)
					}
					splitBlock, err := rexBackend.OpenDevSplit(ctx, sessID, agentBlockID, cwd, flagCmd)
					if err == nil && splitBlock.BlockID != "" {
						taskMap["dev_block_id"] = splitBlock.BlockID
						_ = state.SaveRawState(rawState)
					}
				}
			}

			_ = rexBackend.FocusRexWindow(ctx, sessID, winID)

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":       "focused",
				"project":      projectName,
				"task":         taskSlug,
				"dev_block_id": taskMap["dev_block_id"],
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
	cmd.Flags().StringVar(&flagCmd, "cmd", "", "Command to run in dev split pane (e.g. 'npm run dev')")
	cmd.Flags().BoolVar(&flagFocusOnly, "focus-only", false, "Only switch focus without launching dev split")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}
