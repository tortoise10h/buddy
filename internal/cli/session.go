package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/backend/rex"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newEnsureSessionCmd() *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "ensure-session",
		Short: "Ensure a Rex session exists for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				if len(args) > 0 {
					projectName = args[0]
				} else {
					return fmt.Errorf("missing required --project (-p) argument")
				}
			}

			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, err := state.LoadStateForEngine("rex")
			if err != nil {
				return err
			}

			projName, projPath, err := project.ResolveProject(projectName, cfg, st)
			if err != nil {
				return err
			}

			rexDriver := rex.NewRexBackend()
			sess, err := rexDriver.EnsureSession(cmd.Context(), projName, projPath)
			if err != nil {
				return fmt.Errorf("failed to ensure session: %w", err)
			}

			status := "exists"
			err = state.MutateStateForEngine("rex", func(s *state.State) error {
				if p, ok := s.Projects[projName]; ok {
					if p.RexSessionID == "" {
						status = "adopted"
					}
					p.RexSessionID = sess.SessionID
					p.ProjectPath = projPath
					s.Projects[projName] = p
				} else {
					status = "created"
					s.Projects[projName] = state.ProjectState{
						Name:            projName,
						ProjectPath:     projPath,
						RexSessionID:    sess.SessionID,
						RexSessionLabel: sess.Name,
						Tasks:           make(map[string]state.TaskState),
					}
				}
				return nil
			})
			if err != nil {
				return err
			}

			resp := map[string]any{
				"status":         status,
				"project":        projName,
				"project_path":   projPath,
				"rex_session_id": sess.SessionID,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	}

	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name or path")

	return cmd
}
