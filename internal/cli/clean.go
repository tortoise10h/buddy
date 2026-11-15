package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/state"
)

var errAlreadyCleaned = errors.New("task already cleaned")

func isCleanableStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "closed" || s == "done"
}

func removeSeenEntry(engineName, key string) {
	seenPath := state.GetStateSeenPathForEngine(engineName)
	data, err := os.ReadFile(seenPath)
	if err != nil {
		return
	}
	seen := make(map[string]seenTaskInfo)
	if err := json.Unmarshal(data, &seen); err != nil {
		return
	}
	if _, exists := seen[key]; !exists {
		return
	}
	delete(seen, key)
	if updated, err := json.MarshalIndent(seen, "", "  "); err == nil {
		_ = os.WriteFile(seenPath, updated, 0644)
	}
}

func newTaskCleanCmd() *cobra.Command {
	var (
		projectName       string
		taskSlug          string
		reason            string
		force             bool
		closeFirst        bool
		dryRun            bool
		pruneEmptyProject bool
		engineName        string
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove a closed task from Buddy's active engine state and append an audit record to task history.",
		Long: `Remove a closed task from Buddy's active engine state and append an audit record to task history.

By default, refuses to clean tasks whose status is not 'closed' or 'done'. Use --close to
close the task first, or --force to remove tracking state without closing the live window/thread.
Note: --force requires explicit user consent from the user in the current conversation.

Cleaning state only updates Buddy's local tracking (~/.buddy/state/<engine>.json) and history
log; it does NOT delete T3 threads from the T3 UI or remove git worktrees/branches.`,
		Example: `  buddy-ctl task clean -p buddy -t feat-auth --reason "merged PR #42"
  buddy-ctl task clean -p buddy -t feat-auth --close --reason "done and verified"
  buddy-ctl task clean -p buddy -t feat-auth --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" || taskSlug == "" {
				return fmt.Errorf("both --project (-p) and --task (-t) are required")
			}

			engName := resolveActiveEngine(engineName)
			if strings.TrimSpace(reason) == "" {
				reason = "cleaned via CLI"
			}

			if dryRun {
				return runCleanDryRun(cmd, engName, projectName, taskSlug, reason, force, closeFirst, pruneEmptyProject)
			}

			// If --close is requested and the task is not yet closed, close the live surface
			// before acquiring the state flock.
			closedViaFlag := false
			if closeFirst {
				st, err := state.LoadStateForEngine(engName)
				if err != nil {
					return err
				}
				if proj, pErr := st.FindProject(projectName); pErr == nil {
					if t, tErr := proj.FindTask(taskSlug); tErr == nil && !isCleanableStatus(t.Status) {
						targetEng := t.GetEngine()
						if t.WindowID != "" {
							eng, err := getEngine(targetEng)
							if err != nil {
								return err
							}
							if err := eng.Close(cmd.Context(), proj.RexSessionID, t.WindowID); err != nil {
								return err
							}
						}
						closedViaFlag = true
					}
				}
			}

			var (
				cleanedRec        state.HistoryRecord
				alreadyCleanedRec *state.HistoryRecord
				prunedProject     bool
			)

			err := state.MutateStateForEngine(engName, func(s *state.State) error {
				proj, pErr := s.FindProject(projectName)
				if pErr != nil {
					if rec, hErr := state.FindRecentHistoryRecord(projectName, taskSlug, engName); hErr == nil && rec != nil {
						alreadyCleanedRec = rec
						return errAlreadyCleaned
					}
					return fmt.Errorf("task '%s' not found in project '%s' (and no history record exists)", taskSlug, projectName)
				}

				t, tErr := proj.FindTask(taskSlug)
				if tErr != nil {
					if rec, hErr := state.FindRecentHistoryRecord(proj.Name, taskSlug, engName); hErr == nil && rec != nil {
						alreadyCleanedRec = rec
						return errAlreadyCleaned
					}
					return fmt.Errorf("task '%s' not found in project '%s' (and no history record exists)", taskSlug, proj.Name)
				}

				if closedViaFlag && !isCleanableStatus(t.Status) {
					t.Status = "closed"
					t.LastSummary = "Task closed via CLI"
					t.UpdatedAt = state.NowISO()
				}

				wasActive := !isCleanableStatus(t.Status)
				if wasActive && !force {
					return fmt.Errorf("task '%s/%s' has status '%s' (not closed); run 'buddy-ctl task close -p %s -t %s' first, or pass --close / --force",
						proj.Name, t.TaskSlug, t.Status, proj.Name, t.TaskSlug)
				}

				wasForced := force && wasActive
				if wasForced {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"WARN: force-cleaning non-closed task '%s/%s' (status=%s, reason=%q) without closing live surface\n",
						proj.Name, t.TaskSlug, t.Status, reason)
				}

				rec := state.NewHistoryRecord(*proj, *t, reason, wasForced)
				if err := state.AppendHistoryRecord(rec); err != nil {
					return err
				}
				cleanedRec = rec

				delete(proj.Tasks, t.TaskSlug)
				if pruneEmptyProject && len(proj.Tasks) == 0 && len(proj.UntrackedWindows) == 0 {
					delete(s.Projects, proj.Name)
					prunedProject = true
				} else {
					s.Projects[proj.Name] = *proj
				}

				removeSeenEntry(engName, proj.Name+"/"+t.TaskSlug)
				return nil
			})

			if errors.Is(err, errAlreadyCleaned) && alreadyCleanedRec != nil {
				resp := map[string]any{
					"status":     "already_cleaned",
					"project":    alreadyCleanedRec.Project,
					"task":       alreadyCleanedRec.TaskSlug,
					"engine":     alreadyCleanedRec.Engine,
					"cleaned_at": alreadyCleanedRec.Ts,
					"cleanup_id": alreadyCleanedRec.CleanupID,
				}
				return encodeJSON(cmd, resp)
			}
			if err != nil {
				return err
			}

			resp := map[string]any{
				"status":         "cleaned",
				"project":        cleanedRec.Project,
				"task":           cleanedRec.TaskSlug,
				"engine":         cleanedRec.Engine,
				"final_status":   cleanedRec.FinalStatus,
				"forced":         cleanedRec.Forced,
				"pruned_project": prunedProject,
				"cleanup_id":     cleanedRec.CleanupID,
				"history_path":   state.GetHistoryPath(),
			}
			return encodeJSON(cmd, resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or path")
	cmd.Flags().StringVarP(&taskSlug, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&reason, "reason", "r", "cleaned via CLI", "Brief reason for cleanup recorded in history")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Allow cleaning non-closed tasks without closing live surface (requires explicit user consent)")
	cmd.Flags().BoolVar(&closeFirst, "close", false, "Close the task via the engine before cleaning if not already closed")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be cleaned without modifying state or history")
	cmd.Flags().BoolVar(&pruneEmptyProject, "prune-empty-project", false, "Remove the project entry from state when no tasks or untracked windows remain")
	cmd.Flags().StringVar(&engineName, "engine", "", "Execution engine (rex, t3, wezterm)")

	return cmd
}

func runCleanDryRun(cmd *cobra.Command, engName, projectName, taskSlug, reason string, force, closeFirst, pruneEmptyProject bool) error {
	st, err := state.LoadStateForEngine(engName)
	if err != nil {
		return err
	}
	proj, pErr := st.FindProject(projectName)
	if pErr != nil {
		if rec, hErr := state.FindRecentHistoryRecord(projectName, taskSlug, engName); hErr == nil && rec != nil {
			return encodeJSON(cmd, map[string]any{
				"status":     "already_cleaned",
				"dry_run":    true,
				"project":    rec.Project,
				"task":       rec.TaskSlug,
				"engine":     rec.Engine,
				"cleaned_at": rec.Ts,
				"cleanup_id": rec.CleanupID,
			})
		}
		return fmt.Errorf("task '%s' not found in project '%s' (and no history record exists)", taskSlug, projectName)
	}

	t, tErr := proj.FindTask(taskSlug)
	if tErr != nil {
		if rec, hErr := state.FindRecentHistoryRecord(proj.Name, taskSlug, engName); hErr == nil && rec != nil {
			return encodeJSON(cmd, map[string]any{
				"status":     "already_cleaned",
				"dry_run":    true,
				"project":    rec.Project,
				"task":       rec.TaskSlug,
				"engine":     rec.Engine,
				"cleaned_at": rec.Ts,
				"cleanup_id": rec.CleanupID,
			})
		}
		return fmt.Errorf("task '%s' not found in project '%s' (and no history record exists)", taskSlug, proj.Name)
	}

	wasActive := !isCleanableStatus(t.Status)
	if wasActive && !force && !closeFirst {
		return fmt.Errorf("task '%s/%s' has status '%s' (not closed); run 'buddy-ctl task close -p %s -t %s' first, or pass --close / --force",
			proj.Name, t.TaskSlug, t.Status, proj.Name, t.TaskSlug)
	}

	rec := state.NewHistoryRecord(*proj, *t, reason, force && wasActive)
	if closeFirst && wasActive {
		rec.FinalStatus = "closed"
	}
	wouldPrune := pruneEmptyProject && len(proj.Tasks) <= 1 && len(proj.UntrackedWindows) == 0

	return encodeJSON(cmd, map[string]any{
		"status":              "dry_run",
		"dry_run":             true,
		"project":             proj.Name,
		"task":                t.TaskSlug,
		"engine":              engName,
		"would_prune_project": wouldPrune,
		"record":              rec,
	})
}

func encodeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
