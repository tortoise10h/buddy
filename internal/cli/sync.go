package cli

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

const defaultSyncTTL = 45 * time.Second
const defaultT3SyncTimeout = 5 * time.Second

// defaultRexBackend returns the injectable Rex backend used by status --sync and TTL auto-sync.
var defaultRexBackend = func() backend.TerminalBackend {
	return rex.NewRexBackend()
}

// syncT3Fn is injectable for testing to reconcile state with live T3 threads.
var syncT3Fn = func(ctx context.Context) error {
	eng, err := getEngine("t3")
	if err != nil {
		return err
	}
	t3Eng, ok := eng.(*engine.T3Engine)
	if !ok {
		return nil
	}
	return reconcileT3WithEngine(ctx, t3Eng)
}

var defaultShellWindowLabels = map[string]bool{
	"":         true,
	"shell":    true,
	"zsh":      true,
	"bash":     true,
	"fish":     true,
	"sh":       true,
	"demo":     true,
	"terminal": true,
}

func isDefaultShellLabel(label string) bool {
	return defaultShellWindowLabels[strings.ToLower(strings.TrimSpace(label))]
}

func hasAgentChromeIndicators(raw string) bool {
	lower := strings.ToLower(stripANSI(raw))
	return strings.Contains(lower, "? for shortcuts") ||
		strings.Contains(lower, "auto ·") ||
		strings.Contains(lower, "auto •") ||
		strings.Contains(lower, "ask anything") ||
		strings.Contains(lower, "type a message") ||
		strings.Contains(lower, "thought for") ||
		strings.Contains(lower, "running command") ||
		strings.Contains(lower, "esc to cancel")
}

// probeLiveBlockStatus inspects a terminal block via native OSC 7501 program_status
// and falls back to screen text capture using the same heuristics as watch.go.
func probeLiveBlockStatus(ctx context.Context, tb backend.TerminalBackend, sessionID, blockID, currentStatus, currentSummary string) (newStatus, newSummary string, hasAgentSignal bool) {
	if blockID == "" {
		return currentStatus, currentSummary, false
	}

	untilAll := map[string]bool{
		"input": true,
		"demo":  true,
		"done":  true,
		"error": true,
		"any":   true,
	}

	// 1. Native OSC 7501 program_status if supported
	if psp, ok := tb.(backend.ProgramStatusProvider); ok {
		if recs, err := psp.GetProgramStatus(ctx, sessionID, blockID); err == nil && len(recs) > 0 {
			hasAgentSignal = true
			working, ev, sum, _, handled := evalProgramStatus(recs, false, untilAll)
			if handled {
				if working {
					return "running", currentSummary, true
				}
				if ev != "" {
					if sum == "" {
						if out, capErr := tb.Capture(ctx, sessionID, blockID, 30); capErr == nil {
							filtered, _, _ := paneState(out)
							sum = extractIdleSummary(filtered)
						}
					}
					if sum == "" {
						sum = currentSummary
					}
					return ev, sum, true
				}
			}
		}
	}

	// 2. Screen text capture fallback
	out, err := tb.Capture(ctx, sessionID, blockID, 30)
	if err != nil {
		return currentStatus, currentSummary, hasAgentSignal
	}
	if hasAgentChromeIndicators(out) {
		hasAgentSignal = true
	}

	filtered, working, idlePrompt := paneState(out)
	if working {
		return "running", currentSummary, true
	}

	matchedEvent, summary := detectEventWithIdle(filtered, idlePrompt, false, untilAll)
	if matchedEvent != "" {
		if summary == "" {
			summary = currentSummary
		}
		return matchedEvent, summary, hasAgentSignal
	}

	if idlePrompt && len(filtered) > 0 {
		sum := extractIdleSummary(filtered)
		if currentStatus == "" || currentStatus == "unknown" {
			return "running", sum, hasAgentSignal
		}
		return currentStatus, sum, hasAgentSignal
	}

	return currentStatus, currentSummary, hasAgentSignal
}

