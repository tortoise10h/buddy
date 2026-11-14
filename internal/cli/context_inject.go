package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type seenTaskInfo struct {
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

func newContextInjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context-inject",
		Short: "PreInvocation hook handler (outputs ephemeralMessage JSON in <20ms)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fast exit if payload specifies non-buddy workspace, or read stdin safely
			var payload map[string]any
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				raw, _ := io.ReadAll(os.Stdin)
				if len(raw) > 0 {
					_ = json.Unmarshal(raw, &payload)
				}
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			resEngine := engine.ResolveEngine("", os.Getenv, cfg)
			if resEngine.EngineName == "rex" {
				reconcileRexIfStale(cmd.Context(), defaultSyncTTL)
			}

			st, err := state.LoadStateForEngine(resEngine.EngineName)
			if err != nil {
				fmt.Println("{}")
				return nil
			}

			// Read per-engine .<engine>_seen.json for diffing
			seenPath := state.GetStateSeenPathForEngine(resEngine.EngineName)
			stateSeen := make(map[string]seenTaskInfo)
			if seenData, err := os.ReadFile(seenPath); err == nil {
				_ = json.Unmarshal(seenData, &stateSeen)
			}

			newSeen := make(map[string]seenTaskInfo)
			for k, v := range stateSeen {
				newSeen[k] = v
			}

			projNames := make([]string, 0, len(st.Projects))
			for pName := range st.Projects {
				projNames = append(projNames, pName)
			}
			sort.Strings(projNames)

			var taskSummaries []string
			var untrackedSummaries []string
			var nudges []string
			activeProjectsCount := 0

			for _, pName := range projNames {
				pData := st.Projects[pName]
				taskNames := make([]string, 0, len(pData.Tasks))
				for tName := range pData.Tasks {
					taskNames = append(taskNames, tName)
				}
				sort.Strings(taskNames)

				projHasLive := len(pData.UntrackedWindows) > 0
				for _, tName := range taskNames {
					tData := pData.Tasks[tName]
					tKey := fmt.Sprintf("%s/%s", pName, tName)
					status := tData.Status
					if status == "" {
						status = "unknown"
					}
					summary := cleanTaskSummary(tData)
					updated := tData.UpdatedAt

					prevSeen, exists := stateSeen[tKey]
					if status == "waiting_for_input" || status == "ready_for_demo" || status == "error" {
						if !exists || prevSeen.Status != status || prevSeen.UpdatedAt != updated {
							switch status {
							case "waiting_for_input":
								nudges = append(nudges, fmt.Sprintf("• ❓ %s: WAITING FOR INPUT — %q", tKey, summary))
							case "ready_for_demo":
								nudges = append(nudges, fmt.Sprintf("• 🚀 %s: READY FOR DEMO — %q", tKey, summary))
							case "error":
								nudges = append(nudges, fmt.Sprintf("• ⚠️ %s: ERROR — %q", tKey, summary))
							}
						}
					}

					newSeen[tKey] = seenTaskInfo{
						Status:    status,
						UpdatedAt: updated,
					}

					// Filter out dead/completed tasks from active task list
					if !isLiveOpenTaskStatus(status) {
						continue
					}
					projHasLive = true

					agent := tData.Agent
					if agent == "" {
						agent = "agy"
					}
					staleTag := ""
					if tData.LastVerifiedAt != "" && !state.IsFreshWithin(tData.LastVerifiedAt, defaultSyncTTL) {
						staleTag = ", stale"
					}
					taskSummaries = append(taskSummaries, fmt.Sprintf("%s (%s, %s%s: '%s')", tKey, agent, status, staleTag, summary))
				}

				for _, uw := range pData.UntrackedWindows {
					untrackedSummaries = append(untrackedSummaries, fmt.Sprintf("%s/%s [window=%s, %s: '%s']", pName, uw.Label, uw.WindowID, uw.DetectedStatus, uw.Summary))
				}

				if projHasLive {
					activeProjectsCount++
				}
			}

			// Save updated seen state quickly
			if seenData, err := json.MarshalIndent(newSeen, "", "  "); err == nil {
				_ = os.MkdirAll(filepath.Dir(seenPath), 0755)
				_ = os.WriteFile(seenPath, seenData, 0644)
			}

			tasksStr := "None active"
			if len(taskSummaries) > 0 {
				tasksStr = strings.Join(taskSummaries, "; ")
			}

			untrackedSegment := ""
			if len(untrackedSummaries) > 0 {
				untrackedSegment = fmt.Sprintf(" | Untracked live tabs: %s (use 'buddy-ctl task adopt -p <proj> -w <window>' to adopt)", strings.Join(untrackedSummaries, "; "))
			}

			recWatch := computeRecommendedWatch(st)
			watchSegment := ""
			if recWatch != "" {
				watchSegment = fmt.Sprintf(" | Situational Watchdog: '%s'", recWatch)
			}

			var orchAgent, workerAgent string
			if resEngine.EngineName == "t3" {
				orchAgent = "t3 (direct)"
				p, m := cfg.GetT3ProviderModel()
				if p != "" && m != "" {
					workerAgent = p + "/" + m
				} else {
					workerAgent = "unconfigured"
				}
			} else {
				orchAgent = cfg.GetEffectiveOrchestratorAgent(resEngine.EngineName)
				workerAgent = cfg.GetEffectiveWorkerAgent(resEngine.EngineName)
			}

			var nudgeBanner string
			if len(nudges) > 0 {
				nudgeBanner = fmt.Sprintf("\n\n[🚨 BUDDY PROACTIVE NUDGE] State transitions requiring immediate attention:\n%s\nACTION REQUIRED: Prioritize notifying the user about these worker milestones/questions before addressing other requests!", strings.Join(nudges, "\n"))
			}

			ephemeralText := fmt.Sprintf("[Buddy Live State] Orchestrator: %s | Worker: %s | Active projects: %d | Open tasks: %s%s%s. 🚨 GUARDRAILS: (1) NEVER edit project files directly in ~/.buddy/. (2) 3-Tier Tab Triage: reuse via 'send' ONLY on strong/unambiguous match; if related but uncertain, list candidate slug/status/summary and ASK the user; if distinct, 'dispatch'.%s",
				orchAgent, workerAgent, activeProjectsCount, tasksStr, untrackedSegment, watchSegment, nudgeBanner)

			out := map[string]any{
				"injectSteps": []map[string]any{
					{
						"ephemeralMessage": ephemeralText,
					},
				},
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			return enc.Encode(out)
		},
	}
}
