package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

func newDemoCmd() *cobra.Command {
	var (
		project   string
		task      string
		cmdStr    string
		focusOnly bool
	)

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Focus task tab in Rex and optionally launch dev server split",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, winID, blkID, projName, taskSlug, engName, err := resolveTarget(project, task, "", "", "")
			if err != nil {
				return err
			}
			if sessID == "" || winID == "" {
				return fmt.Errorf("missing session_id or window_id for task '%s'", task)
			}

			st, err := state.LoadState()
			if err != nil {
				return err
			}
			projState, err := st.FindProject(projName)
			if err != nil {
				return err
			}
			taskState, err := projState.FindTask(taskSlug)
			if err != nil {
				return err
			}

			isT3 := strings.EqualFold(engName, "t3code") || strings.EqualFold(engName, "t3")

			// Handle T3 Code tasks
			if isT3 {
				demoCwd := taskState.Cwd
				if taskState.WorktreePath != nil && *taskState.WorktreePath != "" {
					demoCwd = *taskState.WorktreePath
				}

				if cmdStr == "" || focusOnly {
					// Thread-level focus is unsupported for T3 Code; clean no-op report
					resp := map[string]any{
						"status":         "noop",
						"worker_backend": "t3code",
						"project":        projName,
						"task":           taskSlug,
						"thread_id":      winID,
						"cwd":            demoCwd,
						"reason":         "focus is unsupported for t3code tasks (t3 app <cwd> only focuses a project workspace, never a threadId); pass --cmd to launch a dev server in host terminal",
					}
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(resp)
				}

				// Cross-host dev server in host terminal backend (Rex)
				cfg, _ := config.LoadConfig()
				termBackendName := "rex"
				if cfg != nil {
					termBackendName = cfg.GetEffectiveTerminalBackend()
				}

				termDriver := rex.NewRexBackend()
				sessInfo, err := termDriver.EnsureSession(cmd.Context(), projName, projState.ProjectPath)
				if err != nil {
					return fmt.Errorf("failed to ensure host session in %s: %w", termBackendName, err)
				}

				var devWinID string
				var devBlkID string
				alreadyRunning := false
				if taskState.DevHostHandle != nil && taskState.DevHostHandle.WindowID != "" {
					if winInfo, err := termDriver.InspectWindow(cmd.Context(), taskState.DevHostHandle.SessionID, taskState.DevHostHandle.WindowID); err == nil && winInfo.WindowID != "" {
						devWinID = taskState.DevHostHandle.WindowID
						devBlkID = taskState.DevHostHandle.BlockID
						alreadyRunning = true
					}
				}

				if !alreadyRunning {
					demoLabel := fmt.Sprintf("%s-demo-%s", projName, taskSlug)
					wID, bID, err := termDriver.CreateTaskWindow(cmd.Context(), sessInfo.SessionID, demoLabel, demoCwd, []string{"sh", "-c", cmdStr})
					if err != nil {
						return fmt.Errorf("failed to launch dev server in %s: %w", termBackendName, err)
					}
					devWinID = wID
					devBlkID = bID

					devHandle := &state.DevHostHandle{
						Backend:   termBackendName,
						SessionID: sessInfo.SessionID,
						WindowID:  devWinID,
						BlockID:   devBlkID,
					}
					_ = state.MutateState(func(s *state.State) error {
						return s.UpdateTaskDevHandle(projName, taskSlug, devHandle)
					})
				}

				_ = termDriver.Focus(cmd.Context(), sessInfo.SessionID, devWinID)

				resp := map[string]any{
					"status":  "ok",
					"project": projName,
					"task":    taskSlug,
					"focused": true,
					"dev_host_handle": map[string]any{
						"backend":    termBackendName,
						"session_id": sessInfo.SessionID,
						"window_id":  devWinID,
						"block_id":   devBlkID,
					},
					"cwd": demoCwd,
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			// Standard Rex task
			eng, err := getEngine(engName)
			if err != nil {
				return err
			}
			if err := eng.Focus(cmd.Context(), sessID, winID); err != nil {
				return err
			}

			var devBlockID *string
			if cmdStr != "" && !focusOnly && blkID != "" {
				splitID, err := eng.OpenDevSplit(cmd.Context(), sessID, blkID, "", cmdStr)
				if err != nil {
					return fmt.Errorf("failed to open dev split: %w", err)
				}
				devBlockID = &splitID
			}

			resp := map[string]any{
				"status":    "ok",
				"project":   projName,
				"task":      taskSlug,
				"focused":   true,
				"dev_split": devBlockID,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&task, "task", "t", "", "Task slug")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "Command to run in dev split pane (e.g. 'npm run dev')")
	cmd.Flags().BoolVar(&focusOnly, "focus-only", false, "Only switch focus without launching dev split")

	return cmd
}
