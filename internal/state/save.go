package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func nowISO() string {
	return time.Now().Format("2006-01-02T15:04:05.999999-07:00")
}

// withLockFile acquires an exclusive advisory file lock on lockPath while executing fn.
func withLockFile(lockPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}

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

// withEngineLock acquires an exclusive advisory file lock for the given engine's state file.
func withEngineLock(engine string, fn func() error) error {
	lockPath := filepath.Join(GetStateDir(), "."+NormalizeEngine(engine)+".lock")
	return withLockFile(lockPath, fn)
}

// SaveState writes the state struct atomically to the default ("rex") state file.
func SaveState(s *State) error {
	return SaveStateForEngine("rex", s)
}

// SaveStateForEngine writes the state struct atomically to ~/.buddy/state/<engine>.json under an advisory flock.
func SaveStateForEngine(engine string, s *State) error {
	return withEngineLock(engine, func() error {
		return writeStateDirectForEngine(engine, s)
	})
}

// MutateState executes a read-modify-write cycle on the default ("rex") state file.
func MutateState(fn func(s *State) error) error {
	return MutateStateForEngine("rex", fn)
}

// MutateStateForEngine executes a read-modify-write cycle on ~/.buddy/state/<engine>.json
// inside an exclusive file lock, preventing concurrent processes from overwriting or corrupting state.
func MutateStateForEngine(engine string, fn func(s *State) error) error {
	migrateLegacyStateIfNeeded()
	return withEngineLock(engine, func() error {
		st, err := readStateFile(GetStatePathForEngine(engine))
		if err != nil {
			return err
		}

		if err := fn(st); err != nil {
			return err
		}

		return writeStateDirectForEngine(engine, st)
	})
}

func writeStateDirectForEngine(engine string, s *State) error {
	dir := GetStateDir()
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

	norm := NormalizeEngine(engine)
	tmpPath := filepath.Join(dir, "."+norm+".json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp state file: %w", err)
	}

	finalPath := GetStatePathForEngine(norm)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to commit state file: %w", err)
	}

	return nil
}

// migrateLegacyStateIfNeeded splits a legacy ~/.buddy/state.json into ~/.buddy/state/<engine>.json
// files and renames the legacy file to state.json.bak.
func migrateLegacyStateIfNeeded() {
	legacyPath := GetLegacyStatePath()
	if _, err := os.Stat(legacyPath); err != nil {
		return
	}

	lockPath := filepath.Join(GetBuddyDir(), ".migrate.lock")
	_ = withLockFile(lockPath, func() error {
		if _, err := os.Stat(legacyPath); err != nil {
			return nil
		}

		legacy, err := readStateFile(legacyPath)
		if err != nil {
			return nil
		}

		partitioned := make(map[string]*State)
		ensureEngineState := func(eng string) *State {
			norm := NormalizeEngine(eng)
			if es, ok := partitioned[norm]; ok {
				return es
			}
			es := &State{
				Version:        legacy.Version,
				BuddySessionID: legacy.BuddySessionID,
				UpdatedAt:      legacy.UpdatedAt,
				Projects:       make(map[string]ProjectState),
			}
			if es.Version == 0 {
				es.Version = 1
			}
			partitioned[norm] = es
			return es
		}

		for pName, pData := range legacy.Projects {
			if len(pData.Tasks) == 0 {
				if pData.RexSessionID != "" {
					es := ensureEngineState("rex")
					pCopy := pData
					pCopy.Tasks = make(map[string]TaskState)
					es.Projects[pName] = pCopy
				}
				continue
			}

			tasksByEngine := make(map[string]map[string]TaskState)
			for tSlug, tData := range pData.Tasks {
				eng := NormalizeEngine(tData.GetEngine())
				if tasksByEngine[eng] == nil {
					tasksByEngine[eng] = make(map[string]TaskState)
				}
				tasksByEngine[eng][tSlug] = tData
			}

			for eng, tasks := range tasksByEngine {
				es := ensureEngineState(eng)
				pCopy := pData
				pCopy.Tasks = tasks
				if eng != "rex" && strings.HasPrefix(pCopy.RexSessionID, "session:") {
					pCopy.RexSessionID = ""
					pCopy.RexSessionLabel = ""
				}
				es.Projects[pName] = pCopy
			}
		}

		for eng, es := range partitioned {
			targetPath := GetStatePathForEngine(eng)
			if _, statErr := os.Stat(targetPath); os.IsNotExist(statErr) {
				if err := writeStateDirectForEngine(eng, es); err != nil {
					return err
				}
			}
		}

		legacySeen := filepath.Join(GetBuddyDir(), ".state_seen.json")
		rexSeen := GetStateSeenPathForEngine("rex")
		if _, err := os.Stat(legacySeen); err == nil {
			if _, err := os.Stat(rexSeen); os.IsNotExist(err) {
				if err := os.MkdirAll(GetStateDir(), 0755); err == nil {
					_ = os.Rename(legacySeen, rexSeen)
				}
			}
		}

		return os.Rename(legacyPath, legacyPath+".bak")
	})
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
	if task.LastVerifiedAt == "" {
		task.LastVerifiedAt = now
	}
	if task.Origin == "" {
		task.Origin = "buddy"
	}
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
	now := nowISO()
	task.UpdatedAt = now
	task.LastVerifiedAt = now

	proj.Tasks[task.TaskSlug] = *task
	s.Projects[proj.Name] = *proj
	return nil
}

// NowISO returns the current timestamp in ISO-8601 format.
func NowISO() string {
	return nowISO()
}

// IsFreshWithin returns true if the given ISO timestamp is non-empty and within ttl of now.
func IsFreshWithin(isoTimestamp string, ttl time.Duration) bool {
	if strings.TrimSpace(isoTimestamp) == "" || ttl <= 0 {
		return false
	}
	t, err := time.Parse("2006-01-02T15:04:05.999999-07:00", isoTimestamp)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, isoTimestamp)
		if err != nil {
			return false
		}
	}
	return time.Since(t) <= ttl
}
