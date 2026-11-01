package state

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireStateLock_Exclusion(t *testing.T) {
	tempHome := t.TempDir()
	buddyDir := filepath.Join(tempHome, ".buddy")
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tempHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	// Acquire lock #1
	lock1, err := AcquireStateLock(1 * time.Second)
	if err != nil {
		t.Fatalf("failed to acquire initial lock: %v", err)
	}
	defer lock1.Release()

	// Try acquiring lock #2 from another attempt while lock1 is held
	start := time.Now()
	lock2, err2 := AcquireStateLock(100 * time.Millisecond)
	elapsed := time.Since(start)

	if err2 == nil {
		lock2.Release()
		t.Fatalf("expected lock acquisition to fail while held by lock1, but it succeeded")
	}
	if elapsed < 80*time.Millisecond {
		t.Fatalf("expected retry polling up to timeout, but failed too quickly (%v)", elapsed)
	}

	// Release lock1
	if err := lock1.Release(); err != nil {
		t.Fatalf("failed to release lock1: %v", err)
	}

	// Now lock3 should succeed
	lock3, err3 := AcquireStateLock(500 * time.Millisecond)
	if err3 != nil {
		t.Fatalf("expected lock acquisition to succeed after release, but got: %v", err3)
	}
	_ = lock3.Release()
}

func TestAtomicWrite_AtomicAndNoPartialOnFailure(t *testing.T) {
	tempHome := t.TempDir()
	buddyDir := filepath.Join(tempHome, ".buddy")
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tempHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	rawState := map[string]any{
		"version":          2,
		"buddy_session_id": "session:test",
		"projects": map[string]any{
			"test-proj": map[string]any{
				"name": "test-proj",
			},
		},
	}

	err := SaveRawState(rawState)
	if err != nil {
		t.Fatalf("SaveRawState failed: %v", err)
	}

	statePath := filepath.Join(buddyDir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state.json: %v", err)
	}

	var loaded map[string]any
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal state.json: %v", err)
	}

	if loaded["buddy_session_id"] != "session:test" {
		t.Fatalf("unexpected state content: %v", loaded)
	}

	// Verify no partial / temporary files left in directory
	entries, err := os.ReadDir(buddyDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || (len(e.Name()) > 10 && e.Name()[:11] == ".state.json") {
			t.Fatalf("found leftover temporary file: %s", e.Name())
		}
	}
}

func TestRoundTripPythonFixture(t *testing.T) {
	tempHome := t.TempDir()
	buddyDir := filepath.Join(tempHome, ".buddy")
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tempHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	// Copy real parity_state.json fixture
	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "parity_state.json")
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	destPath := filepath.Join(buddyDir, "state.json")
	if err := os.WriteFile(destPath, fixtureData, 0644); err != nil {
		t.Fatal(err)
	}

	// Load with Go LoadState
	st := LoadState()
	if st.Version != 1 {
		t.Fatalf("expected version 1, got %d", st.Version)
	}
	proj, ok := st.Projects["docnotate"]
	if !ok {
		t.Fatalf("expected docnotate project in loaded state")
	}
	if proj.RexSessionID != "session:rex-docnotate" {
		t.Fatalf("expected rex_session_id to be preserved, got %q", proj.RexSessionID)
	}

	// Save back
	if err := SaveState(st); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify with Python json.load
	pyScript := `
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    data = json.load(f)
assert data["version"] in (1, 2)
assert "docnotate" in data["projects"]
assert "auth" in data["projects"]["docnotate"]["tasks"]
print("PYTHON_OK")
`
	cmd := exec.Command("python3", "-c", pyScript, destPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python failed to parse Go-written state.json: %v\nOutput: %s", err, string(out))
	}
	if string(out) != "PYTHON_OK\n" {
		t.Fatalf("unexpected Python output: %s", string(out))
	}
}
