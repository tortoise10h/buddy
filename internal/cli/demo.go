package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newDemoCmd() *cobra.Command {
	var (
		project   string
		task      string
		cmdStr    string
		focusOnly bool
	)

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Focus task tab in Rex and optionally launch dev server split",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, winID, blkID, projName, taskSlug, engName, err := resolveTarget(project, task, "", "", "")
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
			if err := eng.Focus(cmd.Context(), sessID, winID); err != nil {
				return err
			}

			var devBlockID *string
			if cmdStr != "" && !focusOnly && blkID != "" {
				splitID, err := eng.OpenDevSplit(cmd.Context(), sessID, blkID, "", cmdStr)
				if err != nil {
					return fmt.Errorf("failed to open dev split: %w", err)
				}
				devBlockID = &splitID
			}

			resp := map[string]any{
				"status":    "ok",
				"project":   projName,
				"task":      taskSlug,
				"focused":   true,
				"dev_split": devBlockID,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "Command to run in dev split pane (e.g. 'npm run dev')")
	cmd.Flags().BoolVar(&focusOnly, "focus-only", false, "Only switch focus without launching dev split")

	return cmd
}
