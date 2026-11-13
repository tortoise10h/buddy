package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/state"
)

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\].*?(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

	watchDemoLineRe  = regexp.MustCompile(`(?i)^(?:(?:\[?[✓✔xX☑]\]?|[✅•●*-])\s*)?(?:task\s+complete[d]?(?::\s*.+)?|implementation\s+complete[d]?(?::\s*.+)?|all\s+tests\s+pass(?:ed)?(?::\s*.+)?|ready\s+for\s+(?:review|demo)|successfully\s+(?:implemented|built|completed)(?::\s*.+)?|finished\s+implementing|done)[.!]?$`)
	watchErrorLineRe = regexp.MustCompile(`(?i)^(?:panic:\s+|fatal(?:\s+error)?:|traceback\s+\(most\s+recent\s+call\s+last\):?|npm\s+err!|build\s+failed)`)

	inputStartPromptRe  = regexp.MustCompile(`^(?:\[\?\]|\?)\s+`)
	toolOutputRe        = regexp.MustCompile(`(?i)(?:^|\s)(?:[a-zA-Z0-9_-]+\.[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+|\./\S+)\s+(?:\[.*\]|\d+(?:\.\d+)?s|\(cached\))`)
	interactivePhraseRe = regexp.MustCompile(`(?i)(?:(?:select|choose)\s+(?:an?\s+)?(?:option|action|mode):?|proceed\s*\?|clarification\s+needed|approval\s+needed|what\s+would\s+you\s+like|do\s+you\s+want\s+to|would\s+you\s+(?:like|prefer)|which\s+(?:one|option)|please\s+confirm|press\s+enter\s+to\s+continue|\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\))`)
	inputQuestionRe     = regexp.MustCompile(`(?i)^(?:(?:\[\?\]|\?)\s+.*|\s*(?:do\s+you\s+want\s+to|would\s+you\s+(?:like|prefer)|what\s+would\s+you\s+like|which\s+(?:one|option)|please\s+confirm|press\s+enter\s+to\s+continue|(?:select|choose)\s+(?:an?\s+)?(?:option|action|mode):?|clarification\s+needed|approval\s+needed|proceed\s*\?).*|.*(?:\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\)).*$)`)
	questionSentenceRe  = regexp.MustCompile(`(?i)^(?:which|what|how|where|when|who|should|can|could|would|do|does|did|is|are|will|may|shall)\s+.*\?$`)

	workingRe = regexp.MustCompile(`[\x{2800}-\x{28ff}⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒]|(?i)(?:running\s+command|thought\s+for|thinking\.\.\.|esc\s+to\s+cancel)`)

	idlePromptLineRe = regexp.MustCompile(`(?i)^(?:[>❯›]\s*(?:type a message|ask anything)?|type a message|ask anything)\s*$`)

	// Legacy aliases kept for compatibility
	watchDemoRe  = watchDemoLineRe
	watchErrorRe = watchErrorLineRe
	watchInputRe = inputQuestionRe

	// notifyFn is injectable for testing to intercept desktop notifications.
	notifyFn = notify.SendDesktopNotification

	// syncRexFn is injectable for testing to reconcile state with live Rex sessions.
	syncRexFn = func(ctx context.Context) error {
		return reconcileRexWithBackend(ctx, rex.NewRexBackend())
	}
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
	filtered, working, _ = paneState(raw)
	return filtered, working
}

func isBarePromptLine(line string) bool {
	return idlePromptLineRe.MatchString(strings.TrimSpace(line))
}

func isIdlePromptChrome(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "? for shortcuts") ||
		strings.Contains(lower, "ctrl+o to expand") ||
		strings.HasPrefix(lower, "auto ·") ||
		strings.HasPrefix(lower, "auto •") ||
		strings.Contains(lower, "auto ·") ||
		strings.Contains(lower, "type a message") ||
		strings.Contains(lower, "ask anything")
}

