package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(newTaskStartCmd())
	cmd.AddCommand(newTaskUpdateCmd())
	cmd.AddCommand(newTaskCloseCmd())
	cmd.AddCommand(newTaskCleanCmd())
	cmd.AddCommand(newTaskHistoryCmd())
	cmd.AddCommand(newTaskAdoptCmd())

	return cmd
}

func newTaskStartCmd() *cobra.Command {
	var (
		projectName string
		taskSlug    string
		agentName   string
		modelName   string
		prompt      string
		engineName  string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new feature task tab in project session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("missing required --project (-p) argument")
			}
			if taskSlug == "" {
				if prompt != "" {
					taskSlug = project.DeriveSlug("", prompt, projectName)
				} else {
					return fmt.Errorf("missing required --task (-t) argument")
				}
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}

			resEngine := engine.ResolveEngine(engineName, os.Getenv, cfg)
			resEngine.WarnUnknownHost(os.Stderr)
			engineName = resEngine.EngineName
			eng, err := getEngine(engineName)
			if err != nil {
				return err
			}

			reconcileActiveIfStale(cmd.Context(), engineName, defaultSyncTTL)

			st, err := state.LoadStateForEngine(engineName)
			if err != nil {
				return err
			}

			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				return err
			}

			slug := project.DeriveSlug(taskSlug, prompt, projName)

			if !force {
				if candidates := findCandidateCollisions(st, projName, slug, prompt); len(candidates) > 0 {
					return formatCandidateCollisionError(projName, slug, candidates)
				}
			}

			resolved, err := resolveWorkerWithModel(engineName, agentName, modelName, cfg)
			if err != nil {
				return err
			}

			res, err := eng.Dispatch(cmd.Context(), engine.DispatchRequest{
				ProjectName: projName,
				ProjectPath: projPath,
				TaskSlug:    slug,
				Prompt:      prompt,
				Model:       resolved.Model,
				AgentCmd:    resolved.AgentCmd,
			})
			if err != nil {
				return err
			}

			preview := "Started via CLI"
			if prompt != "" {
				preview = prompt
				if len(preview) > 60 {
					preview = preview[:60] + "..."
				}
			}

			var initialPrompt *string
			if prompt != "" {
				initialPrompt = &prompt
			}

			var updatedState *state.State
			err = state.MutateStateForEngine(engineName, func(s *state.State) error {
				s.RecordTask(projName, projPath, res.SessionID, projName, state.TaskState{
					TaskSlug:      slug,
					Engine:        engineName,
					WindowID:      res.WindowID,
					WindowLabel:   slug,
					AgentBlockID:  res.BlockID,
					Agent:         resolved.AgentLabel,
					Model:         resolved.Model,
					Cwd:           projPath,
					Status:        "running",
					Origin:        "buddy",
					InitialPrompt: initialPrompt,
					LastSummary:   preview,
				})
				updatedState = s
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to record task in state: %w", err)
			}

			recWatch := computeRecommendedWatch(updatedState)
			activeInProj := collectActiveTasksInProject(updatedState, projName)

			resp := map[string]any{
				"status":                  "started",
				"project":                 projName,
				"task":                    slug,
				"picked_up":               res.PickedUp,
				"window_id":               res.WindowID,
				"agent_block_id":          res.BlockID,
				"agent":                   resolved.AgentLabel,
				"active_tasks_in_project": activeInProj,
				"recommended_watch":       recWatch,
			}
			if resolved.Model != "" {
				resp["model"] = resolved.Model
			}
			if res.PickupReason != "" {
				resp["pickup_reason"] = res.PickupReason
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or path")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug (e.g. feat-auth)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Agent CLI to launch")
	cmd.Flags().StringVarP(&modelName, "model", "M", "", "Model override for the worker agent (e.g. gemini-4-neon-high or provider/model)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Initial prompt for worker agent")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, opencode, wezterm)")
	cmd.Flags().BoolVar(&force, "force", false, "Bypass candidate duplicate guard and start a new task tab")

	return cmd
}

