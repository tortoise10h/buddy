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

func TestState_PerEnginePathsAndFreshStart(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	if got := NormalizeEngine(""); got != "rex" {
		t.Errorf("NormalizeEngine(\"\") = %q, want \"rex\"", got)
	}
	if got := NormalizeEngine("  T3 "); got != "t3" {
		t.Errorf("NormalizeEngine(\"  T3 \") = %q, want \"t3\"", got)
	}

	wantRexPath := filepath.Join(tmpDir, "state", "rex.json")
	if got := GetStatePathForEngine(""); got != wantRexPath {
		t.Errorf("GetStatePathForEngine(\"\") = %q, want %q", got, wantRexPath)
	}
	wantT3Seen := filepath.Join(tmpDir, "state", ".t3_seen.json")
	if got := GetStateSeenPathForEngine("t3"); got != wantT3Seen {
		t.Errorf("GetStateSeenPathForEngine(\"t3\") = %q, want %q", got, wantT3Seen)
	}

	// Fresh-start: missing per-engine state file returns an empty initialized State.
	st, err := LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine(t3) on missing file failed: %v", err)
	}
	if st.Version != 1 || st.Projects == nil || len(st.Projects) != 0 {
		t.Errorf("expected empty initialized state, got %+v", st)
	}

	engines, err := ListStateEngines()
	if err != nil {
		t.Fatalf("ListStateEngines on fresh dir failed: %v", err)
	}
	if len(engines) != 0 {
		t.Errorf("expected no engines on fresh dir, got %v", engines)
	}

	// SaveStateForEngine creates the per-engine file atomically.
	st.RecordTask("buddy", "/tmp/buddy", "proj-t3-1", "buddy", TaskState{
		TaskSlug:     "feat-save",
		Engine:       "t3",
		WindowID:     "thread-1",
		AgentBlockID: "thread-1",
	})
	if err := SaveStateForEngine("t3", st); err != nil {
		t.Fatalf("SaveStateForEngine(t3) failed: %v", err)
	}
	if _, err := os.Stat(GetStatePathForEngine("t3")); err != nil {
		t.Errorf("expected t3 state file to exist: %v", err)
	}
}
