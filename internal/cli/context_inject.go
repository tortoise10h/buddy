package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
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

			st, err := state.LoadState()
			if err != nil {
				fmt.Println("{}")
				return nil
			}

			// Read .state_seen.json for diffing
			seenPath := filepath.Join(state.GetBuddyDir(), ".state_seen.json")
			stateSeen := make(map[string]seenTaskInfo)
			if seenData, err := os.ReadFile(seenPath); err == nil {
				_ = json.Unmarshal(seenData, &stateSeen)
			}

			newSeen := make(map[string]seenTaskInfo)
			for k, v := range stateSeen {
				newSeen[k] = v
			}

			var taskSummaries []string
			var nudges []string

			for pName, pData := range st.Projects {
				for tName, tData := range pData.Tasks {
					tKey := fmt.Sprintf("%s/%s", pName, tName)
					agent := tData.Agent
					if agent == "" {
						agent = "agy"
					}
					status := tData.Status
					if status == "" {
						status = "unknown"
					}
					summary := tData.LastSummary
					updated := tData.UpdatedAt

					taskSummaries = append(taskSummaries, fmt.Sprintf("%s (%s, %s: '%s')", tKey, agent, status, summary))

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
				}
			}

			// Save updated seen state asynchronously or quickly
			if seenData, err := json.MarshalIndent(newSeen, "", "  "); err == nil {
				_ = os.WriteFile(seenPath, seenData, 0644)
			}

			tasksStr := "None active"
			if len(taskSummaries) > 0 {
				tasksStr = strings.Join(taskSummaries, "; ")
			}

			orchAgent := cfg.GetEffectiveOrchestratorAgent()
			workerAgent := cfg.GetEffectiveWorkerAgent()

			var nudgeBanner string
			if len(nudges) > 0 {
				nudgeBanner = fmt.Sprintf("\n\n[🚨 BUDDY PROACTIVE NUDGE] State transitions requiring immediate attention:\n%s\nACTION REQUIRED: Prioritize notifying the user about these worker milestones/questions before addressing other requests!", strings.Join(nudges, "\n"))
			}

			ephemeralText := fmt.Sprintf("[Buddy Live State] Orchestrator: %s | Worker: %s | Active projects: %d | Tasks: %s. 🚨 ZERO-TOLERANCE GUARDRAIL (RULE #1): NEVER edit project files, docs, or code in this session. Even for minor edits, typos, or READMEs, ALWAYS dispatch a task tab via 'buddy-ctl dispatch'.%s",
				orchAgent, workerAgent, len(st.Projects), tasksStr, nudgeBanner)

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
