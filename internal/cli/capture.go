package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCaptureCmd() *cobra.Command {
	var (
		project    string
		task       string
		session    string
		block      string
		lines      int
		raw        bool
		engineName string
	)

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture output from worker agent pane in O(1)",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, _, blkID, projName, taskSlug, engName, err := resolveTargetWithEngine(project, task, session, "", block, engineName)
			if err != nil {
				return err
			}
			if sessID == "" || blkID == "" {
				return fmt.Errorf("missing session_id or agent_block_id (specify --project/--task or -s/-b)")
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			out, err := eng.Capture(cmd.Context(), sessID, blkID, lines)
			if err != nil {
				return err
			}

			if raw {
				fmt.Print(out)
				return nil
			}

			trimmed := strings.TrimRight(out, "\r\n")
			var resultLines []string
			if trimmed != "" {
				resultLines = strings.Split(trimmed, "\n")
			} else {
				resultLines = []string{}
			}

			resp := map[string]any{
				"status":   "ok",
				"project":  projName,
				"task":     taskSlug,
				"lines":    resultLines,
				"text":     out,
				"is_alive": true,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&session, "session", "s", "", "Direct session ID")
	cmd.Flags().StringVarP(&block, "block", "b", "", "Direct block ID")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of trailing lines to capture")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print raw terminal output directly")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, wezterm)")

	return cmd
}
