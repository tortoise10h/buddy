package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/state"
)

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\].*?(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

	watchDemoLineRe  = regexp.MustCompile(`(?i)^(?:(?:\[?[✓✔xX☑]\]?|[✅•●*-])\s*)?(?:task\s+complete[d]?(?::\s*.+)?|implementation\s+complete[d]?(?::\s*.+)?|all\s+tests\s+pass(?:ed)?(?::\s*.+)?|ready\s+for\s+(?:review|demo)|successfully\s+(?:implemented|built|completed)(?::\s*.+)?|finished\s+implementing|done)[.!]?$`)
	watchErrorLineRe = regexp.MustCompile(`(?i)^(?:panic:\s+|fatal(?:\s+error)?:|traceback\s+\(most\s+recent\s+call\s+last\):?|npm\s+err!|build\s+failed)`)

	inputStartPromptRe = regexp.MustCompile(`^(?:\[\?\]|\?)\s+`)
	inputQuestionRe    = regexp.MustCompile(`(?i)^(?:(?:\[\?\]|\?)\s+.*|\s*(?:do\s+you\s+want\s+to|would\s+you\s+(?:like|prefer)|what\s+would\s+you\s+like|which\s+(?:one|option)|please\s+confirm|press\s+enter\s+to\s+continue|(?:select|choose)\s+(?:an?\s+)?(?:option|action|mode):?|clarification\s+needed|approval\s+needed|proceed\s*\?).*|.*(?:\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\))\s*$)`)

	workingRe = regexp.MustCompile(`[\x{2800}-\x{28ff}⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒]|(?i)(?:running\s+command|thought\s+for|thinking\.\.\.|esc\s+to\s+cancel)`)

	// Legacy aliases kept for compatibility
	watchDemoRe  = watchDemoLineRe
	watchErrorRe = watchErrorLineRe
	watchInputRe = inputQuestionRe
)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func isBoxOrRuleRune(r rune) bool {
	// Unicode Box Drawing: U+2500 - U+257F
	// Unicode Block Elements: U+2580 - U+259F
	if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
		return true
	}
	switch r {
	case '-', '_', '=', '~', '*', '+', '|', '—', '–':
		return true
	default:
		return false
	}
}

func isRuleOrBorderLine(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			continue
		}
		if !isBoxOrRuleRune(r) {
			return false
		}
	}
	return true
}

func isPureGlyphs(s string) bool {
	hasGlyph := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		// Braille Patterns: U+2800 - U+28FF
		if (r >= 0x2800 && r <= 0x28FF) || strings.ContainsRune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒●○▪▫◆◇•*", r) {
			hasGlyph = true
			continue
		}
		return false
	}
	return hasGlyph
}

func isChromeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "? for shortcuts") ||
		strings.Contains(lower, "esc to cancel") ||
		strings.Contains(lower, "ctrl+o to expand") ||
		strings.HasPrefix(lower, "tip:") ||
		strings.Contains(lower, "tip: use /diff") ||
		strings.HasPrefix(lower, "auto ·") ||
		strings.HasPrefix(lower, "auto •") ||
		strings.Contains(lower, "auto ·") {
		return true
	}
	if isPureGlyphs(trimmed) {
		return true
	}
	return false
}

func paneLines(raw string) (filtered []string, working bool) {
	clean := stripANSI(raw)
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	rawLines := strings.Split(clean, "\n")

	var nonEmpty []string
	for _, l := range rawLines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}

	// Check tail (last up to 6 non-empty lines) for active working state
	tailLen := 6
	if len(nonEmpty) < tailLen {
		tailLen = len(nonEmpty)
	}
	for i := len(nonEmpty) - tailLen; i < len(nonEmpty); i++ {
		if workingRe.MatchString(nonEmpty[i]) {
			working = true
			break
		}
	}

	for _, l := range rawLines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if isRuleOrBorderLine(trimmed) || isChromeLine(trimmed) {
			continue
		}
		trimmedBorder := strings.Trim(trimmed, "│┃║| \t")
		if isRuleOrBorderLine(trimmedBorder) || isChromeLine(trimmedBorder) || trimmedBorder == "" {
			continue
		}
		filtered = append(filtered, trimmedBorder)
	}

	return filtered, working
}

func matchInputLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "for shortcuts") {
		return false
	}
	if inputStartPromptRe.MatchString(trimmed) {
		rest := strings.TrimSpace(inputStartPromptRe.ReplaceAllString(trimmed, ""))
		if rest == "" {
			return false
		}
		if strings.HasPrefix(strings.ToLower(rest), "for shortcuts") {
			return false
		}
		return true
	}
	return inputQuestionRe.MatchString(trimmed)
}

func extractSummary(lines []string, matchIdx int) string {
	if matchIdx < 0 || matchIdx >= len(lines) {
		return ""
	}
	summary := lines[matchIdx]
	for j := matchIdx + 1; j < len(lines) && j <= matchIdx+2; j++ {
		next := lines[j]
		if watchDemoLineRe.MatchString(next) || matchInputLine(next) || watchErrorLineRe.MatchString(next) {
			break
		}
		prevTrimmed := strings.TrimSpace(summary)
		if strings.HasSuffix(prevTrimmed, ".") || strings.HasSuffix(prevTrimmed, "!") || strings.HasSuffix(prevTrimmed, "?") {
			break
		}
		summary = summary + " " + next
	}
	return strings.TrimSpace(summary)
}