// reconcileRexIfStale runs a bounded deep reconcile when the Rex state cache is older than ttl.
func reconcileRexIfStale(ctx context.Context, ttl time.Duration) {
	st, err := state.LoadStateForEngine("rex")
	if err != nil {
		return
	}
	if state.IsFreshWithin(st.LastSyncedAt, ttl) {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_ = syncRexFn(syncCtx)
}

// reconcileRexWithBackend reconciles cached Rex project sessions and task windows
// against live Rex sessions/windows. It marks vanished sessions/windows as missing,
// recovers tasks previously marked missing when their Rex window is alive, deep-probes
// live task blocks to refresh status/summary, and discovers untracked windows.
func reconcileRexWithBackend(ctx context.Context, tb backend.TerminalBackend) error {
	if tb == nil || !tb.IsAvailable(ctx) {
		return nil
	}

	liveSessions, err := tb.ListSessions(ctx)
	if err != nil {
		return err
	}

	byID := make(map[string]backend.SessionInfo, len(liveSessions))
	byLabel := make(map[string]backend.SessionInfo, len(liveSessions))
	byPath := make(map[string]backend.SessionInfo, len(liveSessions))
	for _, s := range liveSessions {
		byID[s.SessionID] = s
		if s.Name != "" {
			byLabel[strings.ToLower(s.Name)] = s
		}
		if s.Path != "" {
			byPath[filepath.Clean(s.Path)] = s
		}
	}

	windowsBySession := make(map[string][]backend.WindowInfo)
	cleanedRexWindows, _ := state.CleanedWindowIDs("rex")

	return state.MutateStateForEngine("rex", func(s *state.State) error {
		now := state.NowISO()
		s.LastSyncedAt = now

		for pName, pData := range s.Projects {
			sess, found := byID[pData.RexSessionID]
			if !found && pData.RexSessionLabel != "" {
				sess, found = byLabel[strings.ToLower(pData.RexSessionLabel)]
			}
			if !found {
				sess, found = byLabel[strings.ToLower(pName)]
			}
			if !found && pData.ProjectPath != "" {
				sess, found = byPath[filepath.Clean(pData.ProjectPath)]
			}

			if !found {
				for tSlug, tData := range pData.Tasks {
					if tData.GetEngine() != "rex" {
						continue
					}
					if tData.Status == "closed" || tData.Status == "done" {
						continue
					}
					tData.Status = "missing"
					tData.LastSummary = "Rex session not found"
					tData.LastVerifiedAt = now
					pData.Tasks[tSlug] = tData
				}
				pData.UntrackedWindows = nil
				s.Projects[pName] = pData
				continue
			}

			if pData.RexSessionID != sess.SessionID {
				pData.RexSessionID = sess.SessionID
			}

			windows, ok := windowsBySession[sess.SessionID]
			if !ok {
				var listErr error
				windows, listErr = tb.ListWindows(ctx, sess.SessionID)
				if listErr != nil {
					s.Projects[pName] = pData
					continue
				}
				windowsBySession[sess.SessionID] = windows
			}

			winByID := make(map[string]backend.WindowInfo, len(windows))
			winByLabel := make(map[string]backend.WindowInfo, len(windows))
			for _, w := range windows {
				winByID[w.WindowID] = w
				if w.Label != "" {
					winByLabel[strings.ToLower(w.Label)] = w
				}
			}

			matchedWindowIDs := make(map[string]bool, len(windows))

			for tSlug, tData := range pData.Tasks {
				if tData.GetEngine() != "rex" {
					continue
				}

				win, winFound := winByID[tData.WindowID]
				if !winFound && tData.WindowLabel != "" {
					win, winFound = winByLabel[strings.ToLower(tData.WindowLabel)]
				}
				if !winFound {
					win, winFound = winByLabel[strings.ToLower(tSlug)]
				}

				if !winFound {
					if tData.Status != "closed" && tData.Status != "done" {
						tData.Status = "missing"
						tData.LastSummary = "Window closed in Rex"
						tData.LastVerifiedAt = now
						pData.Tasks[tSlug] = tData
					}
					continue
				}

				matchedWindowIDs[win.WindowID] = true

				if tData.WindowID != win.WindowID {
					tData.WindowID = win.WindowID
				}
				if tData.AgentBlockID == "" && len(win.BlockIDs) > 0 {
					tData.AgentBlockID = win.BlockIDs[0]
				}
				if tData.Status == "missing" {
					tData.Status = "running"
					if tData.LastSummary == "Rex session not found" || tData.LastSummary == "Window closed in Rex" {
						tData.LastSummary = "Reconnected to live Rex window"
					}
				}

				// Deep probe live block for non-closed tasks
				if tData.Status != "closed" && tData.Status != "done" && tData.AgentBlockID != "" {
					probedStatus, probedSummary, _ := probeLiveBlockStatus(ctx, tb, sess.SessionID, tData.AgentBlockID, tData.Status, tData.LastSummary)
					if probedStatus != "" && probedStatus != tData.Status {
						tData.Status = probedStatus
						tData.UpdatedAt = now
					}
					if probedSummary != "" && probedSummary != tData.LastSummary {
						tData.LastSummary = probedSummary
						tData.UpdatedAt = now
					}
					tData.LastVerifiedAt = now
				}

				pData.Tasks[tSlug] = tData
			}

			// Discover untracked windows in this project's Rex session
			var untracked []state.UntrackedWindow
			for _, w := range windows {
				if matchedWindowIDs[w.WindowID] {
					continue
				}
				if _, wasCleaned := cleanedRexWindows[w.WindowID]; wasCleaned {
					continue
				}
				blockID := ""
				if len(w.BlockIDs) > 0 {
					blockID = w.BlockIDs[0]
				}

				detectedStatus, summary, hasAgentSignal := probeLiveBlockStatus(ctx, tb, sess.SessionID, blockID, "running", "")
				if isDefaultShellLabel(w.Label) && !hasAgentSignal {
					continue
				}
				if detectedStatus == "" {
					detectedStatus = "running"
				}
				if summary == "" {
					summary = "Untracked window in Rex"
				}
				untracked = append(untracked, state.UntrackedWindow{
					WindowID:       w.WindowID,
					Label:          w.Label,
					BlockID:        blockID,
					DetectedStatus: detectedStatus,
					Summary:        summary,
				})
			}
			pData.UntrackedWindows = untracked

			s.Projects[pName] = pData
		}
		return nil
	})
}

// reconcileActiveIfStale runs bounded TTL-gated state reconciliation for the active engine.
func reconcileActiveIfStale(ctx context.Context, engineName string, ttl time.Duration) {
	switch engineName {
	case "rex":
		reconcileRexIfStale(ctx, ttl)
	case "t3":
		reconcileT3IfStale(ctx, ttl)
	}
}

// reconcileActiveNow runs an immediate live state reconciliation for the given engine.
func reconcileActiveNow(ctx context.Context, engineName string) {
	switch engineName {
	case "rex":
		_ = reconcileRexWithBackend(ctx, defaultRexBackend())
	case "t3":
		_ = syncT3Fn(ctx)
	}
}

// reconcileT3IfStale runs a bounded deep reconcile when the T3 state cache is older than ttl.
func reconcileT3IfStale(ctx context.Context, ttl time.Duration) {
	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		return
	}
	if state.IsFreshWithin(st.LastSyncedAt, ttl) {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, defaultT3SyncTimeout)
	defer cancel()
	_ = syncT3Fn(syncCtx)
}

