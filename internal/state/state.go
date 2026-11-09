package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DevHostHandle represents an external dev server process or terminal window/tab
// running in the host terminal backend (Rex/Herdr) alongside a worker task.
type DevHostHandle struct {
	Backend   string `json:"backend,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	WindowID  string `json:"window_id,omitempty"`
	BlockID   string `json:"block_id,omitempty"`
}

type TaskState struct {
	TaskSlug      string         `json:"task_slug"`
	Engine        string         `json:"engine,omitempty"`
	WindowID      string         `json:"window_id"`
	WindowLabel   string         `json:"window_label"`
	AgentBlockID  string         `json:"agent_block_id"`
	DevBlockID    *string        `json:"dev_block_id,omitempty"`
	DevHostHandle *DevHostHandle `json:"dev_host_handle,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Cwd           string  `json:"cwd,omitempty"`
	WorktreePath  *string `json:"worktree_path,omitempty"`
	Branch        *string `json:"branch,omitempty"`
	Status        string  `json:"status"`
	InitialPrompt *string `json:"initial_prompt,omitempty"`
	LastSummary   string  `json:"last_summary,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
}

func (t *TaskState) GetEngine() string {
	if t.Engine != "" {
		return t.Engine
	}
	return "rex"
}

type ProjectState struct {
	Name            string               `json:"name"`
	ProjectPath     string               `json:"project_path"`
	RexSessionID    string               `json:"rex_session_id"`
	RexSessionLabel string               `json:"rex_session_label"`
	Tasks           map[string]TaskState `json:"tasks"`
}

type State struct {
	Version        int                     `json:"version"`
	BuddySessionID string                  `json:"buddy_session_id"`
	UpdatedAt      string                  `json:"updated_at"`
	Projects       map[string]ProjectState `json:"projects"`
}

// GetBuddyDir returns the base directory for buddy state and configuration.
func GetBuddyDir() string {
	if env := os.Getenv("BUDDY_DIR"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".buddy"
	}
	return filepath.Join(home, ".buddy")
}

// GetStatePath returns the path to state.json.
func GetStatePath() string {
	return filepath.Join(GetBuddyDir(), "state.json")
}

// LoadState reads state.json without modifying it.
func LoadState() (*State, error) {
	path := GetStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version:  1,
				Projects: make(map[string]ProjectState),
			}, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse state JSON: %w", err)
	}
	if s.Projects == nil {
		s.Projects = make(map[string]ProjectState)
	}
	return &s, nil
}

// FindProject looks up a project by exact match or case-insensitive prefix/substring.
func (s *State) FindProject(query string) (*ProjectState, error) {
	if len(s.Projects) == 0 {
		return nil, fmt.Errorf("no projects tracked in state")
	}

	// 1. Exact match
	if p, ok := s.Projects[query]; ok {
		return &p, nil
	}

	// 2. Case-insensitive exact match
	lowerQuery := strings.ToLower(query)
	for name, p := range s.Projects {
		if strings.ToLower(name) == lowerQuery {
			return &p, nil
		}
	}

	// 3. Substring match
	var matches []*ProjectState
	for name, p := range s.Projects {
		if strings.Contains(strings.ToLower(name), lowerQuery) {
			pCopy := p
			matches = append(matches, &pCopy)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		return nil, fmt.Errorf("ambiguous project '%s', matches: %s", query, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("project '%s' not found in state", query)
}

// FindTask finds a task within a project by slug or name.
func (p *ProjectState) FindTask(taskSlug string) (*TaskState, error) {
	if len(p.Tasks) == 0 {
		return nil, fmt.Errorf("no tasks in project '%s'", p.Name)
	}

	// Exact match
	if t, ok := p.Tasks[taskSlug]; ok {
		return &t, nil
	}

	// Case-insensitive match
	lower := strings.ToLower(taskSlug)
	for slug, t := range p.Tasks {
		if strings.ToLower(slug) == lower {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("task '%s' not found in project '%s'", taskSlug, p.Name)
}
