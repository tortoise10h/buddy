package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

type BatchTaskSpec struct {
	Project string `json:"project"`
	Task    string `json:"task"`
	Prompt  string `json:"prompt"`
	Agent   string `json:"agent,omitempty"`
	Engine  string `json:"engine,omitempty"`
}

type BatchTaskResult struct {
	Status       string `json:"status"`
	Project      string `json:"project,omitempty"`
	Task         string `json:"task,omitempty"`
	WindowID     string `json:"window_id,omitempty"`
	AgentBlockID string `json:"agent_block_id,omitempty"`
	PickedUp     bool   `json:"picked_up,omitempty"`
	PickupReason string `json:"pickup_reason,omitempty"`
	Error        string `json:"error,omitempty"`
}

func newBatchCmd() *cobra.Command {
	var (
		rawJSON  string
		filePath string
	)

	cmd := &cobra.Command{
		Use:   "batch",
		Short: "Batch dispatcher: Dispatches tasks to multiple projects concurrently or sequentially",
		RunE: func(cmd *cobra.Command, args []string) error {
			var rawBytes []byte
			if rawJSON != "" {
				rawBytes = []byte(rawJSON)
			} else if filePath != "" {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read batch file: %w", err)
				}
				rawBytes = data
			} else {
				return fmt.Errorf("provide --json (-j) or --file (-f) with array of task specs")
			}

			var specs []BatchTaskSpec
			if err := json.Unmarshal(rawBytes, &specs); err != nil {
				return fmt.Errorf("invalid batch JSON: %w (must be array of task objects)", err)
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				cfg = config.DefaultConfig()
			}
			stateByEngine := make(map[string]*state.State)
			getStateForEngine := func(engName string) *state.State {
				if st, ok := stateByEngine[engName]; ok {
					return st
				}
				st, err := state.LoadStateForEngine(engName)
				if err != nil {
					st = &state.State{}
				}
				stateByEngine[engName] = st
				return st
			}

			var results []BatchTaskResult
			successCount := 0
			failCount := 0

			for _, spec := range specs {
				if spec.Project == "" {
					results = append(results, BatchTaskResult{
						Status: "error",
						Error:  "missing required 'project' in spec",
					})
					failCount++
					continue
				}

				resEngine := engine.ResolveEngine(spec.Engine, os.Getenv, cfg)
				resEngine.WarnUnknownHost(os.Stderr)
				engineName := resEngine.EngineName

				projName, projPath, err := project.ResolveProject(spec.Project, cfg, getStateForEngine(engineName))
				if err != nil {
					results = append(results, BatchTaskResult{
						Status:  "error",
						Project: spec.Project,
						Error:   fmt.Sprintf("project resolution failed: %v", err),
					})
					failCount++
					continue
				}

				slug := project.DeriveSlug(spec.Task, spec.Prompt, projName)

				eng, err := getEngine(engineName)
				if err != nil {
					results = append(results, BatchTaskResult{
						Status:  "error",
						Project: projName,
						Task:    slug,
						Error:   fmt.Sprintf("engine lookup failed: %v", err),
					})
					failCount++
					continue
				}

				agentName, agentCmd, err := resolveWorkerForEngine(engineName, spec.Agent, cfg)
				if err != nil {
					results = append(results, BatchTaskResult{
						Status:  "error",
						Project: projName,
						Task:    slug,
						Error:   err.Error(),
					})
					failCount++
					continue
				}

				res, err := eng.Dispatch(cmd.Context(), engine.DispatchRequest{
					ProjectName: projName,
					ProjectPath: projPath,
					TaskSlug:    slug,
					Prompt:      spec.Prompt,
					AgentCmd:    agentCmd,
				})
				if err != nil {
					results = append(results, BatchTaskResult{
						Status:  "error",
						Project: projName,
						Task:    slug,
						Error:   fmt.Sprintf("dispatch failed: %v", err),
					})
					failCount++
					continue
				}

				preview := spec.Prompt
				if len(preview) > 60 {
					preview = preview[:60] + "..."
				}

				var updatedState *state.State
				_ = state.MutateStateForEngine(engineName, func(s *state.State) error {
					var initialPrompt *string
					if spec.Prompt != "" {
						initialPrompt = &spec.Prompt
					}
					s.RecordTask(projName, projPath, res.SessionID, projName, state.TaskState{
						TaskSlug:      slug,
						Engine:        engineName,
						WindowID:      res.WindowID,
						WindowLabel:   slug,
						AgentBlockID:  res.BlockID,
						Agent:         agentName,
						Cwd:           projPath,
						Status:        "running",
						Origin:        "buddy",
						InitialPrompt: initialPrompt,
						LastSummary:   preview,
					})
					updatedState = s
					return nil
				})
				if updatedState != nil {
					stateByEngine[engineName] = updatedState
				}

				results = append(results, BatchTaskResult{
					Status:       "dispatched",
					Project:      projName,
					Task:         slug,
					WindowID:     res.WindowID,
					AgentBlockID: res.BlockID,
					PickedUp:     res.PickedUp,
					PickupReason: res.PickupReason,
				})
				successCount++
			}

			var recWatch string
			for _, st := range stateByEngine {
				if rw := computeRecommendedWatch(st); rw != "" {
					recWatch = rw
					if strings.Contains(rw, "--all") {
						break
					}
				}
			}

			resp := map[string]any{
				"status":            "ok",
				"dispatched_count":  successCount,
				"failed_count":      failCount,
				"results":           results,
				"recommended_watch": recWatch,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&rawJSON, "json", "j", "", "JSON array of task specs")
	cmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to file containing JSON array of task specs")

	return cmd
}
