package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrAborted is returned when the user cancels setup (e.g. Ctrl+C / Esc).
var ErrAborted = errors.New("setup aborted by user")

// CustomSentinel is the option value indicating the user wants to type a custom harness name.
const CustomSentinel = "__custom__"

// Option represents a selectable choice in Select or MultiSelect prompts.
type Option struct {
	Label    string
	Value    string
	Selected bool
}

// DirPickerReq configures the Step 1 interactive directory picker (supports immediate '+' / 'a' add shortcut).
type DirPickerReq struct {
	Title       string
	Description string
	Options     []Option
}

// DirPickerResp captures the user's selections and whether they pressed '+' / 'a' to add a directory.
type DirPickerResp struct {
	Selected     []string
	AddRequested bool
}

// SelectReq configures a single-choice selection prompt.
type SelectReq struct {
	Title       string
	Description string
	Options     []Option
	Default     string
}

// MultiSelectReq configures a multi-choice selection prompt.
type MultiSelectReq struct {
	Title       string
	Description string
	Options     []Option
	MinSelected int
}

// InputReq configures a text input prompt with optional validation.
type InputReq struct {
	Title       string
	Description string
	Placeholder string
	Default     string
	Validate    func(string) error
}

// ConfirmReq configures a yes/no confirmation prompt.
type ConfirmReq struct {
	Title       string
	Description string
	Affirmative string
	Negative    string
	Default     bool
}

// NoteReq configures a styled informational note/banner.
type NoteReq struct {
	Title string
	Body  string
}

// ConfigFormState holds live mutable fields bound directly to huh.Form groups
// so WithHideFunc predicates react immediately as the user navigates (incl. Shift+Tab).
type ConfigFormState struct {
	// Scope controls (for --only <section> and --skip-skills)
	ShowEngines  bool
	ShowAgents   bool
	ShowSkills   bool
	ShowAdvanced bool

	// Environment probe context (read-only during form)
	Probe *ProbeResult

	// Step 2: Selected platforms (subset of rex, opencode, t3)
	SelectedEngines []string

	// Step 3: Per-engine config (only shown for engines selected in Step 2)
	OrchestratorAgent  string // harness name or CustomSentinel (rex)
	OrchestratorCustom string
	WorkerAgent        string // harness name or CustomSentinel (rex)
	WorkerCustom       string
	OpenCodeModel      string // optional "<provider>/<model>" (opencode)
	T3ModelSpec        string // optional "<provider>/<model>" (t3)

	// Step 4: Optional / advanced settings
	ConfigureAdvanced    bool // default false (skip)
	NotificationsEnabled bool // default false
	SkillTargets         []string
}

// HasSelectedEngine reports whether engineName is currently selected in Step 2.
func (s *ConfigFormState) HasSelectedEngine(engineName string) bool {
	for _, e := range s.SelectedEngines {
		if e == engineName {
			return true
		}
	}
	return false
}

// DeriveDefaultEngine picks the default_engine from SelectedEngines:
// 1. Current detected host if selected
// 2. First selected platform that is installed on the machine
// 3. First selected platform (fallback "rex")
func (s *ConfigFormState) DeriveDefaultEngine() string {
	if s == nil || len(s.SelectedEngines) == 0 {
		return "rex"
	}
	if s.Probe != nil && s.Probe.DetectedHost != "" && s.HasSelectedEngine(s.Probe.DetectedHost) {
		return s.Probe.DetectedHost
	}
	if s.Probe != nil {
		for _, e := range s.SelectedEngines {
			switch e {
			case "rex":
				if s.Probe.HasRex {
					return "rex"
				}
			case "opencode":
				if s.Probe.HasOpenCode {
					return "opencode"
				}
			case "t3":
				if s.Probe.HasT3 {
					return "t3"
				}
			}
		}
	}
	return s.SelectedEngines[0]
}

// Prompter abstracts interactive prompts, spinners, and the multi-group config wizard.
type Prompter interface {
	IsInteractive() bool
	Note(ctx context.Context, req NoteReq) error
	Select(ctx context.Context, req SelectReq) (string, error)
	MultiSelect(ctx context.Context, req MultiSelectReq) ([]string, error)
	PickDirectories(ctx context.Context, req DirPickerReq) (DirPickerResp, error)
	Input(ctx context.Context, req InputReq) (string, error)
	Confirm(ctx context.Context, req ConfirmReq) (bool, error)
	Spin(ctx context.Context, title string, fn func(context.Context) error) error

	// RunConfigForm runs Steps 2-4 (Platforms MultiSelect, Per-Engine Config only for
	// selected platforms, and Optional/Advanced Config confirm + fields) as one cohesive
	// multi-group huh.Form with Shift+Tab back-navigation and reactive WithHideFunc groups.
	RunConfigForm(ctx context.Context, state *ConfigFormState) error
}

func validateOptionalProviderModel(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("use <provider>/<model> format (e.g. antigravity/gemini-3.8-flash-medium) or leave empty to skip")
	}
	return nil
}

func validateNonEmptyToken(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("value cannot be empty")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return fmt.Errorf("harness name must not contain whitespace")
	}
	return nil
}