func paneState(raw string) (filtered []string, working bool, idlePrompt bool) {
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

	// Check tail (last up to 6 non-empty lines) for active working state or idle TUI prompt
	tailLen := 6
	if len(nonEmpty) < tailLen {
		tailLen = len(nonEmpty)
	}
	for i := len(nonEmpty) - tailLen; i < len(nonEmpty); i++ {
		if workingRe.MatchString(nonEmpty[i]) {
			working = true
			break
		}
		trimmedBorder := strings.Trim(nonEmpty[i], "│┃║| \t")
		if isIdlePromptChrome(nonEmpty[i]) || isIdlePromptChrome(trimmedBorder) || isBarePromptLine(trimmedBorder) {
			idlePrompt = true
		}
	}
	if working {
		idlePrompt = false
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

	return filtered, working, idlePrompt
}

func matchInputLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	// Outright reject known false positives
	if strings.Contains(lower, "for shortcuts") || strings.Contains(lower, "[no test files]") {
		return false
	}

	// Reject candidates that look like tool output (package/module path followed by timing or bracket text)
	if toolOutputRe.MatchString(trimmed) {
		return false
	}

	// If line starts with "?" or "[?]" prompt marker:
	if inputStartPromptRe.MatchString(trimmed) {
		rest := strings.TrimSpace(inputStartPromptRe.ReplaceAllString(trimmed, ""))
		if rest == "" {
			return false
		}
		if strings.HasPrefix(strings.ToLower(rest), "for shortcuts") {
			return false
		}
		// Candidate must end with "?" or match explicit interactive phrases
		return strings.HasSuffix(rest, "?") || interactivePhraseRe.MatchString(rest)
	}

	// For lines prefixed with choice markers like [y/n]
	if strings.HasPrefix(lower, "[y/n]") || strings.HasPrefix(lower, "(y/n)") ||
		strings.HasPrefix(lower, "[yes/no]") || strings.HasPrefix(lower, "(yes/no)") {
		return strings.HasSuffix(trimmed, "?") || interactivePhraseRe.MatchString(trimmed)
	}

	if questionSentenceRe.MatchString(trimmed) {
		return true
	}

	return inputQuestionRe.MatchString(trimmed) && (strings.HasSuffix(trimmed, "?") || interactivePhraseRe.MatchString(trimmed))
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

func hasMeaningfulOutput(filtered []string) bool {
	for _, l := range filtered {
		if !isBarePromptLine(l) {
			return true
		}
	}
	return false
}

func extractIdleSummary(filtered []string) string {
	var meaningful []string
	for _, l := range filtered {
		if !isBarePromptLine(l) {
			meaningful = append(meaningful, l)
		}
	}
	if len(meaningful) == 0 {
		return "Worker returned to idle prompt"
	}
	start := len(meaningful) - 2
	if start < 0 {
		start = 0
	}
	summary := strings.TrimSpace(strings.Join(meaningful[start:], " "))
	if len(summary) > 240 {
		summary = summary[:237] + "..."
	}
	return summary
}

func detectEventWithIdle(filtered []string, idlePrompt, sawWorking bool, untilSet map[string]bool) (matchedEvent, summary string) {
	if matchedEvent, summary = detectEvent(filtered, untilSet); matchedEvent != "" {
		return matchedEvent, summary
	}
	if idlePrompt && (untilSet["demo"] || untilSet["done"] || untilSet["any"]) && (sawWorking || hasMeaningfulOutput(filtered)) {
		return "ready_for_demo", extractIdleSummary(filtered)
	}
	return "", ""
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

func isWatchableStatus(status string) bool {
	switch status {
	case "running", "planning", "waiting_for_input", "ready_for_demo":
		return true
	default:
		return false
	}
}

func isTerminalEvent(status string, untilSet map[string]bool) bool {
	switch status {
	case "ready_for_demo":
		return untilSet["demo"] || untilSet["done"] || untilSet["any"]
	case "error":
		return untilSet["error"] || untilSet["any"]
	default:
		return false
	}
}

// evalProgramStatus interprets native OSC 7501 program_status records (e.g. from Rex).
// Returns handled=false when no records exist or when idle without a prior working turn,
// signaling the caller to fall back to screen text grep.
func evalProgramStatus(records []backend.ProgramStatusRecord, sawWorking bool, untilSet map[string]bool) (working bool, event, summary, snapshot string, handled bool) {
	if len(records) == 0 {
		return false, "", "", "", false
	}

	pickText := func(r backend.ProgramStatusRecord, fallback string) string {
		if r.Msg != "" {
			return r.Msg
		}
		if r.Title != "" {
			return r.Title
		}
		return fallback
	}

	for _, r := range records {
		if r.State == "blocked" {
			if untilSet["input"] || untilSet["any"] {
				fb := "Worker is waiting for input"
				if r.Kind != "" {
					fb = fmt.Sprintf("Worker blocked (%s)", r.Kind)
				}
				return false, "waiting_for_input", pickText(r, fb), fmt.Sprintf("native:blocked:%s:%s:%s", r.Kind, r.Title, r.Msg), true
			}
		}
	}
	for _, r := range records {
		if r.State == "working" {
			return true, "", "", "", true
		}
	}
	for _, r := range records {
		if r.State == "error" {
			if untilSet["error"] || untilSet["any"] {
				return false, "error", pickText(r, "Worker reported error"), fmt.Sprintf("native:error:%s:%s", r.Title, r.Msg), true
			}
		}
	}
	for _, r := range records {
		if r.State == "done" {
			if untilSet["demo"] || untilSet["done"] || untilSet["any"] {
				return false, "ready_for_demo", pickText(r, ""), fmt.Sprintf("native:done:%s:%s", r.Title, r.Msg), true
			}
		}
	}
	if sawWorking && (untilSet["demo"] || untilSet["done"] || untilSet["any"]) {
		return false, "ready_for_demo", pickText(records[0], ""), fmt.Sprintf("native:idle:%s:%s", records[0].Title, records[0].Msg), true
	}

	return false, "", "", "", false
}

type watchTarget struct {
	project   string
	task      string
	sessionID string
	blockID   string
	engine    string
	status    string
}

type watchDebounceState struct {
	lastEvent    string
	lastSnapshot string
	consecutive  int
}

type watchEvent struct {
	event           string
	project         string
	task            string
	summary         string
	alreadyReported bool
}

type watchRunner struct {
	cmd           *cobra.Command
	projectName   string
	taskSlug      string
	watchAll      bool
	interval      time.Duration
	timeout       time.Duration
	debounceCount int
	notifyEnabled bool
	untilSet      map[string]bool

	startTime       time.Time
	sawWorking      map[string]bool
	initiallyActive map[string]bool
	debounceMap     map[string]*watchDebounceState
}

func newWatchRunner(cmd *cobra.Command, projectName, taskSlug string, watchAll bool, intervalSec, timeoutSec, debounceCount int, untilEvents string, notifyEnabled bool) *watchRunner {
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
		if trimmed := strings.TrimSpace(evt); trimmed != "" {
			untilSet[trimmed] = true
		}
	}

	return &watchRunner{
		cmd:             cmd,
		projectName:     projectName,
		taskSlug:        taskSlug,
		watchAll:        watchAll,
		interval:        time.Duration(intervalSec) * time.Second,
		timeout:         time.Duration(timeoutSec) * time.Second,
		debounceCount:   debounceCount,
		notifyEnabled:   notifyEnabled,
		untilSet:        untilSet,
		startTime:       time.Now(),
		sawWorking:      make(map[string]bool),
		initiallyActive: make(map[string]bool),
		debounceMap:     make(map[string]*watchDebounceState),
	}
}

func (w *watchRunner) run() error {
	w.snapshotInitiallyActive()
	_ = syncRexFn(w.cmd.Context())

	if !w.watchAll {
		handled, err := w.preflightSingleTarget()
		if err != nil || handled {
			return err
		}
	}

	deadline := w.startTime.Add(w.timeout)
	firstPoll := true

	for time.Now().Before(deadline) {
		if err := w.cmd.Context().Err(); err != nil {
			return err
		}

		if !firstPoll {
			_ = syncRexFn(w.cmd.Context())
		}
		firstPoll = false

		st, err := state.LoadState()
		if err != nil {
			if err := w.sleepOrDone(); err != nil {
				return err
			}
			continue
		}

		targets, missingEv, err := w.collectTargets(st)
		if err != nil {
			return err
		}
		if missingEv != nil {
			return w.emitEvent(*missingEv)
		}

		for _, target := range targets {
			ev, err := w.pollTarget(target)
			if err != nil {
				return err
			}
			if ev != nil {
				return w.emitEvent(*ev)
			}
		}

		if err := w.sleepOrDone(); err != nil {
			return err
		}
	}

	return w.emitTimeout()
}

func (w *watchRunner) snapshotInitiallyActive() {
	initialSt, err := state.LoadState()
	if err != nil {
		return
	}
	for pName, pData := range initialSt.Projects {
		for tName, tData := range pData.Tasks {
			if isWatchableStatus(tData.Status) {
				w.initiallyActive[pName+"/"+tName] = true
			}
		}
	}
}

func (w *watchRunner) preflightSingleTarget() (bool, error) {
	st, err := state.LoadState()
	if err != nil {
		return false, fmt.Errorf("could not load state: %w", err)
	}
	p, err := st.FindProject(w.projectName)
	if err != nil {
		return false, err
	}
	t, err := p.FindTask(w.taskSlug)
	if err != nil {
		return false, err
	}

	if isTerminalEvent(t.Status, w.untilSet) {
		stillTerminal := true
		if t.AgentBlockID != "" {
			if eng, err := getEngine(t.GetEngine()); err == nil {
				target := watchTarget{
					project:   p.Name,
					task:      t.TaskSlug,
					sessionID: p.RexSessionID,
					blockID:   t.AgentBlockID,
					engine:    t.GetEngine(),
					status:    t.Status,
				}
				if working, _, _, _, probeErr := w.probeTarget(eng, target); probeErr == nil && working {
					stillTerminal = false
					w.sawWorking[p.Name+"/"+t.TaskSlug] = true
					_ = state.MutateState(func(s *state.State) error {
						return s.UpdateTask(p.Name, t.TaskSlug, "running", t.LastSummary)
					})
				}
			}
		}
		if stillTerminal {
			return true, w.emitEvent(watchEvent{
				event:           t.Status,
				project:         p.Name,
				task:            t.TaskSlug,
				summary:         t.LastSummary,
				alreadyReported: true,
			})
		}
	}

	if t.AgentBlockID == "" {
		return false, fmt.Errorf("task %q in project %q has no active agent block", t.TaskSlug, p.Name)
	}
	if _, err := getEngine(t.GetEngine()); err != nil {
		return false, err
	}
	return false, nil
}

func (w *watchRunner) collectTargets(st *state.State) ([]watchTarget, *watchEvent, error) {
	if !w.watchAll {
		p, err := st.FindProject(w.projectName)
		if err != nil {
			return nil, nil, err
		}
		t, err := p.FindTask(w.taskSlug)
		if err != nil {
			return nil, nil, err
		}
		return []watchTarget{{
			project:   p.Name,
			task:      t.TaskSlug,
			sessionID: p.RexSessionID,
			blockID:   t.AgentBlockID,
			engine:    t.GetEngine(),
			status:    t.Status,
		}}, nil, nil
	}

	var targets []watchTarget
	for pName, pData := range st.Projects {
		for tName, tData := range pData.Tasks {
			key := pName + "/" + tName
			if tData.Status == "missing" && w.initiallyActive[key] && (w.untilSet["missing"] || w.untilSet["any"]) {
				return nil, &watchEvent{
					event:   "missing",
					project: pName,
					task:    tName,
					summary: tData.LastSummary,
				}, nil
			}
			if isWatchableStatus(tData.Status) && tData.AgentBlockID != "" {
				targets = append(targets, watchTarget{
					project:   pName,
					task:      tName,
					sessionID: pData.RexSessionID,
					blockID:   tData.AgentBlockID,
					engine:    tData.GetEngine(),
					status:    tData.Status,
				})
			}
		}
	}
	return targets, nil, nil
}

func (w *watchRunner) probeTarget(eng engine.Engine, target watchTarget) (working bool, matchedEvent, summary, snapshot string, err error) {
	targetKey := target.project + "/" + target.task
	nativeHit := false

	// 1. Primary: query native terminal program_status (e.g. Rex OSC 7501) if supported
	if psp, ok := eng.(backend.ProgramStatusProvider); ok {
		if recs, statusErr := psp.GetProgramStatus(w.cmd.Context(), target.sessionID, target.blockID); statusErr == nil {
			working, matchedEvent, summary, snapshot, nativeHit = evalProgramStatus(recs, w.sawWorking[targetKey], w.untilSet)
		}
	}

	// 2. Fallback: capture pane output and grep/analyze screen text
	if !nativeHit {
		out, capErr := eng.Capture(w.cmd.Context(), target.sessionID, target.blockID, 30)
		if capErr != nil {
			return false, "", "", "", capErr
		}
		filtered, isWorking, idlePrompt := paneState(out)
		working = isWorking
		if !working {
			matchedEvent, summary = detectEventWithIdle(filtered, idlePrompt, w.sawWorking[targetKey], w.untilSet)
			snapshot = strings.Join(filtered, "\n")
		}
		return working, matchedEvent, summary, snapshot, nil
	}

	// Enrich empty native summary from pane tail if available
	if !working && matchedEvent != "" && summary == "" {
		if out, capErr := eng.Capture(w.cmd.Context(), target.sessionID, target.blockID, 30); capErr == nil {
			filtered, _, _ := paneState(out)
			summary = extractIdleSummary(filtered)
		} else {
			summary = "Worker completed task"
		}
	}
	return working, matchedEvent, summary, snapshot, nil
}

func (w *watchRunner) pollTarget(target watchTarget) (*watchEvent, error) {
	eng, err := getEngine(target.engine)
	if err != nil {
		if !w.watchAll {
			return nil, err
		}
		return nil, nil
	}

	working, matchedEvent, summary, snapshot, err := w.probeTarget(eng, target)
	if err != nil {
		if !w.watchAll {
			return nil, fmt.Errorf("failed to capture terminal output: %w", err)
		}
		return nil, nil
	}

	targetKey := target.project + "/" + target.task
	if working {
		w.sawWorking[targetKey] = true
		delete(w.debounceMap, targetKey)
		if target.status == "waiting_for_input" || target.status == "ready_for_demo" {
			_ = state.MutateState(func(s *state.State) error {
				return s.UpdateTask(target.project, target.task, "running", "")
			})
		}
		return nil, nil
	}

	if matchedEvent == "" || (matchedEvent == target.status && !w.sawWorking[targetKey]) {
		delete(w.debounceMap, targetKey)
		return nil, nil
	}

	ds, exists := w.debounceMap[targetKey]
	if !exists || ds.lastEvent != matchedEvent || ds.lastSnapshot != snapshot {
		w.debounceMap[targetKey] = &watchDebounceState{
			lastEvent:    matchedEvent,
			lastSnapshot: snapshot,
			consecutive:  1,
		}
	} else {
		ds.consecutive++
	}

	if w.debounceMap[targetKey].consecutive < w.debounceCount {
		return nil, nil
	}

	_ = state.MutateState(func(s *state.State) error {
		return s.UpdateTask(target.project, target.task, matchedEvent, summary)
	})
	return &watchEvent{
		event:   matchedEvent,
		project: target.project,
		task:    target.task,
		summary: summary,
	}, nil
}

func (w *watchRunner) emitEvent(ev watchEvent) error {
	if !ev.alreadyReported && w.notifyEnabled {
		notifyFn("Buddy Watchdog", fmt.Sprintf("%s/%s (%s)", ev.project, ev.task, ev.event), ev.summary, "Glass", true)
	}
	resp := map[string]any{
		"event":           ev.event,
		"project":         ev.project,
		"task":            ev.task,
		"summary":         ev.summary,
		"elapsed_seconds": int(time.Since(w.startTime).Seconds()),
	}
	if ev.alreadyReported {
		resp["already_reported"] = true
	}
	enc := json.NewEncoder(w.cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func (w *watchRunner) emitTimeout() error {
	elapsed := int(time.Since(w.startTime).Seconds())
	resp := map[string]any{
		"event":           "timeout",
		"summary":         fmt.Sprintf("Watch timed out after %ds", elapsed),
		"elapsed_seconds": elapsed,
	}
	enc := json.NewEncoder(w.cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func (w *watchRunner) sleepOrDone() error {
	select {
	case <-w.cmd.Context().Done():
		return w.cmd.Context().Err()
	case <-time.After(w.interval):
		return nil
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
			runner := newWatchRunner(cmd, projectName, taskSlug, watchAll, intervalSec, timeoutSec, debounceCount, untilEvents, notifyEnabled)
			return runner.run()
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
