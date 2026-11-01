package rex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRexLs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "rex_ls.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out RexLsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to parse rex ls JSON: %v", err)
	}

	if len(out.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(out.Sessions))
	}
	if out.Sessions[0].Label != "docnotate" {
		t.Errorf("expected label 'docnotate', got %s", out.Sessions[0].Label)
	}
	if out.Sessions[1].SessionID != "session:01a18ce6-a7f8-75c8-9959-16d692a220fb" {
		t.Errorf("expected buddy session ID, got %s", out.Sessions[1].SessionID)
	}
}

func TestParseRexSessionInspect(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "rex_session_inspect.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out RexSessionInspectOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to parse rex session inspect JSON: %v", err)
	}

	if out.SessionID != "session:01a18d0d-b56d-752c-ac4c-0cf87d8e07c7" {
		t.Errorf("unexpected session_id: %s", out.SessionID)
	}
	if out.Label != "docnotate" {
		t.Errorf("unexpected label: %s", out.Label)
	}
}

func TestParseRexWindowLs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "rex_window_ls.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out RexWindowLsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to parse rex window ls JSON: %v", err)
	}

	if len(out.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(out.Windows))
	}
	if out.Windows[0].Label != "Server" {
		t.Errorf("expected window 0 label 'Server', got %s", out.Windows[0].Label)
	}
	if out.Windows[1].Label != "Console" {
		t.Errorf("expected window 1 label 'Console', got %s", out.Windows[1].Label)
	}
}

func TestParseRexWindowNew(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "rex_window_new.json"))
	if err != nil {
		t.Fatal(err)
	}

	var out RexWindowNewOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("failed to parse rex window new JSON: %v", err)
	}

	if out.WindowID != "window:01a19630-7843-7d46-bc98-b5b2c8f3bc00" {
		t.Errorf("unexpected window_id: %s", out.WindowID)
	}
	if len(out.BlockIDs) != 1 || out.BlockIDs[0] != "block:01a19630-7832-769f-9a0e-ee0d58739904" {
		t.Errorf("unexpected block_ids: %v", out.BlockIDs)
	}
}
