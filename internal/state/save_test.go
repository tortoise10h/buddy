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
	data, err := os.ReadFile(filepath.Join(tmpDir, "state.json"))
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
