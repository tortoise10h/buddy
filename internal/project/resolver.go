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

type ProjectMatch struct {
	Name string
	Path string
}

// ResolveProject resolves a project query to (project_name, project_path).
func ResolveProject(query string, cfg *config.Config) (string, string) {
	if cfg == nil {
		c := config.LoadConfig()
		cfg = &c
	}

	// 1. If query is an existing directory path
	expandedQuery := expandPath(query)
	fi, err := os.Stat(expandedQuery)
	if err == nil && fi.IsDir() {
		resolved, err := filepath.EvalSymlinks(expandedQuery)
		if err == nil {
			return filepath.Base(resolved), resolved
		}
		return filepath.Base(expandedQuery), expandedQuery
	}

	cleanQuery := strings.ToLower(strings.TrimSpace(query))
	cleanQuery = strings.ReplaceAll(cleanQuery, "_", "-")
	cleanQuery = strings.ReplaceAll(cleanQuery, " ", "-")

	// 2. Check roots in config
	var exactMatches []ProjectMatch
	var candidates []ProjectMatch

	for _, r := range cfg.ProjectsRoots {
		root := expandPath(r)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			cName := e.Name()
			cSlug := strings.ToLower(cName)
			cSlug = strings.ReplaceAll(cSlug, "_", "-")
			cSlug = strings.ReplaceAll(cSlug, " ", "-")
			childPath := filepath.Join(root, cName)

			if cSlug == cleanQuery {
				exactMatches = append(exactMatches, ProjectMatch{Name: cName, Path: childPath})
			} else if strings.Contains(cSlug, cleanQuery) {
				candidates = append(candidates, ProjectMatch{Name: cName, Path: childPath})
			}
		}
	}

	if len(exactMatches) >= 1 {
		if len(exactMatches) > 1 {
			var paths []string
			for _, m := range exactMatches {
				paths = append(paths, m.Path)
			}
			fmt.Fprintf(os.Stderr, "Warning: Ambiguous project name '%s' matches multiple roots: %v. Using %s.\n",
				query, paths, exactMatches[0].Path)
		}
		return exactMatches[0].Name, exactMatches[0].Path
	}

	if len(candidates) >= 1 {
		if len(candidates) > 1 {
			sort.Slice(candidates, func(i, j int) bool {
				return len(candidates[i].Name) < len(candidates[j].Name)
			})
			var paths []string
			for _, c := range candidates {
				paths = append(paths, c.Path)
			}
			fmt.Fprintf(os.Stderr, "Warning: Ambiguous project query '%s' matches multiple projects: %v. Using %s.\n",
				query, paths, candidates[0].Path)
		}
		return candidates[0].Name, candidates[0].Path
	}

	// 3. Check existing projects in state
	st := state.LoadState()
	for projName, projData := range st.Projects {
		pSlug := strings.ToLower(projName)
		pSlug = strings.ReplaceAll(pSlug, "_", "-")
		pSlug = strings.ReplaceAll(pSlug, " ", "-")
		if pSlug == cleanQuery {
			pPath := projData.ProjectPath
			if pPath == "" && len(cfg.ProjectsRoots) > 0 {
				pPath = filepath.Join(expandPath(cfg.ProjectsRoots[0]), projName)
			}
			return projName, pPath
		}
	}

	// Fallback to first root / query
	if len(cfg.ProjectsRoots) > 0 {
		return cleanQuery, filepath.Join(expandPath(cfg.ProjectsRoots[0]), cleanQuery)
	}

	return cleanQuery, cleanQuery
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.Getenv("HOME")
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
