package cli

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

func newFocusCmd() *cobra.Command {
	var (
		project string
		task    string
		window  string
		session string
	)

	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Focus a project tab, task tab, or window in Rex",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, winID, _, _, _, engName, err := resolveTarget(project, task, session, window, "")
			if err != nil {
				return err
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			if err := eng.Focus(cmd.Context(), sessID, winID); err != nil {
				return err
			}

			resp := map[string]any{
				"status":    "focused",
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
