package task

import (
	"strings"
	"testing"
)

func TestDeriveTaskSlug(t *testing.T) {
	tests := []struct {
		name        string
		prompt      string
		project     string
		prefixCheck string
	}{
		{
			name:        "Conversational preamble stripping",
			prompt:      "Hey buddy, can you please add user authentication to the server?",
			project:     "docnotate",
			prefixCheck: "add-user-authentication",
		},
		{
			name:        "Stop words filtered",
			prompt:      "Fix the typo in the documentation",
			project:     "docnotate",
			prefixCheck: "fix-typo-documentation",
		},
		{
			name:        "Fallback to project task when only stop words",
			prompt:      "please do this for me",
			project:     "docnotate",
			prefixCheck: "docnotate-task-",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			slug := DeriveTaskSlug(tc.prompt, tc.project)
			if !strings.HasPrefix(slug, tc.prefixCheck) {
				t.Errorf("expected slug to start with '%s', got '%s'", tc.prefixCheck, slug)
			}
			// Should always have a 4-char hex suffix after hyphen
			parts := strings.Split(slug, "-")
			suffix := parts[len(parts)-1]
			if len(suffix) != 4 {
				t.Errorf("expected 4-char hex suffix, got '%s'", suffix)
			}
		})
	}
}

func TestSanitizeSlug(t *testing.T) {
	input := "Feat: Add_New_Auth#42!"
	expected := "feat-add_new_auth-42"
	actual := SanitizeSlug(input)
	if actual != expected {
		t.Errorf("expected '%s', got '%s'", expected, actual)
	}
}
