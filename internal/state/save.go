package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func nowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.999999-07:00")
}

// withLock acquires an exclusive advisory file lock on .state.lock while executing fn.
func withLock(fn func() error) error {
	dir := GetBuddyDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	lockPath := filepath.Join(dir, ".state.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire state lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}

// SaveState writes the state struct atomically to state.json under an advisory flock.
func SaveState(s *State) error {
	return withLock(func() error {
		return writeStateDirect(s)
	})
}

// MutateState executes a read-modify-write cycle inside an exclusive file lock,
// preventing concurrent processes from overwriting or corrupting state.
func MutateState(fn func(s *State) error) error {
	return withLock(func() error {
		st, err := LoadState()
		if err != nil {
			return err
		}

		if err := fn(st); err != nil {
			return err
		}

		return writeStateDirect(st)
	})
}

func writeStateDirect(s *State) error {
	dir := GetBuddyDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	s.UpdatedAt = nowISO()
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Projects == nil {
		s.Projects = make(map[string]ProjectState)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize state JSON: %w", err)
	}
	data = append(data, '\n')

	tmpPath := filepath.Join(dir, ".state.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp state file: %w", err)
	}

	finalPath := GetStatePath()
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to commit state file: %w", err)
	}

	return nil
}

// RecordTask atomically inserts or updates a task under a project in state.
func (s *State) RecordTask(projectName, projectPath, sessionID, sessionLabel string, task TaskState) {
	if s.Projects == nil {
		s.Projects = make(map[string]ProjectState)
	}

	proj, exists := s.Projects[projectName]
	if !exists {
		proj = ProjectState{
			Name:            projectName,
			ProjectPath:     projectPath,
			RexSessionID:    sessionID,
			RexSessionLabel: sessionLabel,
			Tasks:           make(map[string]TaskState),
		}
	} else {
		if proj.ProjectPath == "" && projectPath != "" {
			proj.ProjectPath = projectPath
		}
		if proj.RexSessionID == "" && sessionID != "" {
			proj.RexSessionID = sessionID
		}
		if proj.RexSessionLabel == "" && sessionLabel != "" {
			proj.RexSessionLabel = sessionLabel
		}
		if proj.Tasks == nil {
			proj.Tasks = make(map[string]TaskState)
		}
	}

	now := nowISO()
	if task.CreatedAt == "" {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.Status == "" {
		task.Status = "running"
	}
	if task.Engine == "" {
		task.Engine = "rex"
	}

	proj.Tasks[task.TaskSlug] = task
	s.Projects[projectName] = proj
}

// UpdateTask updates the status and last_summary of a task in state.
func (s *State) UpdateTask(projectName, taskSlug, status, summary string) error {
	proj, err := s.FindProject(projectName)
	if err != nil {
		return err
	}

	task, err := proj.FindTask(taskSlug)
	if err != nil {
		return err
	}

	if status != "" {
		task.Status = status
	}
	if summary != "" {
		task.LastSummary = summary
	}
	task.UpdatedAt = nowISO()

	proj.Tasks[task.TaskSlug] = *task
	s.Projects[proj.Name] = *proj
	return nil
}

// UpdateTaskDevHandle sets or clears the DevHostHandle of a task in state.
func (s *State) UpdateTaskDevHandle(projectName, taskSlug string, handle *DevHostHandle) error {
	proj, err := s.FindProject(projectName)
	if err != nil {
		return err
	}

	task, err := proj.FindTask(taskSlug)
	if err != nil {
		return err
	}

	task.DevHostHandle = handle
	task.UpdatedAt = nowISO()

	proj.Tasks[task.TaskSlug] = *task
	s.Projects[proj.Name] = *proj
	return nil
}
