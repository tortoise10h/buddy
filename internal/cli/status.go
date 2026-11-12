package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
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
				if rexDriver.IsAvailable(cmd.Context()) {
					liveSessions, err := rexDriver.ListSessions(cmd.Context())
					if err == nil {
						liveSessMap := make(map[string]bool)
						for _, s := range liveSessions {
							liveSessMap[s.SessionID] = true
						}

						_ = state.MutateState(func(s *state.State) error {
							for pName, pData := range s.Projects {
								if !liveSessMap[pData.RexSessionID] {
									for tSlug, tData := range pData.Tasks {
										if tData.GetEngine() != "rex" {
											continue
										}
										tData.Status = "missing"
										tData.LastSummary = "Rex session not found"
										pData.Tasks[tSlug] = tData
									}
								} else {
									windows, err := rexDriver.ListWindows(cmd.Context(), pData.RexSessionID)
									if err == nil {
										winMap := make(map[string]bool)
										for _, w := range windows {
											winMap[w.WindowID] = true
										}
										for tSlug, tData := range pData.Tasks {
											if tData.GetEngine() != "rex" {
												continue
											}
											if !winMap[tData.WindowID] {
												tData.Status = "missing"
												tData.LastSummary = "Window closed in Rex"
												pData.Tasks[tSlug] = tData
											}
										}
									}
								}
								s.Projects[pName] = pData
							}
							return nil
						})
					}
				}
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

			cfg, _ := config.LoadConfig()
			res := engine.ResolveEngine("", os.Getenv, cfg)

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
			fmt.Printf("Host: %s | Engine: %s (source: %s)\n", res.HostInfo.Kind, res.EngineName, res.Source)
			fmt.Printf("%-16s %-20s %-8s %-8s %-16s %s\n", "PROJECT", "TASK", "AGENT", "ENGINE", "STATUS", "SUMMARY")
			fmt.Println(strings.Repeat("-", 80))
			for pName, pData := range projects {
				if len(pData.Tasks) == 0 {
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
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&formatStr, "format", "f", "json", "Output format: json, brief, table")
	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Filter by project name")
	cmd.Flags().BoolVar(&syncLive, "sync", false, "Reconcile cache with live Rex sessions and windows")

	return cmd
}
