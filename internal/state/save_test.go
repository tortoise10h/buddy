package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestState_SaveAndMutateWithLock(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// 1. MutateState on clean dir creates state.json
	err := MutateState(func(s *State) error {
		s.RecordTask("my-project", "/path/to/project", "sess_1", "My Project", TaskState{
			TaskSlug:     "feat-login",
			WindowID:     "win_1",
			AgentBlockID: "blk_1",
			Agent:        "agy",
		})
		return nil
	})
	if err != nil {
		t.Fatalf("MutateState failed: %v", err)
	}

	// 2. Read back state
	st, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	proj, err := st.FindProject("my-project")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	if proj.RexSessionID != "sess_1" {
		t.Errorf("expected session sess_1, got %s", proj.RexSessionID)
	}

	task, err := proj.FindTask("feat-login")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.WindowID != "win_1" || task.AgentBlockID != "blk_1" {
		t.Errorf("unexpected task: %+v", task)
	}
	if task.Engine != "rex" {
		t.Errorf("expected default engine rex, got %s", task.Engine)
	}

	// 3. Test concurrent mutations don't corrupt the file
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			_ = MutateState(func(s *State) error {
				s.RecordTask("my-project", "/path/to/project", "sess_1", "My Project", TaskState{
					TaskSlug:     filepath.Join("task", string(rune('a'+idx))),
					WindowID:     "win_x",
					AgentBlockID: "blk_x",
				})
				return nil
			})
		}()
	}
	wg.Wait()

	// 4. Verify file on disk is valid JSON
	data, err := os.ReadFile(filepath.Join(tmpDir, "state", "rex.json"))
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("state file is empty")
	}

	stFinal, err := LoadState()
	if err != nil {
		t.Fatalf("final state JSON is invalid: %v", err)
	}
	finalProj, _ := stFinal.FindProject("my-project")
	if len(finalProj.Tasks) < 2 {
		t.Errorf("expected multiple tasks recorded concurrently, got %d", len(finalProj.Tasks))
	}
}

func TestState_PerEngineIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	if err := MutateStateForEngine("rex", func(s *State) error {
		s.RecordTask("buddy", "/tmp/buddy", "session:rex-1", "buddy", TaskState{
			TaskSlug:     "feat-rex",
			Engine:       "rex",
			WindowID:     "win-1",
			AgentBlockID: "blk-1",
			Status:       "running",
		})
		return nil
	}); err != nil {
		t.Fatalf("MutateStateForEngine(rex) failed: %v", err)
	}

	if err := MutateStateForEngine("t3", func(s *State) error {
		s.RecordTask("buddy", "/tmp/buddy", "proj-t3-1", "buddy", TaskState{
			TaskSlug:     "feat-t3",
			Engine:       "t3",
			WindowID:     "thread-1",
			AgentBlockID: "thread-1",
			Status:       "running",
		})
		return nil
	}); err != nil {
		t.Fatalf("MutateStateForEngine(t3) failed: %v", err)
	}

	rexState, err := LoadStateForEngine("rex")
	if err != nil {
		t.Fatalf("LoadStateForEngine(rex) failed: %v", err)
	}
	if _, ok := rexState.Projects["buddy"].Tasks["feat-t3"]; ok {
		t.Errorf("rex state should not contain t3 task")
	}
	if _, ok := rexState.Projects["buddy"].Tasks["feat-rex"]; !ok {
		t.Errorf("rex state missing feat-rex task")
	}

	t3State, err := LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
	}
	if _, ok := t3State.Projects["buddy"].Tasks["feat-rex"]; ok {
		t.Errorf("t3 state should not contain rex task")
	}
	if _, ok := t3State.Projects["buddy"].Tasks["feat-t3"]; !ok {
		t.Errorf("t3 state missing feat-t3 task")
	}

	engines, err := ListStateEngines()
	if err != nil {
		t.Fatalf("ListStateEngines failed: %v", err)
	}
	if len(engines) != 2 || engines[0] != "rex" || engines[1] != "t3" {
		t.Errorf("expected [rex t3], got %v", engines)
	}
}

func TestState_LegacyMigration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	legacyJSON := `{
  "version": 1,
  "buddy_session_id": "legacy-1",
  "projects": {
    "buddy": {
      "name": "buddy",
      "project_path": "/tmp/buddy",
      "rex_session_id": "session:rex-1",
      "rex_session_label": "Buddy",
      "tasks": {
        "rex-task": {
          "task_slug": "rex-task",
          "window_id": "win-1",
          "agent_block_id": "blk-1",
          "status": "running"
        },
        "t3-task": {
          "task_slug": "t3-task",
          "engine": "t3",
          "window_id": "thread-1",
          "agent_block_id": "thread-1",
          "status": "running"
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "state.json"), []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy state.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".state_seen.json"), []byte(`{"buddy/rex-task":{"status":"running"}}`), 0644); err != nil {
		t.Fatalf("failed to write legacy .state_seen.json: %v", err)
	}

	t3State, err := LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
	}
	if _, ok := t3State.Projects["buddy"].Tasks["t3-task"]; !ok {
		t.Errorf("expected t3-task in migrated t3 state")
	}
	if _, ok := t3State.Projects["buddy"].Tasks["rex-task"]; ok {
		t.Errorf("did not expect rex-task in migrated t3 state")
	}
	if t3State.Projects["buddy"].RexSessionID != "" {
		t.Errorf("expected rex session ID cleared for t3 project, got %q", t3State.Projects["buddy"].RexSessionID)
	}

	rexState, err := LoadStateForEngine("rex")
	if err != nil {
		t.Fatalf("LoadStateForEngine(rex) failed: %v", err)
	}
	if _, ok := rexState.Projects["buddy"].Tasks["rex-task"]; !ok {
		t.Errorf("expected rex-task in migrated rex state")
	}
	if rexState.Projects["buddy"].RexSessionID != "session:rex-1" {
		t.Errorf("expected rex session ID preserved, got %q", rexState.Projects["buddy"].RexSessionID)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "state.json")); !os.IsNotExist(err) {
		t.Errorf("expected legacy state.json to be renamed")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "state.json.bak")); err != nil {
		t.Errorf("expected state.json.bak to exist: %v", err)
	}
	if _, err := os.Stat(GetStateSeenPathForEngine("rex")); err != nil {
		t.Errorf("expected migrated .rex_seen.json to exist: %v", err)
	}
}