// reconcileT3WithEngine reconciles cached T3 tasks and discovers untracked project threads.
// It is bounded to one ThreadShow (via ProbeStatus) per tracked active task and one
// ThreadList per tracked project that has a filesystem path.
func reconcileT3WithEngine(ctx context.Context, eng *engine.T3Engine) error {
	if eng == nil {
		return nil
	}

	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		return err
	}

	type taskProbeResult struct {
		probe   engine.TaskStatusProbe
		missing bool
		ok      bool
	}
	probeResults := make(map[string]taskProbeResult)
	projectThreads := make(map[string][]t3.ThreadSummary)
	projectListed := make(map[string]bool)

	for pName, pData := range st.Projects {
		for tSlug, tData := range pData.Tasks {
			if tData.GetEngine() != "t3" {
				continue
			}
			if tData.Status == "closed" || tData.Status == "done" {
				continue
			}
			threadID := tData.AgentBlockID
			if threadID == "" {
				threadID = tData.WindowID
			}
			if threadID == "" {
				continue
			}
			probe, probeErr := eng.ProbeStatus(ctx, pData.RexSessionID, threadID)
			key := pName + "\x00" + tSlug
			if probeErr != nil {
				if isT3ThreadNotFoundError(probeErr) {
					probeResults[key] = taskProbeResult{missing: true}
				}
				continue
			}
			probeResults[key] = taskProbeResult{probe: probe, ok: true}
		}

		if pData.ProjectPath != "" {
			if threads, listErr := eng.ListProjectThreads(ctx, pData.ProjectPath); listErr == nil {
				projectThreads[pName] = threads
				projectListed[pName] = true
			}
		}
	}

	cleanedT3Threads, _ := state.CleanedWindowIDs("t3")

	return state.MutateStateForEngine("t3", func(s *state.State) error {
		now := state.NowISO()
		s.LastSyncedAt = now

		for pName, pData := range s.Projects {
			trackedIDs := make(map[string]bool, len(pData.Tasks)*2)

			for tSlug, tData := range pData.Tasks {
				if tData.WindowID != "" {
					trackedIDs[tData.WindowID] = true
				}
				if tData.AgentBlockID != "" {
					trackedIDs[tData.AgentBlockID] = true
				}
				if tData.GetEngine() != "t3" {
					continue
				}
				if tData.Status == "closed" || tData.Status == "done" {
					continue
				}

				res, hasRes := probeResults[pName+"\x00"+tSlug]
				if !hasRes {
					continue
				}
				if res.missing {
					if tData.Status != "missing" {
						tData.Status = "missing"
						tData.UpdatedAt = now
					}
					tData.LastSummary = "Thread not found in T3"
					tData.LastVerifiedAt = now
					pData.Tasks[tSlug] = tData
					continue
				}
				if !res.ok {
					continue
				}

				if res.probe.Status != "" && res.probe.Status != "unknown" && res.probe.Status != tData.Status {
					tData.Status = res.probe.Status
					tData.UpdatedAt = now
				}
				if res.probe.Summary != "" && res.probe.Summary != tData.LastSummary {
					tData.LastSummary = res.probe.Summary
					tData.UpdatedAt = now
				}
				tData.LastVerifiedAt = now
				pData.Tasks[tSlug] = tData
			}

			if projectListed[pName] {
				threads := projectThreads[pName]
				var untracked []state.UntrackedWindow
				for _, th := range threads {
					if th.ThreadID == "" || trackedIDs[th.ThreadID] {
						continue
					}
					if _, wasCleaned := cleanedT3Threads[th.ThreadID]; wasCleaned {
						continue
					}
					if th.Settled {
						continue
					}
					label := truncateT3Label(th.Title, 40)
					if label == "" {
						label = th.ThreadID
					}
					summary := strings.Join(strings.Fields(th.Title), " ")
					if summary == "" {
						summary = "Untracked thread in T3"
					} else if len(summary) > 240 {
						summary = summary[:237] + "..."
					}
					untracked = append(untracked, state.UntrackedWindow{
						WindowID:       th.ThreadID,
						Label:          label,
						BlockID:        th.ThreadID,
						DetectedStatus: mapT3ThreadSummaryStatus(th),
						Summary:        summary,
					})
				}
				pData.UntrackedWindows = untracked
			}

			s.Projects[pName] = pData
		}
		return nil
	})
}