func newTaskAdoptCmd() *cobra.Command {
	var (
		projectName string
		windowSpec  string
		taskSlug    string
		agentName   string
		engineName  string
	)

	cmd := &cobra.Command{
		Use:     "adopt",
		Aliases: []string{"attach"},
		Short:   "Adopt an existing untracked project window or block into Buddy state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" || windowSpec == "" {
				return fmt.Errorf("both --project (-p) and --window (-w) are required")
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}

			engName := resolveActiveEngine(engineName)
			var tb backend.TerminalBackend
			if engName == "rex" {
				tb = defaultRexBackend()
				_ = reconcileRexWithBackend(cmd.Context(), tb)
			} else if engName == "t3" {
				_ = syncT3Fn(cmd.Context())
			}

			st, err := state.LoadStateForEngine(engName)
			if err != nil {
				return err
			}

			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				if p, findErr := st.FindProject(projectName); findErr == nil {
					projName = p.Name
					projPath = p.ProjectPath
				} else {
					return err
				}
			}

			var sessID string
			if p, findErr := st.FindProject(projName); findErr == nil {
				sessID = p.RexSessionID
			}

			var matchedWin backend.WindowInfo
			var found bool
			var detectedStatus, detectedSummary string

			// 1. Check untracked windows/threads already discovered in state
			if p, findErr := st.FindProject(projName); findErr == nil {
				for _, uw := range p.UntrackedWindows {
					if uw.WindowID == windowSpec || strings.EqualFold(uw.Label, windowSpec) {
						matchedWin = backend.WindowInfo{
							WindowID:  uw.WindowID,
							Label:     uw.Label,
							SessionID: p.RexSessionID,
						}
						if uw.BlockID != "" {
							matchedWin.BlockIDs = []string{uw.BlockID}
						}
						detectedStatus = uw.DetectedStatus
						detectedSummary = uw.Summary
						found = true
						break
					}
				}
			}

			if engName == "t3" {
				var t3Eng *engine.T3Engine
				if eng, engErr := getEngine("t3"); engErr == nil {
					if cast, ok := eng.(*engine.T3Engine); ok {
						t3Eng = cast
					}
				}

				if !found && t3Eng != nil {
					if probe, probeErr := t3Eng.ProbeStatus(cmd.Context(), sessID, windowSpec); probeErr == nil {
						matchedWin = backend.WindowInfo{
							WindowID:  windowSpec,
							Label:     windowSpec,
							SessionID: sessID,
							BlockIDs:  []string{windowSpec},
						}
						if probe.Status != "" && probe.Status != "unknown" {
							detectedStatus = probe.Status
						}
						if probe.Summary != "" {
							detectedSummary = probe.Summary
						}
						found = true
					}
				} else if found && t3Eng != nil {
					if probe, probeErr := t3Eng.ProbeStatus(cmd.Context(), sessID, matchedWin.WindowID); probeErr == nil {
						if probe.Status != "" && probe.Status != "unknown" {
							detectedStatus = probe.Status
						}
						if probe.Summary != "" {
							detectedSummary = probe.Summary
						}
					}
				}

				if found && sessID == "" && t3Eng != nil && projPath != "" {
					if threads, listErr := t3Eng.ListProjectThreads(cmd.Context(), projPath); listErr == nil {
						for _, th := range threads {
							if th.ThreadID == matchedWin.WindowID && th.ProjectID != "" {
								sessID = th.ProjectID
								if matchedWin.Label == matchedWin.WindowID && th.Title != "" {
									matchedWin.Label = truncateT3Label(th.Title, 40)
								}
								break
							}
						}
					}
				}

				if !found {
					return fmt.Errorf("window %q not found in project %q", windowSpec, projName)
				}

				blockID := matchedWin.WindowID
				if len(matchedWin.BlockIDs) > 0 && matchedWin.BlockIDs[0] != "" {
					blockID = matchedWin.BlockIDs[0]
				}
				if detectedStatus == "" {
					detectedStatus = "running"
				}
				if detectedSummary == "" {
					detectedSummary = "Adopted T3 thread"
				}
				rawSlug := taskSlug
				if rawSlug == "" {
					rawSlug = matchedWin.Label
				}
				if rawSlug == "" {
					rawSlug = matchedWin.WindowID
				}
				slug := project.DeriveSlug(rawSlug, detectedSummary, projName)

				if agentName == "" {
					agentName = cfg.GetEffectiveWorkerAgent(engName)
					if agentName == "" {
						agentName = "agy"
					}
				}

				var updatedState *state.State
				err = state.MutateStateForEngine(engName, func(s *state.State) error {
					s.RecordTask(projName, projPath, sessID, projName, state.TaskState{
						TaskSlug:     slug,
						Engine:       engName,
						WindowID:     matchedWin.WindowID,
						WindowLabel:  matchedWin.Label,
						AgentBlockID: blockID,
						Agent:        agentName,
						Cwd:          projPath,
						Status:       detectedStatus,
						Origin:       "user",
						LastSummary:  detectedSummary,
					})
					if p, ok := s.Projects[projName]; ok && len(p.UntrackedWindows) > 0 {
						var remaining []state.UntrackedWindow
						for _, uw := range p.UntrackedWindows {
							if uw.WindowID != matchedWin.WindowID {
								remaining = append(remaining, uw)
							}
						}
						p.UntrackedWindows = remaining
						s.Projects[projName] = p
					}
					updatedState = s
					return nil
				})
				if err != nil {
					return err
				}

				recWatch := computeRecommendedWatch(updatedState)
				resp := map[string]any{
					"status":            "adopted",
					"project":           projName,
					"task":              slug,
					"origin":            "user",
					"task_status":       detectedStatus,
					"summary":           detectedSummary,
					"window_id":         matchedWin.WindowID,
					"agent_block_id":    blockID,
					"recommended_watch": recWatch,
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			if engName == "opencode" {
				var ocEng *engine.OpenCodeEngine
				if eng, engErr := getEngine("opencode"); engErr == nil {
					if cast, ok := eng.(*engine.OpenCodeEngine); ok {
						ocEng = cast
					}
				}

				if !found && ocEng != nil {
					if probe, probeErr := ocEng.ProbeStatus(cmd.Context(), sessID, windowSpec); probeErr == nil {
						matchedWin = backend.WindowInfo{
							WindowID:  windowSpec,
							Label:     windowSpec,
							SessionID: sessID,
							BlockIDs:  []string{windowSpec},
						}
						if probe.Status != "" && probe.Status != "unknown" {
							detectedStatus = probe.Status
						}
						if probe.Summary != "" {
							detectedSummary = probe.Summary
						}
						found = true
					}
				} else if found && ocEng != nil {
					if probe, probeErr := ocEng.ProbeStatus(cmd.Context(), sessID, matchedWin.WindowID); probeErr == nil {
						if probe.Status != "" && probe.Status != "unknown" {
							detectedStatus = probe.Status
						}
						if probe.Summary != "" {
							detectedSummary = probe.Summary
						}
					}
				}

				if (!found || sessID == "" || matchedWin.Label == matchedWin.WindowID) && ocEng != nil && projPath != "" {
					if sessions, listErr := ocEng.ListProjectSessions(cmd.Context(), projPath, 50); listErr == nil {
						for _, s := range sessions {
							if s.ID == windowSpec || (!found && strings.EqualFold(s.Title, windowSpec)) {
								if !found {
									matchedWin = backend.WindowInfo{
										WindowID:  s.ID,
										Label:     truncateT3Label(s.Title, 40),
										SessionID: s.ProjectID,
										BlockIDs:  []string{s.ID},
									}
									found = true
								}
								if sessID == "" && s.ProjectID != "" {
									sessID = s.ProjectID
								}
								if (matchedWin.Label == "" || matchedWin.Label == matchedWin.WindowID) && s.Title != "" {
									matchedWin.Label = truncateT3Label(s.Title, 40)
								}
								break
							}
						}
					}
				}

				if !found {
					return fmt.Errorf("session %q not found in project %q", windowSpec, projName)
				}

				blockID := matchedWin.WindowID
				if len(matchedWin.BlockIDs) > 0 && matchedWin.BlockIDs[0] != "" {
					blockID = matchedWin.BlockIDs[0]
				}
				if detectedStatus == "" {
					detectedStatus = "running"
				}
				if detectedSummary == "" {
					detectedSummary = "Adopted OpenCode session"
				}
				rawSlug := taskSlug
				if rawSlug == "" {
					rawSlug = matchedWin.Label
				}
				if rawSlug == "" {
					rawSlug = matchedWin.WindowID
				}
				slug := project.DeriveSlug(rawSlug, detectedSummary, projName)

				if agentName == "" {
					agentName = "opencode"
				}

				var updatedState *state.State
				err = state.MutateStateForEngine(engName, func(s *state.State) error {
					s.RecordTask(projName, projPath, sessID, projName, state.TaskState{
						TaskSlug:     slug,
						Engine:       engName,
						WindowID:     matchedWin.WindowID,
						WindowLabel:  matchedWin.Label,
						AgentBlockID: blockID,
						Agent:        agentName,
						Cwd:          projPath,
						Status:       detectedStatus,
						Origin:       "user",
						LastSummary:  detectedSummary,
					})
					if p, ok := s.Projects[projName]; ok && len(p.UntrackedWindows) > 0 {
						var remaining []state.UntrackedWindow
						for _, uw := range p.UntrackedWindows {
							if uw.WindowID != matchedWin.WindowID {
								remaining = append(remaining, uw)
							}
						}
						p.UntrackedWindows = remaining
						s.Projects[projName] = p
					}
					updatedState = s
					return nil
				})
				if err != nil {
					return err
				}

				recWatch := computeRecommendedWatch(updatedState)
				resp := map[string]any{
					"status":            "adopted",
					"project":           projName,
					"task":              slug,
					"origin":            "user",
					"task_status":       detectedStatus,
					"summary":           detectedSummary,
					"window_id":         matchedWin.WindowID,
					"agent_block_id":    blockID,
					"recommended_watch": recWatch,
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			// 2. Query live backend sessions/windows if not already in UntrackedWindows
			if !found && tb != nil && tb.IsAvailable(cmd.Context()) {
				if sessID == "" {
					if sessions, sErr := tb.ListSessions(cmd.Context()); sErr == nil {
						for _, s := range sessions {
							if strings.EqualFold(s.Name, projName) {
								sessID = s.SessionID
								break
							}
						}
					}
				}
				if sessID != "" {
					if windows, wErr := tb.ListWindows(cmd.Context(), sessID); wErr == nil {
						for _, w := range windows {
							if w.WindowID == windowSpec || strings.EqualFold(w.Label, windowSpec) {
								matchedWin = w
								found = true
								break
							}
						}
					}
				}
			}

			if !found {
				return fmt.Errorf("window %q not found in project %q", windowSpec, projName)
			}

			blockID := ""
			if len(matchedWin.BlockIDs) > 0 {
				blockID = matchedWin.BlockIDs[0]
			}
			if sessID == "" {
				sessID = matchedWin.SessionID
			}

			if tb != nil && blockID != "" {
				probedStatus, probedSummary, _ := probeLiveBlockStatus(cmd.Context(), tb, sessID, blockID, detectedStatus, detectedSummary)
				if probedStatus != "" {
					detectedStatus = probedStatus
				}
				if probedSummary != "" {
					detectedSummary = probedSummary
				}
			}
			if detectedStatus == "" {
				detectedStatus = "running"
			}
			if detectedSummary == "" {
				detectedSummary = "Adopted user tab"
			}

			rawSlug := taskSlug
			if rawSlug == "" {
				rawSlug = matchedWin.Label
			}
			if rawSlug == "" {
				rawSlug = matchedWin.WindowID
			}
			slug := project.DeriveSlug(rawSlug, detectedSummary, projName)

			if agentName == "" {
				agentName = cfg.GetEffectiveWorkerAgent(engName)
				if agentName == "" {
					agentName = "agy"
				}
			}

			var updatedState *state.State
			err = state.MutateStateForEngine(engName, func(s *state.State) error {
				s.RecordTask(projName, projPath, sessID, projName, state.TaskState{
					TaskSlug:     slug,
					Engine:       engName,
					WindowID:     matchedWin.WindowID,
					WindowLabel:  matchedWin.Label,
					AgentBlockID: blockID,
					Agent:        agentName,
					Cwd:          projPath,
					Status:       detectedStatus,
					Origin:       "user",
					LastSummary:  detectedSummary,
				})
				if p, ok := s.Projects[projName]; ok && len(p.UntrackedWindows) > 0 {
					var remaining []state.UntrackedWindow
					for _, uw := range p.UntrackedWindows {
						if uw.WindowID != matchedWin.WindowID {
							remaining = append(remaining, uw)
						}
					}
					p.UntrackedWindows = remaining
					s.Projects[projName] = p
				}
				updatedState = s
				return nil
			})
			if err != nil {
				return err
			}

			recWatch := computeRecommendedWatch(updatedState)
			resp := map[string]any{
				"status":            "adopted",
				"project":           projName,
				"task":              slug,
				"origin":            "user",
				"task_status":       detectedStatus,
				"summary":           detectedSummary,
				"window_id":         matchedWin.WindowID,
				"agent_block_id":    blockID,
				"recommended_watch": recWatch,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&windowSpec, "window", "w", "", "Window ID or label to adopt")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug to assign (defaults to window label)")
	cmd.Flags().StringVarP(&agentName, "agent", "a", "", "Worker agent running in the tab")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, opencode, wezterm)")

	return cmd
}

func newTaskUpdateCmd() *cobra.Command {
	var (
		project    string
		task       string
		status     string
		summary    string
		engineName string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update task status or summary in state cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" || task == "" {
				return fmt.Errorf("both --project (-p) and --task (-t) are required")
			}

			engName := resolveActiveEngine(engineName)
			var updatedProj, updatedTask string
			err := state.MutateStateForEngine(engName, func(s *state.State) error {
				p, err := s.FindProject(project)
				if err != nil {
					return err
				}
				updatedProj = p.Name

				t, err := p.FindTask(task)
				if err != nil {
					return err
				}
				updatedTask = t.TaskSlug

				if status != "" {
					t.Status = status
				}
				if summary != "" {
					t.LastSummary = summary
				}
				now := state.NowISO()
				t.UpdatedAt = now
				t.LastVerifiedAt = now

				p.Tasks[t.TaskSlug] = *t
				s.Projects[p.Name] = *p
				return nil
			})
			if err != nil {
				return err
			}

			resp := map[string]any{
				"status":  "ok",
				"project": updatedProj,
				"task":    updatedTask,
				"updated": map[string]any{
					"status":  status,
					"summary": summary,
				},
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&status, "status", "", "New task status")
	cmd.Flags().StringVar(&summary, "summary", "", "New task summary")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, opencode, wezterm)")

	return cmd
}

func newTaskCloseCmd() *cobra.Command {
	var (
		project    string
		task       string
		window     string
		session    string
		engineName string
	)

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close task tab in Rex and update state",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedEngine := resolveActiveEngine(engineName)
			var sessID, winID, projName, taskSlug, engName string
			var err error
			if project == "" && session == "" && window != "" && config.IsNonTerminalEngine(resolvedEngine) {
				winID = window
				engName = resolvedEngine
			} else {
				sessID, winID, _, projName, taskSlug, engName, err = resolveTargetWithEngine(project, task, session, window, "", engineName)
				if err != nil {
					return err
				}
			}
			if winID == "" || (!config.IsNonTerminalEngine(engName) && sessID == "") {
				return fmt.Errorf("missing session_id or window_id for task '%s'", task)
			}

			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			if err := eng.Close(cmd.Context(), sessID, winID); err != nil {
				return err
			}

			_ = state.MutateStateForEngine(engName, func(s *state.State) error {
				if projName != "" && taskSlug != "" {
					return s.UpdateTask(projName, taskSlug, "closed", "Task closed via CLI")
				}
				for pName, pData := range s.Projects {
					for tSlug, tData := range pData.Tasks {
						if tData.WindowID == winID {
							return s.UpdateTask(pName, tSlug, "closed", "Task closed via CLI")
						}
					}
				}
				return nil
			})

			resp := map[string]any{
				"status":    "closed",
				"project":   projName,
				"task":      taskSlug,
				"window_id": winID,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&window, "window", "w", "", "Direct window ID")
	cmd.Flags().StringVarP(&session, "session", "s", "", "Direct session ID")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, opencode, wezterm)")

	return cmd
}
