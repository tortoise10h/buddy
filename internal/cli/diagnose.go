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

func newDiagnoseCmd() *cobra.Command {
	var (
		flagProject string
		flagTask    string
		flagLines   int
	)

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Diagnostician helper: Analyzes full terminal log for errors and stack traces",
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

			limit := flagLines
			if limit <= 0 {
				limit = 300
			}

			capRes, err := rexBackend.Capture(ctx, sessID, blockID, 0)
			if err != nil {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Failed to capture pane output",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rawLines := splitLines(capRes.Text)
			if len(rawLines) > limit {
				rawLines = rawLines[len(rawLines)-limit:]
			}

			errorIndicators := []string{
				"error", "failed", "exception", "traceback", "fatal", "panic", "cannot find", "not found",
				"syntaxerror", "typeerror", "referenceerror", "assertionerror",
			}

			type Snippet struct {
				Line    int    `json:"line"`
				Text    string `json:"text"`
				Context string `json:"context"`
			}

			var extractedSnippets []Snippet
			for idx, line := range rawLines {
				lineLower := strings.ToLower(line)
				for _, ind := range errorIndicators {
					if strings.Contains(lineLower, ind) {
						cStart := idx - 3
						if cStart < 0 {
							cStart = 0
						}
						cEnd := idx + 4
						if cEnd > len(rawLines) {
							cEnd = len(rawLines)
						}
						snippetCtx := strings.Join(rawLines[cStart:cEnd], "\n")
						extractedSnippets = append(extractedSnippets, Snippet{
							Line:    idx + 1,
							Text:    strings.TrimSpace(line),
							Context: snippetCtx,
						})
						break
					}
				}
			}

			uniqueSnippets := make([]Snippet, 0)
			seenTexts := make(map[string]bool)
			for _, s := range extractedSnippets {
				if !seenTexts[s.Text] {
					seenTexts[s.Text] = true
					uniqueSnippets = append(uniqueSnippets, s)
					if len(uniqueSnippets) >= 6 {
						break
					}
				}
			}

			tailLines := rawLines
			if len(rawLines) >= 30 {
				tailLines = rawLines[len(rawLines)-30:]
			}
			if tailLines == nil {
				tailLines = []string{}
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"project":              projectName,
				"task":                 taskSlug,
				"total_lines_analyzed": len(rawLines),
				"has_errors":           len(uniqueSnippets) > 0,
				"error_snippets_count": len(uniqueSnippets),
				"errors":               uniqueSnippets,
				"tail_lines":           tailLines,
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
	cmd.Flags().IntVarP(&flagLines, "lines", "n", 300, "Number of trailing lines to inspect (default: 300)")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}
