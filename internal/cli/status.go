package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

func newStatusCmd() *cobra.Command {
	var (
		formatStr   string
		projectName string
		syncLive    bool
		noSync      bool
		flagEngine  string
		allEngines  bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show tracked projects and tasks from cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.LoadConfig()
			res := engine.ResolveEngine(flagEngine, os.Getenv, cfg)

			if !noSync && (allEngines || res.EngineName == "rex") {
				if syncLive {
					_ = reconcileRexWithBackend(cmd.Context(), defaultRexBackend())
				} else {
					reconcileRexIfStale(cmd.Context(), defaultSyncTTL)
				}
			}

			var st *state.State
			var err error
			if allEngines {
				st = &state.State{
					Version:  1,
					Projects: make(map[string]state.ProjectState),
				}
				engines, listErr := state.ListStateEngines()
				if listErr != nil {
					return listErr
				}
				for _, engName := range engines {
					engState, loadErr := state.LoadStateForEngine(engName)
					if loadErr != nil {
						return loadErr
					}
					if engState.UpdatedAt > st.UpdatedAt {
						st.UpdatedAt = engState.UpdatedAt
					}
					if st.BuddySessionID == "" && engState.BuddySessionID != "" {
						st.BuddySessionID = engState.BuddySessionID
					}
					for pName, pData := range engState.Projects {
						existing, exists := st.Projects[pName]
						if !exists {
							pCopy := pData
							pCopy.Tasks = make(map[string]state.TaskState, len(pData.Tasks))
							for k, v := range pData.Tasks {
								pCopy.Tasks[k] = v
							}
							st.Projects[pName] = pCopy
							continue
						}
						if existing.RexSessionID == "" && pData.RexSessionID != "" {
							existing.RexSessionID = pData.RexSessionID
							existing.RexSessionLabel = pData.RexSessionLabel
						}
						if existing.Tasks == nil {
							existing.Tasks = make(map[string]state.TaskState)
						}
						for k, v := range pData.Tasks {
							existing.Tasks[k] = v
						}
						st.Projects[pName] = existing
					}
				}
			} else {
				st, err = state.LoadStateForEngine(res.EngineName)
				if err != nil {
					return err
				}
			}

			projects := st.Projects
			if projectName != "" {
				matched, err := st.FindProject(projectName)
				if err != nil {
					return err
				}
				projects = map[string]state.ProjectState{
					matched.Name: *matched,
				}
			}

			format := strings.ToLower(formatStr)
			if format == "" || format == "json" {
				hostBlock := map[string]any{
					"detected": res.HostInfo.Kind,
					"engine":   res.EngineName,
					"source":   res.Source,
					"signals":  res.HostInfo.Signals,
				}
				resp := map[string]any{
					"version":          st.Version,
					"host":             hostBlock,
					"buddy_session_id": st.BuddySessionID,
					"updated_at":       st.UpdatedAt,
					"last_synced_at":   st.LastSyncedAt,
					"projects":         projects,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if format == "brief" {
				fmt.Printf("Host: %s | Engine: %s (source: %s)\n", res.HostInfo.Kind, res.EngineName, res.Source)
				var lines []string
				for pName, pData := range projects {
					if len(pData.Tasks) == 0 && len(pData.UntrackedWindows) == 0 {
						lines = append(lines, fmt.Sprintf("%s: No active tasks (session: %s)", pName, pData.RexSessionID))
						continue
					}
					for tSlug, tData := range pData.Tasks {
						agent := tData.Agent
						if agent == "" {
							agent = "agy"
						}
						lines = append(lines, fmt.Sprintf("• %s/%s [%s | %s]: %s", pName, tSlug, agent, tData.Status, tData.LastSummary))
					}
					for _, uw := range pData.UntrackedWindows {
						lines = append(lines, fmt.Sprintf("• %s/%s [untracked | %s]: %s (window: %s)", pName, uw.Label, uw.DetectedStatus, uw.Summary, uw.WindowID))
					}
				}
				if len(lines) == 0 {
					fmt.Println("No active projects or tasks.")
				} else {
					fmt.Println(strings.Join(lines, "\n"))
				}
				return nil
			}

			// Table format
			fmt.Printf("Host: %s | Engine: %s (source: %s)\n", res.HostInfo.Kind, res.EngineName, res.Source)
			fmt.Printf("%-16s %-20s %-8s %-8s %-16s %s\n", "PROJECT", "TASK", "AGENT", "ENGINE", "STATUS", "SUMMARY")
			fmt.Println(strings.Repeat("-", 80))
			for pName, pData := range projects {
				if len(pData.Tasks) == 0 && len(pData.UntrackedWindows) == 0 {
					fmt.Printf("%-16s %-20s %-8s %-8s %-16s %s\n", pName, "(none)", "-", "-", "idle", "Rex session: "+pData.RexSessionID)
					continue
				}
				for tSlug, tData := range pData.Tasks {
					agent := tData.Agent
					if agent == "" {
						agent = "agy"
					}
					eng := tData.GetEngine()
					summary := tData.LastSummary
					if len(summary) > 40 {
						summary = summary[:40] + "..."
					}
					fmt.Printf("%-16s %-20s %-8s %-8s %-16s %s\n", pName, tSlug, agent, eng, tData.Status, summary)
				}
				for _, uw := range pData.UntrackedWindows {
					summary := uw.Summary
					if len(summary) > 40 {
						summary = summary[:40] + "..."
					}
					fmt.Printf("%-16s %-20s %-8s %-8s %-16s %s\n", pName, uw.Label, "user", "rex", "untracked:"+uw.DetectedStatus, summary)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format: json, brief, table")
	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Filter by project name")
	cmd.Flags().BoolVar(&syncLive, "sync", false, "Reconcile cache with live Rex sessions and windows")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip automatic TTL reconciliation")
	cmd.Flags().StringVar(&flagEngine, "engine", "", "Execution engine (rex, t3, wezterm)")
	cmd.Flags().BoolVar(&allEngines, "all-engines", false, "Show state across all engines")

	return cmd
}
