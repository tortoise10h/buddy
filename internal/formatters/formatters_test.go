package formatters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/state"
)

func getSampleProjects() map[string]state.ProjectNode {
	return map[string]state.ProjectNode{
		"docnotate": {
			Name:         "docnotate",
			RexSessionID: "session:01a18d0d-b56d-752c-ac4c-0cf87d8e07c7",
			Tasks:        map[string]state.TaskNode{},
		},
		"myproj": {
			Name:         "myproj",
			RexSessionID: "session:01a18ce6-a7f8-75c8-9959-16d692a220fb",
			Tasks: map[string]state.TaskNode{
				"feat-auth": {
					TaskSlug:    "feat-auth",
					Agent:       "agy",
					Status:      "running",
					LastSummary: "Implementing JWT authentication",
				},
			},
		},
	}
}

func TestFormatStatusBrief(t *testing.T) {
	projects := getSampleProjects()
	actual := FormatStatusBrief(projects)

	goldenPath := filepath.Join("..", "..", "testdata", "golden", "status_brief.golden")
	expectedBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.TrimSpace(string(expectedBytes))

	if strings.TrimSpace(actual) != expected {
		t.Errorf("brief output mismatch:\nExpected:\n%s\nGot:\n%s", expected, actual)
	}
}

func TestFormatStatusTable(t *testing.T) {
	projects := getSampleProjects()
	actual := FormatStatusTable(projects)

	goldenPath := filepath.Join("..", "..", "testdata", "golden", "status_table.golden")
	expectedBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.TrimSpace(string(expectedBytes))

	if strings.TrimSpace(actual) != expected {
		t.Errorf("table output mismatch:\nExpected:\n%s\nGot:\n%s", expected, actual)
	}
}

func TestFormatJSON(t *testing.T) {
	m := map[string]any{
		"status":  "ok",
		"message": "hello <world> & goodbye",
	}
	out, err := FormatJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	// Verify no HTML escaping
	if !strings.Contains(out, "<world> &") {
		t.Errorf("expected unescaped HTML characters, got: %s", out)
	}
}