func detectEvent(filtered []string, untilSet map[string]bool) (matchedEvent, summary string) {
	for i := len(filtered) - 1; i >= 0; i-- {
		line := filtered[i]
		if (untilSet["input"] || untilSet["any"]) && matchInputLine(line) {
			return "waiting_for_input", extractSummary(filtered, i)
		} else if (untilSet["demo"] || untilSet["done"] || untilSet["any"]) && watchDemoLineRe.MatchString(line) {
			return "ready_for_demo", extractSummary(filtered, i)
		} else if (untilSet["error"] || untilSet["any"]) && watchErrorLineRe.MatchString(line) {
			return "error", extractSummary(filtered, i)
		}
	}
	return "", ""
}

func isTerminalEvent(status string, untilSet map[string]bool) bool {
	switch status {
	case "waiting_for_input":
		return untilSet["input"] || untilSet["any"]
	case "ready_for_demo":
		return untilSet["demo"] || untilSet["done"] || untilSet["any"]
	case "error":
		return untilSet["error"] || untilSet["any"]
	default:
		return false
	}
}

func newWatchCmd() *cobra.Command {
	var (
		projectName   string
		taskSlug      string
		watchAll      bool
		intervalSec   int
		timeoutSec    int
		debounceCount int
		untilEvents   string
		notifyEnabled bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watchdog monitor: Polls worker agent pane(s) until input needed, finished, or error",
		Long: `Watchdog monitor: Polls worker agent pane(s) until input needed, finished, or error.
Filters TUI chrome and verifies event stability across debounced polls before reporting.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !watchAll && (projectName == "" || taskSlug == "") {
				return fmt.Errorf("provide --project and --task, or use --all to monitor all active tasks")
			}

			if intervalSec < 0 {
				intervalSec = 5
			}
			if timeoutSec <= 0 {
				timeoutSec = 600
			}
			if debounceCount <= 0 {
				debounceCount = 1
			}

			untilSet := make(map[string]bool)
			for _, evt := range strings.Split(strings.ToLower(untilEvents), ",") {
				trimmed := strings.TrimSpace(evt)
				if trimmed != "" {
					untilSet[trimmed] = true
				}
			}

			startTime := time.Now()
			deadline := startTime.Add(time.Duration(timeoutSec) * time.Second)

			// In single target mode, fail fast on missing project/task, missing block ID, or already terminal state
			if !watchAll {
				st, err := state.LoadState()
				if err != nil {
					return fmt.Errorf("could not load state: %w", err)
				}
				p, err := st.FindProject(projectName)
				if err != nil {
					return err
				}
				t, err := p.FindTask(taskSlug)
				if err != nil {
					return err
				}

				if isTerminalEvent(t.Status, untilSet) {
					elapsed := int(time.Since(startTime).Seconds())
					resp := map[string]any{
						"event":            t.Status,
						"project":          p.Name,
						"task":             t.TaskSlug,
						"summary":          t.LastSummary,
						"elapsed_seconds":  elapsed,
						"already_reported": true,
					}
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(resp)
				}

				if t.AgentBlockID == "" {
					return fmt.Errorf("task %q in project %q has no active agent block", t.TaskSlug, p.Name)
				}

				if _, err := getEngine(t.GetEngine()); err != nil {
					return err
				}
			}

			type targetTask struct {
				project   string
				task      string
				sessionID string
				blockID   string
				engine    string
			}

			type debounceState struct {
				lastEvent    string
				lastSnapshot string
				consecutive  int
			}
			debounceMap := make(map[string]*debounceState)

			for time.Now().Before(deadline) {
				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				default:
				}

				st, err := state.LoadState()
				if err != nil {
					select {
					case <-cmd.Context().Done():
						return cmd.Context().Err()
					case <-time.After(time.Duration(intervalSec) * time.Second):
					}
					continue
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
					if err != nil {
						return err
					}
					t, err := p.FindTask(taskSlug)
					if err != nil {
						return err
					}
					targets = append(targets, targetTask{
						project:   p.Name,
						task:      t.TaskSlug,
						sessionID: p.RexSessionID,
						blockID:   t.AgentBlockID,
						engine:    t.GetEngine(),
					})
				}

				for _, target := range targets {
					eng, err := getEngine(target.engine)
					if err != nil {
						if !watchAll {
							return err
						}
						continue
					}

					out, err := eng.Capture(cmd.Context(), target.sessionID, target.blockID, 30)
					if err != nil {
						if !watchAll {
							return fmt.Errorf("failed to capture terminal output: %w", err)
						}
						continue
					}

					targetKey := target.project + "/" + target.task
					filtered, working := paneLines(out)
					if working {
						// Suppress events while actively executing
						delete(debounceMap, targetKey)
						continue
					}

					matchedEvent, summary := detectEvent(filtered, untilSet)
					if matchedEvent == "" {
						delete(debounceMap, targetKey)
						continue
					}

					snapshot := strings.Join(filtered, "\n")
					ds, exists := debounceMap[targetKey]
					if !exists || ds.lastEvent != matchedEvent || ds.lastSnapshot != snapshot {
						debounceMap[targetKey] = &debounceState{
							lastEvent:    matchedEvent,
							lastSnapshot: snapshot,
							consecutive:  1,
						}
					} else {
						ds.consecutive++
					}

					if debounceMap[targetKey].consecutive >= debounceCount {
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

				select {
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(time.Duration(intervalSec) * time.Second):
				}
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
	cmd.Flags().IntVar(&debounceCount, "debounce", 2, "Number of consecutive stable polls required to confirm an event")
	cmd.Flags().StringVar(&untilEvents, "until", "input,demo,done,error,missing", "Comma-separated events to wait for")
	cmd.Flags().BoolVar(&notifyEnabled, "notify", true, "Send desktop notification on trigger")

	return cmd
}
