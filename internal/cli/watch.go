package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/state"
)

var (
	watchInputRe = regexp.MustCompile(`(?i)(\?|\[\?\])\s+|\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\)|(select|choose)\s+(an?\s+)?(option|action|mode):?|proceed\s*\?|(clarification|approval)\s+needed|what\s+would\s+you\s+like|do\s+you\s+want\s+to|would\s+you\s+prefer|please\s+confirm|press\s+enter\s+to\s+continue`)
	watchDemoRe  = regexp.MustCompile(`(?i)task\s+complete|implementation\s+complete|all\s+tests\s+pass|ready\s+for\s+(review|demo)|successfully\s+(implemented|built|completed)|done\s*!?|finished\s+implementing`)
	watchErrorRe = regexp.MustCompile(`(?i)traceback\s+\(most\s+recent\s+call\s+last\)|fatal\s+error|build\s+failed|npm\s+err!|panic:\s+`)
)

func newWatchCmd() *cobra.Command {
	var (
		projectName   string
		taskSlug      string
		watchAll      bool
		intervalSec   int
		timeoutSec    int
		untilEvents   string
		notifyEnabled bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watchdog monitor: Polls worker agent pane(s) until input needed, finished, or error",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !watchAll && (projectName == "" || taskSlug == "") {
				return fmt.Errorf("provide --project and --task, or use --all to monitor all active tasks")
			}

			if intervalSec <= 0 {
				intervalSec = 5
			}
			if timeoutSec <= 0 {
				timeoutSec = 600
			}

			untilSet := make(map[string]bool)
			for _, evt := range strings.Split(strings.ToLower(untilEvents), ",") {
				untilSet[strings.TrimSpace(evt)] = true
			}

			startTime := time.Now()
			deadline := startTime.Add(time.Duration(timeoutSec) * time.Second)

			for time.Now().Before(deadline) {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				default:
				}

				st, err := state.LoadState()
				if err != nil {
					time.Sleep(time.Duration(intervalSec) * time.Second)
					continue
				}

				type targetTask struct {
					project   string
					task      string
					sessionID string
					blockID   string
					engine    string
				}

				var targets []targetTask
				if watchAll {
					for pName, pData := range st.Projects {
						for tName, tData := range pData.Tasks {
							if tData.Status == "running" && tData.AgentBlockID != "" {
								targets = append(targets, targetTask{
									project:   pName,
									task:      tName,
									sessionID: pData.RexSessionID,
									blockID:   tData.AgentBlockID,
									engine:    tData.GetEngine(),
								})
							}
						}
					}
				} else {
					p, err := st.FindProject(projectName)
					if err == nil {
						t, err := p.FindTask(taskSlug)
						if err == nil {
							targets = append(targets, targetTask{
								project:   p.Name,
								task:      t.TaskSlug,
								sessionID: p.RexSessionID,
								blockID:   t.AgentBlockID,
								engine:    t.GetEngine(),
							})
						}
					}
				}

				for _, target := range targets {
					eng, err := getEngine(target.engine)
					if err != nil {
						continue
					}

					out, err := eng.Capture(cmd.Context(), target.sessionID, target.blockID, 20)
					if err != nil {
						continue
					}

					var matchedEvent, summary string
					lines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")

					if (untilSet["input"] || untilSet["any"]) && watchInputRe.MatchString(out) {
						matchedEvent = "waiting_for_input"
						for i := len(lines) - 1; i >= 0; i-- {
							if watchInputRe.MatchString(lines[i]) {
								summary = strings.TrimSpace(lines[i])
								break
							}
						}
					} else if (untilSet["demo"] || untilSet["done"] || untilSet["any"]) && watchDemoRe.MatchString(out) {
						matchedEvent = "ready_for_demo"
						for i := len(lines) - 1; i >= 0; i-- {
							if watchDemoRe.MatchString(lines[i]) {
								summary = strings.TrimSpace(lines[i])
								break
							}
						}
					} else if (untilSet["error"] || untilSet["any"]) && watchErrorRe.MatchString(out) {
						matchedEvent = "error"
						for i := len(lines) - 1; i >= 0; i-- {
							if watchErrorRe.MatchString(lines[i]) {
								summary = strings.TrimSpace(lines[i])
								break
							}
						}
					}

					if matchedEvent != "" {
						_ = state.MutateState(func(s *state.State) error {
							return s.UpdateTask(target.project, target.task, matchedEvent, summary)
						})

						if notifyEnabled {
							notify.SendDesktopNotification("Buddy Watchdog", fmt.Sprintf("%s/%s (%s)", target.project, target.task, matchedEvent), summary, "Glass", true)
						}

						elapsed := int(time.Since(startTime).Seconds())
						resp := map[string]any{
							"event":           matchedEvent,
							"project":         target.project,
							"task":            target.task,
							"summary":         summary,
							"elapsed_seconds": elapsed,
						}
						enc := json.NewEncoder(cmd.OutOrStdout())
						enc.SetIndent("", "  ")
						return enc.Encode(resp)
					}
				}

				time.Sleep(time.Duration(intervalSec) * time.Second)
			}

			// Timed out
			elapsed := int(time.Since(startTime).Seconds())
			resp := map[string]any{
				"event":           "timeout",
				"summary":         fmt.Sprintf("Watch timed out after %ds", elapsed),
				"elapsed_seconds": elapsed,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug")
	cmd.Flags().BoolVar(&watchAll, "all", false, "Monitor all active tasks across projects")
	cmd.Flags().IntVar(&intervalSec, "interval", 5, "Polling interval in seconds")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 600, "Timeout in seconds")
	cmd.Flags().StringVar(&untilEvents, "until", "input,demo,done,error,missing", "Comma-separated events to wait for")
	cmd.Flags().BoolVar(&notifyEnabled, "notify", true, "Send desktop notification on trigger")

	return cmd
}
