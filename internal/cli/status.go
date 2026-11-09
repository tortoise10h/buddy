package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/state"
)

func newStatusCmd() *cobra.Command {
	var (
		formatStr   string
		projectName string
		syncLive    bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show tracked projects and tasks from cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if syncLive {
				rexDriver := rex.NewRexBackend()
				t3Driver := t3.NewT3Backend()
				rexAvailable := rexDriver.IsAvailable(cmd.Context())
				t3Available := t3Driver.IsAvailable(cmd.Context())

				var liveRexSessMap map[string]bool
				if rexAvailable {
					if liveSessions, err := rexDriver.ListSessions(cmd.Context()); err == nil {
						liveRexSessMap = make(map[string]bool)
						for _, s := range liveSessions {
							liveRexSessMap[s.SessionID] = true
						}
					}
				}

				_ = state.MutateState(func(s *state.State) error {
					for pName, pData := range s.Projects {
						var rexWinMap map[string]bool
						if rexAvailable && liveRexSessMap != nil && liveRexSessMap[pData.RexSessionID] {
							if windows, err := rexDriver.ListWindows(cmd.Context(), pData.RexSessionID); err == nil {
								rexWinMap = make(map[string]bool)
								for _, w := range windows {
									rexWinMap[w.WindowID] = true
								}
							}
						}

						for tSlug, tData := range pData.Tasks {
							eng := strings.ToLower(tData.GetEngine())
							if eng == "t3code" || eng == "t3" {
								if t3Available {
									handle := backend.TaskHandle{
										ProjectID: pName,
										ThreadID:  tData.WindowID,
									}
									rep, err := t3Driver.GetAgentStatus(cmd.Context(), handle)
									if err == nil && rep != nil {
										tData.Status = rep.Status
										tData.LastSummary = rep.Summary
									} else {
										alive, aliveErr := t3Driver.IsTaskAlive(cmd.Context(), handle)
										if aliveErr == nil && !alive {
											tData.Status = "missing"
											tData.LastSummary = "Thread closed or archived in T3 Code"
										}
									}
								} else {
									tData.Status = "missing"
									tData.LastSummary = "T3 Code server unavailable"
								}
								pData.Tasks[tSlug] = tData
							} else {
								// Rex task
								if !rexAvailable || (liveRexSessMap != nil && !liveRexSessMap[pData.RexSessionID]) {
									tData.Status = "missing"
									tData.LastSummary = "Rex session not found"
									pData.Tasks[tSlug] = tData
								} else if rexWinMap != nil && !rexWinMap[tData.WindowID] {
									tData.Status = "missing"
									tData.LastSummary = "Window closed in Rex"
									pData.Tasks[tSlug] = tData
								}
							}
						}
						s.Projects[pName] = pData
					}
					return nil
				})
			}

			st, err := state.LoadState()
			if err != nil {
				return err
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
				resp := map[string]any{
					"version":          st.Version,
					"buddy_session_id": st.BuddySessionID,
					"updated_at":       st.UpdatedAt,
					"projects":         projects,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if format == "brief" {
				var lines []string
				for pName, pData := range projects {
					if len(pData.Tasks) == 0 {
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
				}
				if len(lines) == 0 {
					fmt.Println("No active projects or tasks.")
				} else {
					fmt.Println(strings.Join(lines, "\n"))
				}
				return nil
			}

			// Table format
			fmt.Printf("%-16s %-20s %-8s %-16s %s\n", "PROJECT", "TASK", "AGENT", "STATUS", "SUMMARY")
			fmt.Println(strings.Repeat("-", 80))
			for pName, pData := range projects {
				if len(pData.Tasks) == 0 {
					fmt.Printf("%-16s %-20s %-8s %-16s %s\n", pName, "(none)", "-", "idle", "Rex session: "+pData.RexSessionID)
					continue
				}
				for tSlug, tData := range pData.Tasks {
					agent := tData.Agent
					if agent == "" {
						agent = "agy"
					}
					summary := tData.LastSummary
					if len(summary) > 40 {
						summary = summary[:40] + "..."
					}
					fmt.Printf("%-16s %-20s %-8s %-16s %s\n", pName, tSlug, agent, tData.Status, summary)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format: json, brief, table")
	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Filter by project name")
	cmd.Flags().BoolVar(&syncLive, "sync", false, "Reconcile cache with live Rex sessions and windows")

	return cmd
}
