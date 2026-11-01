package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newStatusCmd() *cobra.Command {
	var (
		flagProject string
		flagSync    bool
		flagFormat  string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show tracked projects and tasks from cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadConfig()
			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			if rawProjects == nil {
				rawProjects = make(map[string]any)
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			if flagSync || len(rawProjects) == 0 {
				sessions, err := rexBackend.ListSessions(ctx)
				if err == nil {
					liveSessions := make(map[string]rex.RexSessionItem, len(sessions))
					for _, s := range sessions {
						liveSessions[s.SessionID] = rex.RexSessionItem{
							SessionID: s.SessionID,
							Label:     s.Name,
							Cwd:       s.Path,
						}
					}

					// 1. Verify tracked projects still exist in Rex
					for _, pVal := range rawProjects {
						pData, ok := pVal.(map[string]any)
						if !ok {
							continue
						}
						sID, _ := pData["rex_session_id"].(string)
						tasks, _ := pData["tasks"].(map[string]any)
						if tasks == nil {
							tasks = make(map[string]any)
							pData["tasks"] = tasks
						}

						if _, exists := liveSessions[sID]; !exists {
							for _, tVal := range tasks {
								if tData, ok := tVal.(map[string]any); ok {
									tData["status"] = "missing"
									tData["last_summary"] = "Rex session not found"
								}
							}
						} else {
							// Check windows
							windows, err := rexBackend.ListWindows(ctx, sID)
							if err == nil {
								winIDs := make(map[string]bool, len(windows))
								for _, w := range windows {
									winIDs[w.WindowID] = true
								}
								for _, tVal := range tasks {
									if tData, ok := tVal.(map[string]any); ok {
										wID, _ := tData["window_id"].(string)
										if !winIDs[wID] {
											tData["status"] = "missing"
											tData["last_summary"] = "Window closed in Rex"
										}
									}
								}
							}
						}
					}

					// 2. Discover untracked live Rex sessions matching project roots
					buddySessName := strings.ToLower(cfg.BuddySessionName)
					if buddySessName == "" {
						buddySessName = "buddy"
					}

					for sID, sInfo := range liveSessions {
						label := sInfo.Label
						if label == "" || strings.ToLower(label) == buddySessName {
							continue
						}

						pSlug := strings.ToLower(label)
						pSlug = strings.ReplaceAll(pSlug, "_", "-")
						pSlug = strings.ReplaceAll(pSlug, " ", "-")

						var matchedDir string
						var matchedName string

						for _, r := range cfg.ProjectsRoots {
							rootPath := r
							if strings.HasPrefix(rootPath, "~/") {
								home, _ := os.UserHomeDir()
								rootPath = filepath.Join(home, rootPath[2:])
							}
							entries, err := os.ReadDir(rootPath)
							if err != nil {
								continue
							}
							for _, child := range entries {
								if !child.IsDir() || strings.HasPrefix(child.Name(), ".") {
									continue
								}
								cSlug := strings.ToLower(child.Name())
								cSlug = strings.ReplaceAll(cSlug, "_", "-")
								cSlug = strings.ReplaceAll(cSlug, " ", "-")
								if cSlug == pSlug {
									matchedDir = filepath.Join(rootPath, child.Name())
									matchedName = child.Name()
									break
								}
							}
							if matchedDir != "" {
								break
							}
						}

						if matchedName != "" {
							if _, exists := rawProjects[matchedName]; !exists {
								newTasks := make(map[string]any)
								rawProjects[matchedName] = map[string]any{
									"name":              matchedName,
									"project_path":      matchedDir,
									"rex_session_id":    sID,
									"rex_session_label": label,
									"tasks":             newTasks,
								}

								windows, err := rexBackend.ListWindows(ctx, sID)
								if err == nil {
									for _, w := range windows {
										wLabel := w.Label
										wID := w.WindowID
										if wLabel != "" && !strings.HasPrefix(wLabel, "Window ") && !strings.EqualFold(wLabel, "shell") {
											focusedBlockID, _ := w.Extra["focused_block_id"].(string)
											newTasks[wLabel] = map[string]any{
												"task_slug":      wLabel,
												"window_id":      wID,
												"window_label":   wLabel,
												"agent_block_id": focusedBlockID,
												"dev_block_id":   nil,
												"agent":          cfg.GetEffectiveWorkerAgent(),
												"cwd":            matchedDir,
												"worktree_path":  nil,
												"branch":         nil,
												"status":         "running",
												"initial_prompt": nil,
												"last_summary":   "Discovered existing Rex window",
												"created_at":     state.NowISO(),
												"updated_at":     state.NowISO(),
											}
										}
									}
								}
							}
						}
					}

					rawState["projects"] = rawProjects
					_ = state.SaveRawState(rawState)
				}
			}

			// Filtering by project
			projectsMap := rawProjects
			if flagProject != "" {
				pName, _ := project.ResolveProject(flagProject, &cfg)
				if val, exists := rawProjects[pName]; exists {
					projectsMap = map[string]any{pName: val}
				} else {
					projectsMap = map[string]any{}
				}
			}

			format := strings.ToLower(flagFormat)
			if format == "" {
				format = "json"
			}

			if format == "json" {
				type StatusJSONOutput struct {
					Version        any `json:"version"`
					BuddySessionID any `json:"buddy_session_id"`
					UpdatedAt      any `json:"updated_at"`
					Projects       any `json:"projects"`
				}
				out := StatusJSONOutput{
					Version:        rawState["version"],
					BuddySessionID: rawState["buddy_session_id"],
					UpdatedAt:      rawState["updated_at"],
					Projects:       projectsMap,
				}
				jsonStr, err := formatters.FormatJSON(out)
				if err != nil {
					return err
				}
				fmt.Println(jsonStr)
				return nil
			}

			// For table and brief, unmarshal filtered projects into typed models
			st := state.LoadState()
			filteredTyped := make(map[string]state.ProjectNode)
			for pName := range projectsMap {
				if node, ok := st.Projects[pName]; ok {
					filteredTyped[pName] = node
				}
			}

			if format == "brief" {
				fmt.Println(formatters.FormatStatusBrief(filteredTyped))
				return nil
			}

			if format == "table" {
				fmt.Println(formatters.FormatStatusTable(filteredTyped))
				return nil
			}

			return fmt.Errorf("unknown format: %s", flagFormat)
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Filter by project name")
	cmd.Flags().BoolVar(&flagSync, "sync", false, "Reconcile cache with live Rex sessions")
	cmd.Flags().StringVarP(&flagFormat, "format", "f", "json", "Output format (json, table, brief)")

	return cmd
}
