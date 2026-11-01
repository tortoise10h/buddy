package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/delivery"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
	"github.com/tortoise10h/buddy/internal/task"
)

func newDispatchCmd() *cobra.Command {
	var (
		flagProject   string
		flagPrompt    string
		flagTask      string
		flagAgent     string
		flagPlanFirst bool
		flagWorktree  bool
		flagBranch    string
	)

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Fast-path atomic task dispatch: auto-slugs/uses slug, spawns, delivers prompt reactively, and verifies pickup in 1 shot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagPrompt == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Missing required --prompt argument",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			cfg := config.LoadConfig()
			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			projectName, projectPath := project.ResolveProject(flagProject, &cfg)

			var taskSlug string
			if strings.TrimSpace(flagTask) != "" {
				taskSlug = task.SanitizeSlug(flagTask)
			} else {
				taskSlug = task.DeriveTaskSlug(flagPrompt, projectName)
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			sessID, err := ensureProjectSession(ctx, rexBackend, projectName, projectPath, rawState)
			if err != nil {
				return err
			}

			agentName := flagAgent
			if agentName == "" {
				agentName = cfg.GetEffectiveWorkerAgent()
			}

			agentInfo, ok := cfg.Agents[agentName]
			if !ok {
				agentInfo = config.AgentConfig{
					Command:       []string{agentName},
					SubmitDelayMS: 1500,
					SubmitKey:     "enter",
				}
			}

			agentCmd := agentInfo.Command
			if len(agentCmd) == 0 {
				agentCmd = []string{agentName}
			}
			submitDelayMS := agentInfo.SubmitDelayMS
			if submitDelayMS == 0 {
				submitDelayMS = 1500
			}
			submitKey := agentInfo.SubmitKey
			if submitKey == "" {
				submitKey = "enter"
			}

			useWorktree := flagWorktree || cfg.UseWorktreeByDefault
			cwd := projectPath
			var wtPath *string
			var branch *string
			if flagBranch != "" {
				b := flagBranch
				branch = &b
			}

			if useWorktree {
				parentDir := filepath.Dir(projectPath)
				wt := filepath.Join(parentDir, fmt.Sprintf("%s-wt-%s", filepath.Base(projectPath), taskSlug))
				wtPath = &wt
				if branch == nil {
					b := fmt.Sprintf("feat/%s", taskSlug)
					branch = &b
				}
				if _, err := os.Stat(wt); os.IsNotExist(err) {
					gitCmd := exec.Command("git", "-C", projectPath, "worktree", "add", "-b", *branch, wt)
					if err := gitCmd.Run(); err != nil {
						fallbackCmd := exec.Command("git", "-C", projectPath, "worktree", "add", wt, *branch)
						_ = fallbackCmd.Run()
					}
				}
				cwd = wt
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			if rawProjects == nil {
				rawProjects = make(map[string]any)
				rawState["projects"] = rawProjects
			}

			projVal, ok := rawProjects[projectName]
			if !ok {
				projVal = map[string]any{
					"name":              projectName,
					"project_path":      projectPath,
					"rex_session_id":    sessID,
					"rex_session_label": projectName,
					"tasks":             make(map[string]any),
				}
				rawProjects[projectName] = projVal
			}

			projMap, _ := projVal.(map[string]any)
			projTasks, _ := projMap["tasks"].(map[string]any)
			if projTasks == nil {
				projTasks = make(map[string]any)
				projMap["tasks"] = projTasks
			}

			// Check if task already exists and is live
			if existingVal, exists := projTasks[taskSlug]; exists {
				existing, _ := existingVal.(map[string]any)
				wID, _ := existing["window_id"].(string)
				if wID != "" {
					winRef, inspectErr := rexBackend.InspectWindow(ctx, sessID, wID)
					if inspectErr == nil && winRef.WindowID != "" {
						outJSON, _ := formatters.FormatJSON(map[string]any{
							"status":         "already_running",
							"project":        projectName,
							"task":           taskSlug,
							"window_id":      wID,
							"agent_block_id": existing["agent_block_id"],
						})
						fmt.Println(outJSON)
						return nil
					}
				}
			}

			// Create new window in Rex
			sessionRef := backends.SessionRef{
				SessionID: sessID,
				Name:      projectName,
				Path:      cwd,
			}
			winRef, blockRef, err := rexBackend.CreateTaskWindow(ctx, sessionRef, taskSlug, cwd, agentCmd)
			if err != nil {
				return err
			}

			now := state.NowISO()
			taskEntry := map[string]any{
				"task_slug":      taskSlug,
				"window_id":      winRef.WindowID,
				"window_label":   taskSlug,
				"agent_block_id": blockRef.BlockID,
				"dev_block_id":   nil,
				"agent":          agentName,
				"cwd":            cwd,
				"worktree_path":  wtPath,
				"branch":         branch,
				"status":         "starting",
				"initial_prompt": flagPrompt,
				"last_summary":   "Task dispatched",
				"created_at":     now,
				"updated_at":     now,
			}
			projTasks[taskSlug] = taskEntry
			_ = state.SaveRawState(rawState)

			pickedUp := false
			preview := ""
			if blockRef.BlockID != "" {
				promptText := flagPrompt
				if flagPlanFirst {
					promptText = fmt.Sprintf(
						"Please inspect the codebase and create a detailed implementation plan for the following task. Before writing any code, outline your plan and list any clarifying questions or trade-offs for the user.\n\nTask: %s",
						flagPrompt,
					)
				}

				pickedUp, preview = delivery.SendPromptSmart(
					ctx,
					rexBackend,
					sessID,
					blockRef.BlockID,
					promptText,
					agentName,
					submitKey,
					submitDelayMS,
				)

				if flagPlanFirst {
					taskEntry["status"] = "planning"
				} else {
					taskEntry["status"] = "running"
				}
				if preview != "" {
					taskEntry["last_summary"] = preview
				} else {
					taskEntry["last_summary"] = "Prompt delivered"
				}
				taskEntry["updated_at"] = state.NowISO()
				_ = state.SaveRawState(rawState)
			}

			type DispatchJSONOutput struct {
				Status       string `json:"status"`
				Project      string `json:"project"`
				Task         string `json:"task"`
				PickedUp     bool   `json:"picked_up"`
				Preview      string `json:"preview"`
				WindowID     string `json:"window_id"`
				AgentBlockID string `json:"agent_block_id"`
				Agent        string `json:"agent"`
			}

			outObj := DispatchJSONOutput{
				Status:       "dispatched",
				Project:      projectName,
				Task:         taskSlug,
				PickedUp:     pickedUp,
				Preview:      preview,
				WindowID:     winRef.WindowID,
				AgentBlockID: blockRef.BlockID,
				Agent:        agentName,
			}

			outJSON, err := formatters.FormatJSON(outObj)
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name or directory path")
	cmd.Flags().StringVarP(&flagPrompt, "prompt", "m", "", "Task prompt to execute")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&flagAgent, "agent", "a", "", "Agent CLI to launch")
	cmd.Flags().BoolVar(&flagPlanFirst, "plan-first", false, "Wrap prompt to instruct worker agent to plan first")
	cmd.Flags().BoolVar(&flagWorktree, "worktree", false, "Create an isolated git worktree")
	cmd.Flags().StringVarP(&flagBranch, "branch", "b", "", "Branch name for worktree")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("prompt")

	return cmd
}

func ensureProjectSession(ctx context.Context, backend *rex.RexBackend, projectName, projectPath string, rawState map[string]any) (string, error) {
	rawProjects, _ := rawState["projects"].(map[string]any)
	if rawProjects == nil {
		rawProjects = make(map[string]any)
		rawState["projects"] = rawProjects
	}

	if projVal, exists := rawProjects[projectName]; exists {
		if projMap, ok := projVal.(map[string]any); ok {
			sID, _ := projMap["rex_session_id"].(string)
			if sID == "" {
				sID, _ = projMap["session_id"].(string)
			}
			if sID != "" {
				ref, err := backend.InspectSession(ctx, sID)
				if err == nil && ref.SessionID != "" {
					return sID, nil
				}
			}
		}
	}

	sessions, err := backend.ListSessions(ctx)
	if err == nil {
		for _, s := range sessions {
			if strings.EqualFold(s.Name, projectName) {
				sID := s.SessionID
				if projVal, exists := rawProjects[projectName]; exists {
					if projMap, ok := projVal.(map[string]any); ok {
						projMap["rex_session_id"] = sID
					}
				} else {
					rawProjects[projectName] = map[string]any{
						"name":              projectName,
						"project_path":      projectPath,
						"rex_session_id":    sID,
						"rex_session_label": projectName,
						"tasks":             make(map[string]any),
					}
				}
				_ = state.SaveRawState(rawState)
				return sID, nil
			}
		}
	}

	sessRef, err := backend.EnsureSession(ctx, projectName, projectPath)
	if err != nil {
		return "", err
	}

	sID := sessRef.SessionID
	if projVal, exists := rawProjects[projectName]; exists {
		if projMap, ok := projVal.(map[string]any); ok {
			projMap["rex_session_id"] = sID
		}
	} else {
		rawProjects[projectName] = map[string]any{
			"name":              projectName,
			"project_path":      projectPath,
			"rex_session_id":    sID,
			"rex_session_label": projectName,
			"tasks":             make(map[string]any),
		}
	}
	_ = state.SaveRawState(rawState)
	return sID, nil
}
