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

func newCaptureCmd() *cobra.Command {
	var (
		flagProject string
		flagTask    string
		flagLines   int
		flagRaw     bool
	)

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture output from worker agent pane in O(1)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagTask == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --task argument")}
			}

			cfg := config.LoadConfig()
			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			projectName, _ := project.ResolveProject(flagProject, &cfg)
			taskSlug := strings.TrimSpace(flagTask)

			rawProjects, _ := rawState["projects"].(map[string]any)
			projVal, ok := rawProjects[projectName]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Project '%s' not found in state", projectName),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			projData, _ := projVal.(map[string]any)
			sessID, _ := projData["rex_session_id"].(string)
			if sessID == "" {
				sessID, _ = projData["session_id"].(string)
			}

			tasksMap, _ := projData["tasks"].(map[string]any)
			taskVal, ok := tasksMap[taskSlug]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Task '%s' not found in project '%s'", taskSlug, projectName),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			taskData, _ := taskVal.(map[string]any)
			blockID, _ := taskData["agent_block_id"].(string)
			if sessID == "" || blockID == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Missing session_id or agent_block_id for task",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			capRes, err := rexBackend.Capture(ctx, sessID, blockID, 0)
			if err != nil {
				// Self-healing: verify window and session
				windows, winErr := rexBackend.ListWindows(ctx, sessID)
				if winErr != nil {
					taskData["status"] = "missing"
					taskData["last_summary"] = "Rex session not found"
					_ = state.SaveRawState(rawState)
					outJSON, _ := formatters.FormatJSON(map[string]string{
						"status": "missing",
						"error":  "Rex session not found",
						"output": "",
					})
					fmt.Println(outJSON)
					return nil
				}

				winExists := false
				targetWinID, _ := taskData["window_id"].(string)
				for _, w := range windows {
					if w.WindowID == targetWinID {
						winExists = true
						break
					}
				}

				if !winExists {
					taskData["status"] = "missing"
					taskData["last_summary"] = "Task tab was closed in Rex"
					_ = state.SaveRawState(rawState)
					outJSON, _ := formatters.FormatJSON(map[string]string{
						"status": "missing",
						"error":  "Task tab closed in Rex",
						"output": "",
					})
					fmt.Println(outJSON)
					return nil
				}
			}

			rawOutput := capRes.Text
			lines := splitLines(rawOutput)
			limit := flagLines
			if limit <= 0 {
				limit = cfg.CaptureLines
			}
			if limit <= 0 {
				limit = 120
			}
			if len(lines) > limit {
				lines = lines[len(lines)-limit:]
			}
			finalOutput := strings.Join(lines, "\n")

			if flagRaw {
				fmt.Println(finalOutput)
				return nil
			}

			taskStatus, _ := taskData["status"].(string)
			taskSummary, _ := taskData["last_summary"].(string)
			if taskSummary == "" {
				taskSummary, _ = taskData["summary"].(string)
			}

			type CaptureJSONOutput struct {
				Project     string `json:"project"`
				Task        string `json:"task"`
				Status      string `json:"status"`
				LastSummary string `json:"last_summary"`
				LinesCount  int    `json:"lines_count"`
				Output      string `json:"output"`
			}

			outObj := CaptureJSONOutput{
				Project:     projectName,
				Task:        taskSlug,
				Status:      taskStatus,
				LastSummary: taskSummary,
				LinesCount:  len(lines),
				Output:      finalOutput,
			}

			outJSON, err := formatters.FormatJSON(outObj)
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug")
	cmd.Flags().IntVarP(&flagLines, "lines", "n", 0, "Number of trailing lines to capture (default: config.capture_lines)")
	cmd.Flags().BoolVar(&flagRaw, "raw", false, "Print raw terminal output directly")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	trimmed := s
	if strings.HasSuffix(trimmed, "\r\n") {
		trimmed = strings.TrimSuffix(trimmed, "\r\n")
	} else if strings.HasSuffix(trimmed, "\n") {
		trimmed = strings.TrimSuffix(trimmed, "\n")
	}
	return strings.Split(trimmed, "\n")
}
