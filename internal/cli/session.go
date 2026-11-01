package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backends/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newEnsureSessionCmd() *cobra.Command {
	var flagProject string

	cmd := &cobra.Command{
		Use:   "ensure-session",
		Short: "Ensure a Rex session exists for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagProject == "" {
				return &ExitError{Code: 2, Err: fmt.Errorf("missing required --project argument")}
			}

			cfg := config.LoadConfig()
			projectName, projectPath := project.ResolveProject(flagProject, &cfg)

			rawState, err := state.LoadRawState()
			if err != nil {
				return err
			}

			rawProjects, _ := rawState["projects"].(map[string]any)
			if rawProjects == nil {
				rawProjects = make(map[string]any)
				rawState["projects"] = rawProjects
			}

			rexBackend := rex.NewRexBackend()
			ctx := context.Background()

			// 1. If recorded in state, verify it still exists
			var sessID string
			if projVal, exists := rawProjects[projectName]; exists {
				if projMap, ok := projVal.(map[string]any); ok {
					s, _ := projMap["rex_session_id"].(string)
					if s == "" {
						s, _ = projMap["session_id"].(string)
					}
					if s != "" {
						ref, err := rexBackend.InspectSession(ctx, s)
						if err == nil && ref.SessionID != "" {
							outJSON, _ := formatters.FormatJSON(map[string]any{
								"status":         "exists",
								"project":        projectName,
								"project_path":   projectPath,
								"rex_session_id": s,
							})
							fmt.Println(outJSON)
							return nil
						}
					}
				}
			}

			// 2. Check live Rex sessions by label
			sessions, err := rexBackend.ListSessions(ctx)
			if err == nil {
				for _, s := range sessions {
					if strings.EqualFold(s.Name, projectName) {
						sessID = s.SessionID
						if projVal, exists := rawProjects[projectName]; exists {
							if projMap, ok := projVal.(map[string]any); ok {
								projMap["rex_session_id"] = sessID
							}
						} else {
							rawProjects[projectName] = map[string]any{
								"name":              projectName,
								"project_path":      projectPath,
								"rex_session_id":    sessID,
								"rex_session_label": s.Name,
								"tasks":             make(map[string]any),
							}
						}
						_ = state.SaveRawState(rawState)
						outJSON, _ := formatters.FormatJSON(map[string]any{
							"status":         "adopted",
							"project":        projectName,
							"project_path":   projectPath,
							"rex_session_id": sessID,
						})
						fmt.Println(outJSON)
						return nil
					}
				}
			}

			// 3. Create new session in Rex
			ref, err := rexBackend.EnsureSession(ctx, projectName, projectPath)
			if err != nil {
				return err
			}
			sessID = ref.SessionID

			if projVal, exists := rawProjects[projectName]; exists {
				if projMap, ok := projVal.(map[string]any); ok {
					projMap["rex_session_id"] = sessID
				}
			} else {
				rawProjects[projectName] = map[string]any{
					"name":              projectName,
					"project_path":      projectPath,
					"rex_session_id":    sessID,
					"rex_session_label": projectName,
					"tasks":             make(map[string]any),
				}
			}
			_ = state.SaveRawState(rawState)

			outJSON, _ := formatters.FormatJSON(map[string]any{
				"status":         "created",
				"project":        projectName,
				"project_path":   projectPath,
				"rex_session_id": sessID,
			})
			fmt.Println(outJSON)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagProject, "project", "p", "", "Project name or directory path")
	_ = cmd.MarkFlagRequired("project")

	return cmd
}
