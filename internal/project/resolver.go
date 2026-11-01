package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

type ProjectInfo struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	ActiveSession bool   `json:"active_session"`
	TasksCount    int    `json:"tasks_count"`
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// ResolveProject resolves a user query to an absolute project directory and canonical name.
func ResolveProject(query string, cfg *config.Config, st *state.State) (string, string, error) {
	if query == "" {
		return "", "", fmt.Errorf("project query cannot be empty")
	}

	// 1. Direct path check
	cleanPath := query
	if strings.HasPrefix(cleanPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cleanPath = filepath.Join(home, cleanPath[2:])
		}
	}
	if abs, err := filepath.Abs(cleanPath); err == nil {
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			return filepath.Base(abs), abs, nil
		}
	}

	cleanQuery := slugify(query)

	// 2. Check if tracked in state with existing project_path
	if st != nil {
		if p, err := st.FindProject(query); err == nil && p.ProjectPath != "" {
			if fi, err := os.Stat(p.ProjectPath); err == nil && fi.IsDir() {
				return p.Name, p.ProjectPath, nil
			}
		}
	}

	// 3. Scan configured project roots
	var roots []string
	if cfg != nil {
		roots = cfg.ProjectsRoots
	}

	var exactMatches [][2]string
	var candidates [][2]string

	for _, rawRoot := range roots {
		rootPath := rawRoot
		if strings.HasPrefix(rootPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				rootPath = filepath.Join(home, rootPath[2:])
			}
		}
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := entry.Name()
			entrySlug := slugify(name)
			fullPath := filepath.Join(rootPath, name)

			if entrySlug == cleanQuery {
				exactMatches = append(exactMatches, [2]string{name, fullPath})
			} else if strings.Contains(entrySlug, cleanQuery) {
				candidates = append(candidates, [2]string{name, fullPath})
			}
		}
	}

	if len(exactMatches) == 1 {
		return exactMatches[0][0], exactMatches[0][1], nil
	}
	if len(exactMatches) > 1 {
		return exactMatches[0][0], exactMatches[0][1], nil
	}

	if len(candidates) == 1 {
		return candidates[0][0], candidates[0][1], nil
	}
	if len(candidates) > 1 {
		names := make([]string, 0, len(candidates))
		for _, c := range candidates {
			names = append(names, c[0])
		}
		return "", "", fmt.Errorf("ambiguous project name '%s', matches: %s", query, strings.Join(names, ", "))
	}

	return "", "", fmt.Errorf("project '%s' not found across configured roots", query)
}

// ListProjects scans all configured project roots and maps them with live state.
func ListProjects(roots []string, query string, st *state.State) ([]ProjectInfo, error) {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	var result []ProjectInfo
	seenPaths := make(map[string]bool)

	for _, rawRoot := range roots {
		rootPath := rawRoot
		if strings.HasPrefix(rootPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				rootPath = filepath.Join(home, rootPath[2:])
			}
		}

		entries, err := os.ReadDir(rootPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			name := entry.Name()
			if lowerQuery != "" && !strings.Contains(strings.ToLower(name), lowerQuery) {
				continue
			}

			fullPath := filepath.Join(rootPath, name)
			if seenPaths[fullPath] {
				continue
			}
			seenPaths[fullPath] = true

			isActive := false
			tasksCount := 0
			if st != nil {
				for pName, pData := range st.Projects {
					if pData.ProjectPath == fullPath || pName == name {
						isActive = true
						tasksCount = len(pData.Tasks)
						break
					}
				}
			}

			result = append(result, ProjectInfo{
				Name:          name,
				Path:          fullPath,
				ActiveSession: isActive,
				TasksCount:    tasksCount,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	return result, nil
}

func AddRoot(cfg *config.Config, path string) (string, error) {
	cleanPath := path
	if strings.HasPrefix(cleanPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cleanPath = filepath.Join(home, cleanPath[2:])
		}
	}
	abs, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", err
	}

	for _, r := range cfg.ProjectsRoots {
		if r == abs || r == path {
			return abs, fmt.Errorf("root already exists: %s", abs)
		}
	}

	cfg.ProjectsRoots = append(cfg.ProjectsRoots, abs)
	if err := config.SaveConfig(cfg); err != nil {
		return abs, err
	}
	return abs, nil
}

func RemoveRoot(cfg *config.Config, path string) (string, error) {
	cleanPath := path
	if strings.HasPrefix(cleanPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cleanPath = filepath.Join(home, cleanPath[2:])
		}
	}
	abs, _ := filepath.Abs(cleanPath)

	newRoots := make([]string, 0, len(cfg.ProjectsRoots))
	found := false
	for _, r := range cfg.ProjectsRoots {
		if r == abs || r == path || r == cleanPath {
			found = true
			continue
		}
		newRoots = append(newRoots, r)
	}

	if !found {
		return path, fmt.Errorf("root not found: %s", path)
	}

	cfg.ProjectsRoots = newRoots
	if err := config.SaveConfig(cfg); err != nil {
		return path, err
	}
	return path, nil
}
