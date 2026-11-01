package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDotPath_Get(t *testing.T) {
	raw := map[string]any{
		"worker_agent": "agy",
		"capture_lines": 120,
		"use_worktree_by_default": false,
		"projects_roots": []any{"/path/1", "/path/2"},
		"notifications": map[string]any{
			"sound": "Glass",
			"desktop_enabled": true,
		},
	}

	// String
	if v := GetDotPath(raw, "worker_agent"); v != "agy" {
		t.Fatalf("expected 'agy', got %v", v)
	}

	// Int
	if v := GetDotPath(raw, "capture_lines"); v != 120 {
		t.Fatalf("expected 120, got %v", v)
	}

	// Bool
	if v := GetDotPath(raw, "use_worktree_by_default"); v != false {
		t.Fatalf("expected false, got %v", v)
	}

	// Array
	if v := GetDotPath(raw, "projects_roots"); !reflect.DeepEqual(v, []any{"/path/1", "/path/2"}) {
		t.Fatalf("expected array, got %v", v)
	}

	// Nested
	if v := GetDotPath(raw, "notifications.sound"); v != "Glass" {
		t.Fatalf("expected 'Glass', got %v", v)
	}

	// Unknown key returns nil (which serializes to null in JSON)
	if v := GetDotPath(raw, "nonexistent"); v != nil {
		t.Fatalf("expected nil for unknown key, got %v", v)
	}
	if v := GetDotPath(raw, "notifications.nonexistent"); v != nil {
		t.Fatalf("expected nil for unknown nested key, got %v", v)
	}
}

func TestDotPath_Set(t *testing.T) {
	raw := make(map[string]any)

	// Set string
	SetDotPath(raw, "worker_agent", "opencode")
	if raw["worker_agent"] != "opencode" {
		t.Fatalf("failed to set string: %v", raw["worker_agent"])
	}

	// Set int
	SetDotPath(raw, "capture_lines", 300)
	if raw["capture_lines"] != 300 {
		t.Fatalf("failed to set int: %v", raw["capture_lines"])
	}

	// Set bool
	SetDotPath(raw, "use_worktree_by_default", true)
	if raw["use_worktree_by_default"] != true {
		t.Fatalf("failed to set bool: %v", raw["use_worktree_by_default"])
	}

	// Set array
	SetDotPath(raw, "projects_roots", []any{"/a", "/b"})
	if !reflect.DeepEqual(raw["projects_roots"], []any{"/a", "/b"}) {
		t.Fatalf("failed to set array: %v", raw["projects_roots"])
	}

	// Set nested
	SetDotPath(raw, "notifications.sound", "Hero")
	notifs, ok := raw["notifications"].(map[string]any)
	if !ok || notifs["sound"] != "Hero" {
		t.Fatalf("failed to set nested key: %v", raw["notifications"])
	}
}

func TestParseConfigValue(t *testing.T) {
	tests := []struct {
		input    string
		expected any
	}{
		{"true", true},
		{"false", false},
		{"42", 42},
		{"hello", "hello"},
		{`["a", "b"]`, []any{"a", "b"}},
		{`{"key": "val"}`, map[string]any{"key": "val"}},
	}

	for _, tt := range tests {
		got := ParseConfigValue(tt.input)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("ParseConfigValue(%q) = %v (%T), expected %v (%T)",
				tt.input, got, got, tt.expected, tt.expected)
		}
	}
}

func TestDefaultsMatchPython(t *testing.T) {
	tempHome := t.TempDir()
	buddyDir := filepath.Join(tempHome, ".buddy")
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tempHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	// Load default raw config from Go
	goRaw, err := LoadRawConfig()
	if err != nil {
		t.Fatalf("LoadRawConfig failed: %v", err)
	}

	// Get Python DEFAULT_CONFIG via python3
	pyScript := `
import json, sys, os
from pathlib import Path

# Extract DEFAULT_CONFIG directly from buddy-ctl
script_path = (Path(__file__).resolve().parent.parent.parent / "skills/buddy/scripts/buddy-ctl") if "__file__" in locals() else Path("../../skills/buddy/scripts/buddy-ctl").resolve()
with open(script_path, "r", encoding="utf-8") as f:
    code = f.read()

# Execute up to DEFAULT_CONFIG definition
local_scope = {}
exec(compile(code, "buddy-ctl", "exec"), local_scope)
default_cfg = local_scope.get("DEFAULT_CONFIG", {})

print(json.dumps(default_cfg))
`
	cmd := exec.Command("python3", "-c", pyScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to extract Python DEFAULT_CONFIG: %v\nOutput: %s", err, string(out))
	}

	var pyDefaults map[string]any
	if err := json.Unmarshal(out, &pyDefaults); err != nil {
		t.Fatalf("failed to unmarshal Python DEFAULT_CONFIG: %v", err)
	}

	// Compare core default scalar keys
	keysToCheck := []string{
		"version",
		"buddy_session_name",
		"orchestrator_agent",
		"worker_agent",
		"default_agent",
		"use_worktree_by_default",
		"capture_lines",
	}

	for _, k := range keysToCheck {
		pyVal := pyDefaults[k]
		goVal := goRaw[k]
		if !reflect.DeepEqual(pyVal, goVal) {
			t.Errorf("default mismatch for key %q: Python=%v Go=%v", k, pyVal, goVal)
		}
	}

	// Compare notifications defaults
	pyNotif, _ := pyDefaults["notifications"].(map[string]any)
	goNotif, _ := goRaw["notifications"].(map[string]any)
	for k, pyV := range pyNotif {
		goV := goNotif[k]
		if !reflect.DeepEqual(pyV, goV) {
			t.Errorf("notifications default mismatch for key %q: Python=%v Go=%v", k, pyV, goV)
		}
	}
}
