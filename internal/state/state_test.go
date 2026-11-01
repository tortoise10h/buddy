package state

import (
	"encoding/json"
	"testing"
)

func TestState_FindProjectAndTask(t *testing.T) {
	rawJSON := `{
		"version": 1,
		"projects": {
			"docnotate": {
				"name": "docnotate",
				"rex_session_id": "sess_doc_1",
				"tasks": {
					"dark-mode": {
						"task_slug": "dark-mode",
						"window_id": "win_dm_1",
						"agent_block_id": "blk_dm_1",
						"status": "running"
					}
				}
			}
		}
	}`

	var s State
	if err := json.Unmarshal([]byte(rawJSON), &s); err != nil {
		t.Fatalf("failed to unmarshal test state: %v", err)
	}

	// Substring / case-insensitive search
	proj, err := s.FindProject("Docno")
	if err != nil {
		t.Fatalf("FindProject failed: %v", err)
	}
	if proj.RexSessionID != "sess_doc_1" {
		t.Errorf("expected sess_doc_1, got %s", proj.RexSessionID)
	}

	task, err := proj.FindTask("Dark-Mode")
	if err != nil {
		t.Fatalf("FindTask failed: %v", err)
	}
	if task.AgentBlockID != "blk_dm_1" {
		t.Errorf("expected blk_dm_1, got %s", task.AgentBlockID)
	}
}
