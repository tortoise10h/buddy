package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

var (
	inputRegex = regexp.MustCompile(`(?i)(?:\?|\[\?\])\s+.*|\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\)|(?:select|choose)\s+(?:an?\s+)?(?:option|action|mode):?|proceed\s*\?|(?:clarification|approval)\s+needed|what\s+would\s+you\s+like|do\s+you\s+want\s+to|would\s+you\s+prefer|please\s+confirm|press\s+enter\s+to\s+continue`)
	demoRegex  = regexp.MustCompile(`(?i)task\s+complete|implementation\s+complete|all\s+tests\s+pass|ready\s+for\s+(?:review|demo)|successfully\s+(?:implemented|built|completed)|done\s*!?|finished\s+implementing`)
	errorRegex = regexp.MustCompile(`(?i)traceback\s+\(most\s+recent\s+call\s+last\)|fatal\s+error|build\s+failed|npm\s+err!|panic:\s+`)
)

func newWatchCmd() *cobra.Command {
	var (
		flagProject  string
		flagTask     string
		flagAll      bool
		flagInterval int
		flagTimeout  int
		flagUntil    string
		flagNotify   bool
		flagNoNotify bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watchdog monitor: Polls worker agent pane(s) until input needed, finished, or error",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadConfig()
			interval := flagInterval
			if interval <= 0 {
				interval = 5
			}
			timeout := flagTimeout
			if timeout <= 0 {
				timeout = 600
			}

			untilStr := strings.ToLower(flagUntil)
			if untilStr == "" {
				untilStr = "input,demo,done,error,missing"
			}
			untilParts := strings.Split(untilStr, ",")
			untilEvents := make(map[string]bool)
			for _, p := range untilParts {
				untilEvents[strings.TrimSpace(p)] = true
			}

			notifyEnabled := true
			if flagNoNotify {
				notifyEnabled = false
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()
			startTime := time.Now()

			if !flagAll {
				if flagProject == "" || flagTask == "" {
					errJSON, _ := formatters.FormatJSON(map[string]string{
						"error": "Provide --project and --task, or use --all to monitor all active tasks",
					})
					fmt.Fprintln(os.Stderr, errJSON)
					return &ExitError{Code: 1}
				}

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

				for {
					elapsed := int(time.Since(startTime).Seconds())
					if elapsed > timeout {
						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "timeout",
							"project":         projectName,
							"task":            taskSlug,
							"summary":         fmt.Sprintf("Watch timed out after %ds", elapsed),
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					capRes, capErr := rexBackend.Capture(ctx, sessID, blockID, 0)
					if capErr != nil {
						// Check if window or session is missing
						windows, winErr := rexBackend.ListWindows(ctx, sessID)
						if winErr != nil {
							taskMap["status"] = "missing"
							taskMap["last_summary"] = "Rex session not found"
							taskMap["updated_at"] = state.NowISO()
							_ = state.SaveRawState(rawState)
							if untilEvents["missing"] || untilEvents["any"] {
								outJSON, _ := formatters.FormatJSON(map[string]any{
									"event":   "missing",
									"project": projectName,
									"task":    taskSlug,
									"summary": "Rex session not found",
								})
								fmt.Println(outJSON)
								return nil
							}
						} else {
							wID, _ := taskMap["window_id"].(string)
							foundWin := false
							for _, w := range windows {
								if w.WindowID == wID {
									foundWin = true
									break
								}
							}
							if !foundWin {
								taskMap["status"] = "missing"
								taskMap["last_summary"] = "Task tab closed in Rex"
								taskMap["updated_at"] = state.NowISO()
								_ = state.SaveRawState(rawState)
								if untilEvents["missing"] || untilEvents["any"] {
									outJSON, _ := formatters.FormatJSON(map[string]any{
										"event":   "missing",
										"project": projectName,
										"task":    taskSlug,
										"summary": "Task tab closed in Rex",
									})
									fmt.Println(outJSON)
									return nil
								}
							}
						}
					}

					text := capRes.Text
					rawLines := splitLines(text)
					var cleanLines []string
					for _, l := range rawLines {
						t := strings.TrimSpace(l)
						if t != "" {
							cleanLines = append(cleanLines, t)
						}
					}
					tailLines := cleanLines
					if len(cleanLines) >= 20 {
						tailLines = cleanLines[len(cleanLines)-20:]
					}
					tailText := strings.Join(tailLines, "\n")

					// 1. Check for input needed
					if (untilEvents["input"] || untilEvents["any"]) && inputRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if inputRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker agent is waiting for input or confirmation"
						}
						taskMap["status"] = "waiting_for_input"
						taskMap["last_summary"] = summary
						taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						if notifyEnabled {
							notify.SendDesktopNotification(
								"Buddy Orchestrator",
								fmt.Sprintf("%s/%s (Input Needed)", projectName, taskSlug),
								summary,
								"Glass",
								&cfg,
							)
						}

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "waiting_for_input",
							"project":         projectName,
							"task":            taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					// 2. Check for demo / done
					if (untilEvents["demo"] || untilEvents["done"] || untilEvents["any"]) && demoRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if demoRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker finished task; ready for demo"
						}
						taskMap["status"] = "ready_for_demo"
						taskMap["last_summary"] = summary
						taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						if notifyEnabled {
							notify.SendDesktopNotification(
								"Buddy Orchestrator",
								fmt.Sprintf("%s/%s (Ready for Demo)", projectName, taskSlug),
								summary,
								"Glass",
								&cfg,
							)
						}

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "ready_for_demo",
							"project":         projectName,
							"task":            taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					// 3. Check for fatal error
					if (untilEvents["error"] || untilEvents["any"]) && errorRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if errorRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker agent encountered an error"
						}
						taskMap["status"] = "error"
						taskMap["last_summary"] = summary
						taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "error",
							"project":         projectName,
							"task":            taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					time.Sleep(time.Duration(interval) * time.Second)
				}
			}

			// Watch all mode
			emptyStreak := 0
			for {
				elapsed := int(time.Since(startTime).Seconds())
				if elapsed > timeout {
					outJSON, _ := formatters.FormatJSON(map[string]any{
						"event":           "timeout",
						"summary":         fmt.Sprintf("Watch --all timed out after %ds", elapsed),
						"elapsed_seconds": elapsed,
					})
					fmt.Println(outJSON)
					return nil
				}

				rawState, err := state.LoadRawState()
				if err != nil {
					time.Sleep(time.Duration(interval) * time.Second)
					continue
				}

				rawProjects, _ := rawState["projects"].(map[string]any)
				type candidate struct {
					projectName string
					sessID      string
					taskSlug    string
					taskMap     map[string]any
				}

				var candidates []candidate
				for pName, pVal := range rawProjects {
					pMap, ok := pVal.(map[string]any)
					if !ok {
						continue
					}
					sID, _ := pMap["rex_session_id"].(string)
					if sID == "" {
						sID, _ = pMap["session_id"].(string)
					}
					tasks, _ := pMap["tasks"].(map[string]any)
					for tSlug, tVal := range tasks {
						tMap, ok := tVal.(map[string]any)
						if !ok {
							continue
						}
						st, _ := tMap["status"].(string)
						if st == "starting" || st == "planning" || st == "running" {
							candidates = append(candidates, candidate{
								projectName: pName,
								sessID:      sID,
								taskSlug:    tSlug,
								taskMap:     tMap,
							})
						}
					}
				}

				if len(candidates) == 0 {
					emptyStreak++
					if emptyStreak > 3 {
						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "idle",
							"summary":         "No tasks currently running or planning across all projects",
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}
					time.Sleep(time.Duration(interval) * time.Second)
					continue
				}

				emptyStreak = 0
				for _, cand := range candidates {
					bID, _ := cand.taskMap["agent_block_id"].(string)
					if cand.sessID == "" || bID == "" {
						continue
					}

					capRes, capErr := rexBackend.Capture(ctx, cand.sessID, bID, 0)
					if capErr != nil {
						continue
					}

					rawLines := splitLines(capRes.Text)
					var cleanLines []string
					for _, l := range rawLines {
						t := strings.TrimSpace(l)
						if t != "" {
							cleanLines = append(cleanLines, t)
						}
					}
					tailLines := cleanLines
					if len(cleanLines) >= 20 {
						tailLines = cleanLines[len(cleanLines)-20:]
					}
					tailText := strings.Join(tailLines, "\n")

					if (untilEvents["input"] || untilEvents["any"]) && inputRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if inputRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker agent is waiting for input or confirmation"
						}
						cand.taskMap["status"] = "waiting_for_input"
						cand.taskMap["last_summary"] = summary
						cand.taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						if notifyEnabled {
							notify.SendDesktopNotification(
								"Buddy Orchestrator",
								fmt.Sprintf("%s/%s (Input Needed)", cand.projectName, cand.taskSlug),
								summary,
								"Glass",
								&cfg,
							)
						}

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "waiting_for_input",
							"project":         cand.projectName,
							"task":            cand.taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					if (untilEvents["demo"] || untilEvents["done"] || untilEvents["any"]) && demoRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if demoRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker finished task; ready for demo"
						}
						cand.taskMap["status"] = "ready_for_demo"
						cand.taskMap["last_summary"] = summary
						cand.taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						if notifyEnabled {
							notify.SendDesktopNotification(
								"Buddy Orchestrator",
								fmt.Sprintf("%s/%s (Ready for Demo)", cand.projectName, cand.taskSlug),
								summary,
								"Glass",
								&cfg,
							)
						}

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "ready_for_demo",
							"project":         cand.projectName,
							"task":            cand.taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}

					if (untilEvents["error"] || untilEvents["any"]) && errorRegex.MatchString(tailText) {
						matchingLine := ""
						for i := len(tailLines) - 1; i >= 0; i-- {
							if errorRegex.MatchString(tailLines[i]) {
								matchingLine = tailLines[i]
								break
							}
						}
						summary := matchingLine
						if summary == "" {
							summary = "Worker agent encountered an error"
						}
						cand.taskMap["status"] = "error"
						cand.taskMap["last_summary"] = summary
						cand.taskMap["updated_at"] = state.NowISO()
						_ = state.SaveRawState(rawState)

						outJSON, _ := formatters.FormatJSON(map[string]any{
							"event":           "error",
							"project":         cand.projectName,
							"task":            cand.taskSlug,
							"summary":         summary,
							"last_output":     tailText,
							"elapsed_seconds": elapsed,
						})
						fmt.Println(outJSON)
						return nil
					}
				}

				time.Sleep(time.Duration(interval) * time.Second)
			}
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name (required unless --all is specified)")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug (required unless --all is specified)")
	cmd.Flags().BoolVarP(&flagAll, "all", "a", false, "Watch all active tasks across all projects simultaneously")
	cmd.Flags().IntVarP(&flagInterval, "interval", "i", 5, "Polling interval in seconds (default: 5)")
	cmd.Flags().IntVarP(&flagTimeout, "timeout", "T", 600, "Max watch duration in seconds (default: 600)")
	cmd.Flags().StringVarP(&flagUntil, "until", "u", "input,demo,done,error,missing", "Comma-separated events to watch for: input,demo,done,error,missing,any")
	cmd.Flags().BoolVar(&flagNotify, "notify", true, "Send desktop notification on event (default: true)")
	cmd.Flags().BoolVar(&flagNoNotify, "no-notify", false, "Disable desktop notification")

	return cmd
}