func isT3ThreadNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "not found") ||
		strings.Contains(lower, "unknown thread") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "no such thread")
}

func truncateT3Label(title string, maxLen int) string {
	collapsed := strings.Join(strings.Fields(title), " ")
	if collapsed == "" || maxLen <= 0 {
		return collapsed
	}
	if len(collapsed) <= maxLen {
		return collapsed
	}
	if maxLen <= 3 {
		return collapsed[:maxLen]
	}
	return collapsed[:maxLen-3] + "..."
}

func mapT3ThreadSummaryStatus(th t3.ThreadSummary) string {
	raw := strings.ToLower(strings.TrimSpace(th.Status))
	isPlan := strings.EqualFold(strings.TrimSpace(th.InteractionMode), "plan")
	switch raw {
	case "error", "failed", "errored", "crashed", "interrupted", "cancelled", "canceled", "rolled_back":
		return "error"
	case "planning":
		return "planning"
	case "preparing", "queued", "starting", "running", "in_progress", "active", "pending":
		if isPlan {
			return "planning"
		}
		return "running"
	case "waiting_for_input", "blocked", "paused", "needs_input":
		return "waiting_for_input"
	case "waiting":
		if th.BlockedOnThreadWait || len(th.PendingBackgroundTasks) > 0 {
			return "running"
		}
		return "waiting_for_input"
	case "idle", "completed", "done", "finished", "settled", "ready":
		if th.BlockedOnThreadWait || len(th.PendingBackgroundTasks) > 0 {
			return "running"
		}
		return "ready_for_demo"
	default:
		return "unknown"
	}
}
