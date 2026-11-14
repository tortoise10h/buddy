package cli

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/state"
)

const defaultSyncTTL = 45 * time.Second

// defaultRexBackend returns the injectable Rex backend used by status --sync and TTL auto-sync.
var defaultRexBackend = func() backend.TerminalBackend {
	return rex.NewRexBackend()
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
