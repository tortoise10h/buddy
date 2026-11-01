package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tortoise10h/buddy/internal/config"
)

type TaskNode struct {
	TaskSlug      string  `json:"task_slug"`
	Backend       string  `json:"backend,omitempty"`
	WindowID      string  `json:"window_id,omitempty"`
	WindowLabel   string  `json:"window_label,omitempty"`
	AgentBlockID  string  `json:"agent_block_id,omitempty"`
	DevBlockID    *string `json:"dev_block_id"`
	Agent         string  `json:"agent,omitempty"`
	Cwd           string  `json:"cwd,omitempty"`
	WorktreePath  *string `json:"worktree_path"`
	WorktreeDir   string  `json:"worktree_dir,omitempty"`
	Branch        *string `json:"branch"`
	Status        string  `json:"status"`
	InitialPrompt *string `json:"initial_prompt"`
	LastSummary   string  `json:"last_summary"`
	Summary       string  `json:"summary,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
}

type ProjectNode struct {
	Name            string              `json:"name"`
	Backend         string              `json:"backend,omitempty"`
	ProjectPath     string              `json:"project_path"`
	SessionID       string              `json:"session_id,omitempty"`
	RexSessionID    string              `json:"rex_session_id,omitempty"`
	RexSessionLabel string              `json:"rex_session_label,omitempty"`
	Tasks           map[string]TaskNode `json:"tasks"`
}

type State struct {
	Version        int                    `json:"version"`
	BuddySessionID *string                `json:"buddy_session_id"`
	UpdatedAt      string                 `json:"updated_at,omitempty"`
	Projects       map[string]ProjectNode `json:"projects"`
}

func NowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.999999-07:00")
}

// LoadState reads state.json into typed State struct, applying v2 upgrades.
func LoadState() State {
	st := State{
		Version:  1,
		Projects: make(map[string]ProjectNode),
	}
	statePath := filepath.Join(config.GetBuddyDir(), "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		st.UpdatedAt = NowISO()
		return st
	}

	if err := json.Unmarshal(data, &st); err != nil {
		st.UpdatedAt = NowISO()
		return st
	}

	if st.Projects == nil {
		st.Projects = make(map[string]ProjectNode)
	}

	for pName, pNode := range st.Projects {
		if pNode.Backend == "" {
			pNode.Backend = "rex"
		}
		if pNode.SessionID == "" && pNode.RexSessionID != "" {
			pNode.SessionID = pNode.RexSessionID
		}
		if pNode.RexSessionID == "" && pNode.SessionID != "" {
			pNode.RexSessionID = pNode.SessionID
		}
		if pNode.Tasks == nil {
			pNode.Tasks = make(map[string]TaskNode)
		}
		for tName, tNode := range pNode.Tasks {
			if tNode.Backend == "" {
				tNode.Backend = pNode.Backend
			}
			pNode.Tasks[tName] = tNode
		}
		st.Projects[pName] = pNode
	}

	return st
}

// LoadRawState loads state as a map for dynamic preservation.
func LoadRawState() (map[string]any, error) {
	statePath := filepath.Join(config.GetBuddyDir(), "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return map[string]any{
			"version":          1,
			"buddy_session_id": nil,
			"updated_at":       NowISO(),
			"projects":         map[string]any{},
		}, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]any{
			"version":          1,
			"buddy_session_id": nil,
			"updated_at":       NowISO(),
			"projects":         map[string]any{},
		}, nil
	}

	if _, ok := raw["projects"].(map[string]any); !ok {
		raw["projects"] = map[string]any{}
	}

	return raw, nil
}

// SaveRawState saves the raw state atomically to disk with file sync.
func SaveRawState(raw map[string]any) error {
	buddyDir := config.GetBuddyDir()
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		return err
	}

	raw["updated_at"] = NowISO()

	// Ensure v2 fields and mirrors
	if projects, ok := raw["projects"].(map[string]any); ok {
		for _, pVal := range projects {
			if pMap, ok := pVal.(map[string]any); ok {
				sID, _ := pMap["rex_session_id"].(string)
				if sID == "" {
					sID, _ = pMap["session_id"].(string)
				}
				if sID != "" {
					pMap["rex_session_id"] = sID
					pMap["session_id"] = sID
				}
				if pMap["backend"] == nil || pMap["backend"] == "" {
					pMap["backend"] = "rex"
				}
			}
		}
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := filepath.Join(buddyDir, fmt.Sprintf(".state.json.tmp.%d.%d", os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, filepath.Join(buddyDir, "state.json"))
}

// SaveState saves typed state atomically to disk.
func SaveState(st State) error {
	buddyDir := config.GetBuddyDir()
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		return err
	}

	st.UpdatedAt = NowISO()
	for pName, pNode := range st.Projects {
		if pNode.SessionID == "" && pNode.RexSessionID != "" {
			pNode.SessionID = pNode.RexSessionID
		}
		if pNode.RexSessionID == "" && pNode.SessionID != "" {
			pNode.RexSessionID = pNode.SessionID
		}
		if pNode.Backend == "" {
			pNode.Backend = "rex"
		}
		st.Projects[pName] = pNode
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := filepath.Join(buddyDir, fmt.Sprintf(".state.json.tmp.%d.%d", os.Getpid(), time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, filepath.Join(buddyDir, "state.json"))
}
