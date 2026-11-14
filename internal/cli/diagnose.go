package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var errorIndicators = []string{
	"error", "failed", "exception", "traceback", "fatal", "panic:", "cannot find", "not found",
	"syntaxerror", "typeerror", "referenceerror", "assertionerror",
}

func newDiagnoseCmd() *cobra.Command {
	var (
		project    string
		task       string
		lines      int
		engineName string
	)

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnostician helper: Analyzes full terminal log for errors and stack traces",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, _, blkID, projName, taskSlug, engName, err := resolveTargetWithEngine(project, task, "", "", "", engineName)
			if err != nil {
				return err
			}
			if sessID == "" || blkID == "" {
				return fmt.Errorf("missing session_id or block_id for task '%s'", task)
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}

			if lines <= 0 {
				lines = 300
			}

			out, err := eng.Capture(cmd.Context(), sessID, blkID, lines)
			if err != nil {
				return fmt.Errorf("failed to capture terminal output: %w", err)
			}

			rawLines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
			var snippets []string
			seenLines := make(map[int]bool)

			for i, line := range rawLines {
				lower := strings.ToLower(line)
				hasError := false
				for _, ind := range errorIndicators {
					if strings.Contains(lower, ind) {
						hasError = true
						break
					}
				}
				if hasError {
					start := i - 2
					if start < 0 {
						start = 0
					}
					end := i + 3
					if end > len(rawLines) {
						end = len(rawLines)
					}

					var snippetLines []string
					for j := start; j < end; j++ {
						if !seenLines[j] {
							seenLines[j] = true
							snippetLines = append(snippetLines, rawLines[j])
						}
					}
					if len(snippetLines) > 0 {
						snippets = append(snippets, strings.Join(snippetLines, "\n"))
					}
				}
			}

			resp := map[string]any{
				"status":         "ok",
				"project":        projName,
				"task":           taskSlug,
				"lines_analyzed": len(rawLines),
				"snippets_found": len(snippets),
				"snippets":       snippets,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().IntVarP(&lines, "lines", "n", 300, "Number of trailing lines to analyze")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, wezterm)")

	return cmd
}
