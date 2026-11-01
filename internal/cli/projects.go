package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

func newProjectsCmd() *cobra.Command {
	var query string

	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List available project directories or manage project roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			st, _ := state.LoadState()

			projects, err := project.ListProjects(cfg.ProjectsRoots, query, st)
			if err != nil {
				return err
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(projects)
		},
	}

	cmd.Flags().StringVarP(&query, "query", "q", "", "Filter projects by substring")

	cmd.AddCommand(&cobra.Command{
		Use:   "roots",
		Short: "List configured project roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}

			type rootDetail struct {
				Path   string `json:"path"`
				Exists bool   `json:"exists"`
			}

			var details []rootDetail
			var paths []string
			for _, r := range cfg.ProjectsRoots {
				clean := r
				if strings.HasPrefix(clean, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						clean = filepath.Join(home, clean[2:])
					}
				}
				abs, _ := filepath.Abs(clean)
				_, err := os.Stat(abs)
				exists := err == nil

				details = append(details, rootDetail{
					Path:   abs,
					Exists: exists,
				})
				paths = append(paths, abs)
			}

			resp := map[string]any{
				"projects_roots": paths,
				"roots":          details,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add-root [path]",
		Short: "Add a project root directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			added, err := project.AddRoot(cfg, args[0])
			if err != nil {
				return err
			}
			resp := map[string]any{
				"status":         "ok",
				"action":         "add-root",
				"added":          added,
				"projects_roots": cfg.ProjectsRoots,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove-root [path]",
		Short: "Remove a project root directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return err
			}
			removed, err := project.RemoveRoot(cfg, args[0])
			if err != nil {
				return err
			}
			resp := map[string]any{
				"status":         "ok",
				"action":         "remove-root",
				"removed":        removed,
				"projects_roots": cfg.ProjectsRoots,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(resp)
		},
	})

	return cmd
}
