package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkContextInject(b *testing.B) {
	tempBuddyDir, err := os.MkdirTemp("", "buddy-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempBuddyDir)

	stateJSON := `{
  "version": 1,
  "buddy_session_id": "session:bench-1234",
  "updated_at": "2026-11-01T15:00:00+07:00",
  "projects": {
    "project-a": {
      "name": "project-a",
      "project_path": "/tmp/project-a",
      "rex_session_id": "session:proj-a",
      "tasks": {
        "task-1": {
          "task_slug": "task-1",
          "agent": "agy",
          "status": "waiting_for_input",
          "last_summary": "Asking about database migration options",
          "updated_at": "2026-11-01T15:01:00+07:00"
        },
        "task-2": {
          "task_slug": "task-2",
          "agent": "agy",
          "status": "running",
          "last_summary": "Running integration tests",
          "updated_at": "2026-11-01T15:02:00+07:00"
        }
      }
    },
    "project-b": {
      "name": "project-b",
      "project_path": "/tmp/project-b",
      "rex_session_id": "session:proj-b",
      "tasks": {
        "feat-login": {
          "task_slug": "feat-login",
          "agent": "agy",
          "status": "ready_for_demo",
          "last_summary": "OAuth login implemented and verified",
          "updated_at": "2026-11-01T15:03:00+07:00"
        }
      }
    }
  }
}`
	_ = os.WriteFile(filepath.Join(tempBuddyDir, "state.json"), []byte(stateJSON), 0644)
	_ = os.WriteFile(filepath.Join(tempBuddyDir, "config.json"), []byte(`{"version":1,"orchestrator_agent":"agy","worker_agent":"agy"}`), 0644)

	origBuddyDir := os.Getenv("BUDDY_DIR")
	defer os.Setenv("BUDDY_DIR", origBuddyDir)
	os.Setenv("BUDDY_DIR", tempBuddyDir)

	origCwd, _ := os.Getwd()
	defer os.Chdir(origCwd)
	_ = os.Chdir(tempBuddyDir)

	payload := `{"workspacePaths": ["` + tempBuddyDir + `"]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := strings.NewReader(payload)
		err := runContextInject(r, io.Discard)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestContextInjectNotBuddy(t *testing.T) {
	tempBuddyDir, err := os.MkdirTemp("", "buddy-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempBuddyDir)

	origBuddyDir := os.Getenv("BUDDY_DIR")
	defer os.Setenv("BUDDY_DIR", origBuddyDir)
	os.Setenv("BUDDY_DIR", tempBuddyDir)

	var buf bytes.Buffer
	r := strings.NewReader(`{"workspacePaths": ["/some/other/workspace"]}`)
	err = runContextInject(r, &buf)
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(buf.String()) != "{}" {
		t.Fatalf("expected empty object '{}', got: %s", buf.String())
	}
}
