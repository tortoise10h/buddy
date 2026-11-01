package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/formatters"
	"github.com/tortoise10h/buddy/internal/state"
)

func newProjectsCmd() *cobra.Command {
	var flagQuery string

	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List available project directories or manage project roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadConfig()
			st := state.LoadState()

			query := strings.ToLower(strings.TrimSpace(flagQuery))
			type ProjectListItem struct {
				Name          string `json:"name"`
				Path          string `json:"path"`
				ActiveSession bool   `json:"active_session"`
				TasksCount    int    `json:"tasks_count"`
			}

			var projectsList []ProjectListItem

			for _, r := range cfg.ProjectsRoots {
				expanded := r
				if strings.HasPrefix(expanded, "~/") {
					home, _ := os.UserHomeDir()
					expanded = filepath.Join(home, expanded[2:])
				}
				entries, err := os.ReadDir(expanded)
				if err != nil {
					continue
				}

				var childNames []string
				for _, e := range entries {
					if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
						childNames = append(childNames, e.Name())
					}
				}
				sort.Strings(childNames)

				for _, name := range childNames {
					if query != "" && !strings.Contains(strings.ToLower(name), query) {
						continue
					}
					childPath := filepath.Join(expanded, name)
					isActive := false
					tasksCount := 0

					for pName, pData := range st.Projects {
						if pData.ProjectPath == childPath || (pName == name && pData.ProjectPath == "") {
							isActive = true
							tasksCount = len(pData.Tasks)
							break
						}
					}

					projectsList = append(projectsList, ProjectListItem{
						Name:          name,
						Path:          childPath,
						ActiveSession: isActive,
						TasksCount:    tasksCount,
					})
				}
			}

			if projectsList == nil {
				projectsList = []ProjectListItem{}
			}

			out := map[string]any{
				"projects": projectsList,
				"total":    len(projectsList),
			}

			jsonStr, err := formatters.FormatJSON(out)
			if err != nil {
				return err
			}
			fmt.Println(jsonStr)
			return nil
		},
	}

	cmd.Flags().StringVarP(&flagQuery, "query", "q", "", "Filter projects by substring")

	// Subcommands: add-root, remove-root, roots
	cmd.AddCommand(newProjectsAddRootCmd())
	cmd.AddCommand(newProjectsRemoveRootCmd())
	cmd.AddCommand(newProjectsRootsCmd())

	return cmd
}

func resolvePath(p string) string {
	expanded := p
	if strings.HasPrefix(expanded, "~/") {
		home, _ := os.UserHomeDir()
		expanded = filepath.Join(home, expanded[2:])
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return eval
	}
	return abs
}

func newProjectsAddRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-root <path>",
		Short: "Add a project root directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Missing PATH argument",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rawPath := args[0]
			absPath := resolvePath(rawPath)

			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Warning: Directory does not exist: %s\n", absPath)
			}

			rawCfg, err := config.LoadRawConfig()
			if err != nil {
				return err
			}

			var currentRoots []string
			if rootsVal, ok := rawCfg["projects_roots"].([]any); ok {
				for _, r := range rootsVal {
					if s, ok := r.(string); ok {
						currentRoots = append(currentRoots, s)
					}
				}
			}

			for _, r := range currentRoots {
				if resolvePath(r) == absPath {
					errJSON, _ := formatters.FormatJSON(map[string]any{
						"error":          fmt.Sprintf("Root already exists: %s", absPath),
						"projects_roots": currentRoots,
					})
					fmt.Fprintln(os.Stderr, errJSON)
					return &ExitError{Code: 1}
				}
			}

			currentRoots = append(currentRoots, absPath)
			var deduped []string
			seen := make(map[string]bool)
			for _, r := range currentRoots {
				res := resolvePath(r)
				if !seen[res] {
					seen[res] = true
					deduped = append(deduped, res)
				}
			}

			rawCfg["projects_roots"] = deduped
			if err := config.SaveRawConfig(rawCfg); err != nil {
				return err
			}

			out := map[string]any{
				"status":         "ok",
				"action":         "add-root",
				"added":          absPath,
				"projects_roots": deduped,
			}
			jsonStr, _ := formatters.FormatJSON(out)
			fmt.Println(jsonStr)
			return nil
		},
	}
}

func newProjectsRemoveRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-root <path>",
		Short: "Remove a project root directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				errJSON, _ := formatters.FormatJSON(map[string]string{
					"error": "Missing PATH argument",
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rawPath := args[0]
			absPath := resolvePath(rawPath)

			rawCfg, err := config.LoadRawConfig()
			if err != nil {
				return err
			}

			var currentRoots []string
			if rootsVal, ok := rawCfg["projects_roots"].([]any); ok {
				for _, r := range rootsVal {
					if s, ok := r.(string); ok {
						currentRoots = append(currentRoots, s)
					}
				}
			}

			found := false
			var newRoots []string
			for _, r := range currentRoots {
				res := resolvePath(r)
				if res == absPath || r == rawPath {
					found = true
				} else {
					newRoots = append(newRoots, r)
				}
			}

			if !found {
				errJSON, _ := formatters.FormatJSON(map[string]any{
					"error":          fmt.Sprintf("Root not found: %s", rawPath),
					"projects_roots": currentRoots,
				})
				fmt.Fprintln(os.Stderr, errJSON)
				return &ExitError{Code: 1}
			}

			rawCfg["projects_roots"] = newRoots
			if err := config.SaveRawConfig(rawCfg); err != nil {
				return err
			}

			out := map[string]any{
				"status":         "ok",
				"action":         "remove-root",
				"removed":        absPath,
				"projects_roots": newRoots,
			}
			jsonStr, _ := formatters.FormatJSON(out)
			fmt.Println(jsonStr)
			return nil
		},
	}
}

func newProjectsRootsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "roots",
		Short: "List configured project roots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.LoadConfig()
			type RootItem struct {
				Path   string `json:"path"`
				Exists bool   `json:"exists"`
			}

			var rootsList []RootItem
			var pathsList []string

			for _, r := range cfg.ProjectsRoots {
				abs := resolvePath(r)
				exists := false
				if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
					exists = true
				} else {
					fmt.Fprintf(os.Stderr, "Warning: Root directory does not exist: %s\n", abs)
				}
				rootsList = append(rootsList, RootItem{
					Path:   abs,
					Exists: exists,
				})
				pathsList = append(pathsList, abs)
			}

			if rootsList == nil {
				rootsList = []RootItem{}
				pathsList = []string{}
			}

			out := map[string]any{
				"projects_roots": pathsList,
				"roots":          rootsList,
			}
			jsonStr, _ := formatters.FormatJSON(out)
			fmt.Println(jsonStr)
			return nil
		},
	}
}
