package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestAppendHistoryRecord_SchemaAndFindRecent(t *testing.T) {
	t.Setenv("BUDDY_DIR", t.TempDir())

	branch := "feat/auth"
	prompt := "Implement JWT middleware"
	proj := ProjectState{
		Name:         "buddy",
		ProjectPath:  "/tmp/buddy",
		RexSessionID: "sess-1",
	}
	task := TaskState{
		TaskSlug:       "feat-auth",
		Engine:         "t3",
		WindowID:       "mcp:thread-1",
		WindowLabel:    "feat-auth",
		AgentBlockID:   "mcp:thread-1",
		Agent:          "antigravity/gemini-3.8-flash-medium",
		Cwd:            "/tmp/buddy",
		Branch:         &branch,
		Status:         "closed",
		Origin:         "buddy",
		InitialPrompt:  &prompt,
		LastSummary:    "Task closed via CLI",
		CreatedAt:      "2026-11-15T10:00:00-07:00",
		UpdatedAt:      "2026-11-15T12:00:00-07:00",
		LastVerifiedAt: "2026-11-15T12:00:00-07:00",
	}

	rec := NewHistoryRecord(proj, task, "merged PR #42", false)
	if err := AppendHistoryRecord(rec); err != nil {
		t.Fatalf("AppendHistoryRecord failed: %v", err)
	}

	raw, err := os.ReadFile(GetHistoryPath())
	if err != nil {
		t.Fatalf("ReadFile history failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 JSONL line, got %d", len(lines))
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("invalid JSON line: %v", err)
	}

	requiredKeys := []string{
		"cleanup_version", "cleanup_id", "ts", "project", "project_path",
		"task_slug", "engine", "session_id", "window_id", "window_label",
		"agent_block_id", "dev_block_id", "agent", "cwd", "worktree_path",
		"branch", "final_status", "origin", "initial_prompt", "last_summary",
		"created_at", "updated_at", "last_verified_at", "reason", "forced",
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing expected key %q in history JSON", k)
		}
	}
	if m["cleanup_version"] != float64(1) {
		t.Errorf("cleanup_version = %v, want 1", m["cleanup_version"])
	}
	if m["final_status"] != "closed" {
		t.Errorf("final_status = %v, want closed", m["final_status"])
	}
	if m["reason"] != "merged PR #42" {
		t.Errorf("reason = %v, want 'merged PR #42'", m["reason"])
	}
	if m["forced"] != false {
		t.Errorf("forced = %v, want false", m["forced"])
	}

	found, err := FindRecentHistoryRecord("buddy", "feat-auth", "t3")
	if err != nil {
		t.Fatalf("FindRecentHistoryRecord failed: %v", err)
	}
	if found == nil || found.CleanupID != rec.CleanupID {
		t.Fatalf("FindRecentHistoryRecord = %+v, want cleanup_id %s", found, rec.CleanupID)
	}

	notFound, err := FindRecentHistoryRecord("buddy", "nonexistent", "t3")
	if err != nil {
		t.Fatalf("FindRecentHistoryRecord error on missing: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil for nonexistent task, got %+v", notFound)
	}
}

func TestHistory_ConcurrentAppend(t *testing.T) {
	t.Setenv("BUDDY_DIR", t.TempDir())

	const workers = 12
	const perWorker = 10
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				proj := ProjectState{Name: "buddy", ProjectPath: "/tmp/buddy"}
				task := TaskState{
					TaskSlug: fmt.Sprintf("task-%d-%d", workerID, i),
					Engine:   "rex",
					WindowID: fmt.Sprintf("win-%d-%d", workerID, i),
					Status:   "closed",
				}
				if err := AppendHistoryRecord(NewHistoryRecord(proj, task, "concurrent", false)); err != nil {
					errCh <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent AppendHistoryRecord failed: %v", err)
	}

	f, err := os.Open(GetHistoryPath())
	if err != nil {
		t.Fatalf("open history failed: %v", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec HistoryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("corrupt JSONL line %d: %v (%q)", count+1, err, line)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if count != workers*perWorker {
		t.Fatalf("got %d records, want %d", count, workers*perWorker)
	}
}
