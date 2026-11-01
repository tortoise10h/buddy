package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/delivery"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
	"github.com/tortoise10h/buddy/internal/worktree"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management",
	}

	cmd.AddCommand(newTaskStartCmd())
	cmd.AddCommand(newTaskUpdateCmd())
	cmd.AddCommand(newTaskCloseCmd())

	return cmd
}

func newTaskStartCmd() *cobra.Command {
	var (
		flagProject   string
		flagTask      string
		flagAgent     string
		flagWorktree  bool
		flagBranch    string
		flagPrompt    string
		flagPlanFirst bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new feature task tab in project session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagTask == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --task argument")}
			}

			cfg := config.LoadConfig()
			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			projectName, projectPath := project.ResolveProject(flagProject, &cfg)
			taskSlug := strings.TrimSpace(flagTask)

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
				wt, b, _ := worktree.EnsureWorktree(projectPath, taskSlug, flagBranch)
				wtPath = &wt
				branch = &b
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
							"status": "already_running",
							"task":   existing,
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

			var initialPrompt any = nil
			if cmd.Flags().Changed("prompt") {
				initialPrompt = flagPrompt
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
				"initial_prompt": initialPrompt,
				"last_summary":   "Task started",
				"created_at":     now,
				"updated_at":     now,
			}
			projTasks[taskSlug] = taskEntry
			_ = state.SaveRawState(rawState)

			if flagPrompt != "" && blockRef.BlockID != "" {
				promptText := flagPrompt
				if flagPlanFirst {
					promptText = fmt.Sprintf(
						"Please inspect the codebase and create a detailed implementation plan for the following task. Before writing any code, outline your plan and list any clarifying questions or trade-offs for the user.\n\nTask: %s",
						flagPrompt,
					)
				}

				_, preview := delivery.SendPromptSmart(
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
					taskEntry["last_summary"] = "Initial prompt sent to worker agent"
				}
				taskEntry["updated_at"] = state.NowISO()
				_ = state.SaveRawState(rawState)
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":  "started",
				"project": projectName,
				"task":    taskEntry,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name or path")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug (e.g. feat-auth)")
	cmd.Flags().StringVarP(&flagAgent, "agent", "a", "", "Agent CLI to launch (agy, pi, opencode, etc.; default: config.worker_agent)")
	cmd.Flags().BoolVar(&flagWorktree, "worktree", false, "Create an isolated git worktree")
	cmd.Flags().StringVarP(&flagBranch, "branch", "b", "", "Branch name for worktree")
	cmd.Flags().StringVar(&flagPrompt, "prompt", "", "Initial prompt to send to the worker agent")
	cmd.Flags().BoolVar(&flagPlanFirst, "plan-first", false, "Wrap prompt to instruct worker agent to plan first")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}

func newTaskUpdateCmd() *cobra.Command {
	var (
		flagProject string
		flagTask    string
		flagStatus  string
		flagSummary string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update task status or summary in state cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagTask == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --task argument")}
			}

			cfg := config.LoadConfig()
			projectName, _ := project.ResolveProject(flagProject, &cfg)
			taskSlug := strings.TrimSpace(flagTask)

			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			projVal, ok := rawProjects[projectName]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Project '%s' not found", projectName),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			projMap, _ := projVal.(map[string]any)
			projTasks, _ := projMap["tasks"].(map[string]any)
			taskVal, ok := projTasks[taskSlug]
			if !ok {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": fmt.Sprintf("Task '%s' not found", taskSlug),
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			taskMap, _ := taskVal.(map[string]any)
			if flagStatus != "" {
				taskMap["status"] = flagStatus
			}
			if flagSummary != "" {
				taskMap["last_summary"] = flagSummary
			}
			taskMap["updated_at"] = state.NowISO()
			_ = state.SaveRawState(rawState)

			st, _ := taskMap["status"].(string)
			if st == "waiting_for_input" || st == "ready_for_demo" {
				statusLabel := "Input Needed"
				if st == "ready_for_demo" {
					statusLabel = "Ready for Demo"
				}
				msg, _ := taskMap["last_summary"].(string)
				if msg == "" {
					msg = fmt.Sprintf("Task is %s", st)
				}
				notify.SendDesktopNotification(
					"Buddy Orchestrator",
					fmt.Sprintf("%s/%s (%s)", projectName, taskSlug, statusLabel),
					msg,
					"Glass",
					&cfg,
				)
			}

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":  "updated",
				"project": projectName,
				"task":    taskMap,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug")
	cmd.Flags().StringVarP(&flagStatus, "status", "s", "", "Task status (starting, planning, waiting_for_input, running, ready_for_demo, done, missing)")
	cmd.Flags().StringVarP(&flagSummary, "summary", "m", "", "Brief 1-line summary of current status")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}

func newTaskCloseCmd() *cobra.Command {
	var (
		flagProject      string
		flagTask         string
		flagKeepWorktree bool
	)

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close task tab in Rex and update state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}
			if flagTask == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --task argument")}
			}

			cfg := config.LoadConfig()
			projectName, _ := project.ResolveProject(flagProject, &cfg)
			taskSlug := strings.TrimSpace(flagTask)

			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			projVal, ok := rawProjects[projectName]
			if !ok {
				outJSON, _ := formatters.FormatJSON(map[string]string{
					"status":  "not_found",
					"message": fmt.Sprintf("Project '%s' not in state", projectName),
				})
				fmt.Println(outJSON)
				return nil
			}

			projMap, _ := projVal.(map[string]any)
			projTasks, _ := projMap["tasks"].(map[string]any)
			taskVal, ok := projTasks[taskSlug]
			if !ok {
				outJSON, _ := formatters.FormatJSON(map[string]string{
					"status":  "not_found",
					"message": fmt.Sprintf("Task '%s' not in state", taskSlug),
				})
				fmt.Println(outJSON)
				return nil
			}

			taskMap, _ := taskVal.(map[string]any)
			sessID, _ := projMap["rex_session_id"].(string)
			if sessID == "" {
				sessID, _ = projMap["session_id"].(string)
			}
			winID, _ := taskMap["window_id"].(string)

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			if sessID != "" && winID != "" {
				_ = rexBackend.CloseWindow(ctx, sessID, winID)
			}

			delete(projTasks, taskSlug)
			_ = state.SaveRawState(rawState)

			outJSON, err := formatters.FormatJSON(map[string]any{
				"status":  "closed",
				"project": projectName,
				"task":    taskSlug,
			})
			if err != nil {
				return err
			}
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&flagTask, "task", "t", "", "Task slug")
	cmd.Flags().BoolVar(&flagKeepWorktree, "keep-worktree", false, "Do not delete git worktree if used")

	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("task")

	return cmd
}
