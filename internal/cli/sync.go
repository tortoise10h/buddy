package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/state"
)

// reconcileRexWithBackend reconciles cached Rex project sessions and task windows
// against live Rex sessions/windows. It marks vanished sessions/windows as missing
// and recovers tasks previously marked missing when their Rex window is alive.
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

	return state.MutateState(func(s *state.State) error {
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
					tData.Status = "missing"
					tData.LastSummary = "Rex session not found"
					pData.Tasks[tSlug] = tData
				}
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
					tData.Status = "missing"
					tData.LastSummary = "Window closed in Rex"
					pData.Tasks[tSlug] = tData
					continue
				}

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
				pData.Tasks[tSlug] = tData
			}

			s.Projects[pName] = pData
		}
		return nil
	})
}
