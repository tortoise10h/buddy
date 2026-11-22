package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/tortoise10h/buddy/internal/config"
)

func TestRun_InteractiveWizardAddDirLoopAndPerEngineConfig(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	// Existing root with 2 repos + a second dir to add via "+ Add directory..." loop
	rootA := filepath.Join(tmpHome, "Dev", "Personal", "Projects")
	rootB := filepath.Join(tmpHome, "WorkRepos")
	_ = os.MkdirAll(filepath.Join(rootA, "docnotate"), 0755)
	_ = os.MkdirAll(filepath.Join(rootA, "buddy"), 0755)
	_ = os.MkdirAll(filepath.Join(rootB, "api-srv"), 0755)

	mock := &MockPrompter{
		Interactive: true,
		// Iteration 1: user has rootA checked and presses '+' (AddRequested: true)
		// Iteration 2: user confirms both rootA and newly added rootB with Enter
		DirPickerResp: []DirPickerResp{
			{Selected: []string{rootA}, AddRequested: true},
			{Selected: []string{rootA, rootB}, AddRequested: false},
		},
		InputResp: []string{rootB},
		ConfigFormFn: func(st *ConfigFormState) error {
			// Step 2: user chooses rex + t3 (NOT opencode)
			st.SelectedEngines = []string{"rex", "t3"}
			// Step 3: rex custom orchestrator + agy worker + t3 model
			st.OrchestratorAgent = CustomSentinel
			st.OrchestratorCustom = "my-orch"
			st.WorkerAgent = "agy"
			st.T3ModelSpec = "antigravity/gemini-3.8-flash-medium"
			// Step 4: skip advanced (ConfigureAdvanced stays false -> notifications disabled)
			return nil
		},
	}

	lookPath := func(name string) (string, error) {
		switch name {
		case "rex", "t3", "agy", "buddy-ctl", "buddy":
			return "/usr/local/bin/" + name, nil
		default:
			return "", os.ErrNotExist
		}
	}

	var out bytes.Buffer
	res, err := Run(context.Background(), Options{
		Version:  "v0.2.0",
		HomeDir:  tmpHome,
		BuddyDir: buddyDir,
		Getenv:   func(string) string { return "" },
		LookPath: lookPath,
		Out:      &out,
		Prompter: mock,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if mock.DirPickerCalls != 2 {
		t.Fatalf("expected 2 PickDirectories calls for '+' add-dir loop, got %d", mock.DirPickerCalls)
	}
	// Ensure no '+ Add directory...' checkbox option was injected into the list
	for _, req := range mock.DirPickerReqs {
		for _, opt := range req.Options {
			if opt.Label == "+ Add directory..." {
				t.Fatalf("'+ Add directory...' must not appear as a checkbox option")
			}
		}
	}
	// Ensure the second PickDirectories call shows rootB pre-selected
	if len(mock.DirPickerReqs) >= 2 {
		foundB := false
		for _, opt := range mock.DirPickerReqs[1].Options {
			if opt.Value == rootB && opt.Selected {
				foundB = true
			}
		}
		if !foundB {
			t.Fatalf("expected newly added %q to be pre-selected on return to directory list: %+v", rootB, mock.DirPickerReqs[1].Options)
		}
	}
	if mock.InputCalls != 1 {
		t.Fatalf("expected 1 Input call for add-dir, got %d", mock.InputCalls)
	}
	if !reflect.DeepEqual(res.ProjectsRoots, []string{rootA, rootB}) {
		t.Fatalf("ProjectsRoots = %v, want [%s %s]", res.ProjectsRoots, rootA, rootB)
	}
	if res.DiscoveredRepos != 3 {
		t.Fatalf("DiscoveredRepos = %d, want 3", res.DiscoveredRepos)
	}
	if res.DefaultEngine != "rex" {
		t.Fatalf("DefaultEngine = %q, want rex", res.DefaultEngine)
	}
	if res.OrchestratorAgent != "my-orch" || res.WorkerAgent != "agy" {
		t.Fatalf("agents = %q/%q, want my-orch/agy", res.OrchestratorAgent, res.WorkerAgent)
	}
	if res.T3Provider != "antigravity" || res.T3Model != "gemini-3.8-flash-medium" {
		t.Fatalf("T3 = %q/%q, want antigravity/gemini-3.8-flash-medium", res.T3Provider, res.T3Model)
	}
	if res.NotificationsEnabled {
		t.Fatalf("expected NotificationsEnabled=false when Step 4 advanced skipped")
	}
	if res.ConfigPath != filepath.Join(buddyDir, "config.json") {
		t.Fatalf("ConfigPath = %q", res.ConfigPath)
	}

	// Verify persisted config & assets
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.GetEffectiveOrchestratorAgent("rex") != "my-orch" {
		t.Fatalf("saved orchestrator = %q, want my-orch", cfg.GetEffectiveOrchestratorAgent("rex"))
	}
	if v, _ := cfg.Notifications["desktop_enabled"].(bool); v {
		t.Fatalf("expected saved notifications.desktop_enabled=false")
	}
	if _, err := os.Stat(filepath.Join(buddyDir, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buddyDir, ".agents", "hooks.json")); err != nil {
		t.Fatalf("hooks.json not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".agents", "skills", "buddy", "SKILL.md")); err != nil {
		t.Fatalf("universal SKILL.md not created: %v", err)
	}
}

func TestDeriveDefaultEngine_PrefersHostThenFirstInstalled(t *testing.T) {
	st := &ConfigFormState{
		Probe: &ProbeResult{
			DetectedHost: "opencode",
			HasRex:       true,
			HasOpenCode:  true,
			HasT3:        false,
		},
		SelectedEngines: []string{"rex", "opencode"},
	}
	if got := st.DeriveDefaultEngine(); got != "opencode" {
		t.Fatalf("DeriveDefaultEngine (host selected) = %q, want opencode", got)
	}

	st.Probe.DetectedHost = "unknown"
	st.SelectedEngines = []string{"t3", "opencode"}
	if got := st.DeriveDefaultEngine(); got != "opencode" {
		t.Fatalf("DeriveDefaultEngine (first installed) = %q, want opencode", got)
	}
}

func TestBuildPlatformOptions_ExcludesWezTerm(t *testing.T) {
	opts := buildPlatformOptions(&ProbeResult{HasRex: true, HasWezTerm: true, HasOpenCode: true}, []string{"rex"})
	if len(opts) != 3 {
		t.Fatalf("expected 3 platform options (rex, opencode, t3), got %d", len(opts))
	}
	for _, o := range opts {
		if o.Value == "wezterm" {
			t.Fatalf("wezterm must not appear in Step 2 platform options")
		}
	}
}

func TestMultiSelectHeight_AccountsForHeaderOffset(t *testing.T) {
	if got := multiSelectHeight(3, true); got != 5 {
		t.Fatalf("multiSelectHeight(3, true) = %d, want 5 (3 options + 2 header lines)", got)
	}
	if got := multiSelectHeight(3, false); got != 4 {
		t.Fatalf("multiSelectHeight(3, false) = %d, want 4 (3 options + 1 title line)", got)
	}
	if got := multiSelectHeight(20, true); got != 14 {
		t.Fatalf("multiSelectHeight(20, true) = %d, want 14 (capped at 12 + 2)", got)
	}
}

func TestDirPickerModel_InterceptsPlusAndA(t *testing.T) {
	for _, keyStr := range []string{"+", "a"} {
		var vals []string
		ms := huh.NewMultiSelect[string]().
			Title("Step 1/5 · Project directories").
			Options(huh.NewOption("~/Dev", "/tmp/dev").Selected(true)).
			Filterable(false).
			Value(&vals)
		form := huh.NewForm(huh.NewGroup(ms))
		m := &dirPickerModel{form: form, ms: ms}

		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(keyStr[0]), Text: keyStr})
		dm := updated.(*dirPickerModel)
		if !dm.addRequested {
			t.Fatalf("pressing %q should set addRequested=true", keyStr)
		}
		if cmd == nil {
			t.Fatalf("pressing %q should return tea.Quit cmd", keyStr)
		}
	}
}

func TestRun_AbortLeavesZeroMutations(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	rootA := filepath.Join(tmpHome, "Projects")
	_ = os.MkdirAll(filepath.Join(rootA, "app1"), 0755)

	mock := &MockPrompter{
		Interactive:   true,
		DirPickerResp: []DirPickerResp{{Selected: []string{rootA}}},
		AbortAtStep:   "configform",
	}

	_, err := Run(context.Background(), Options{
		Version:  "v0.2.0",
		HomeDir:  tmpHome,
		BuddyDir: buddyDir,
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Out:      &bytes.Buffer{},
		Prompter: mock,
	})
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("expected ErrAborted, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(buddyDir, "config.json")); !os.IsNotExist(statErr) {
		t.Fatalf("aborted setup must not write config.json")
	}
	if _, statErr := os.Stat(filepath.Join(buddyDir, "AGENTS.md")); !os.IsNotExist(statErr) {
		t.Fatalf("aborted setup must not write AGENTS.md")
	}
}

func TestRun_NonInteractiveEnvAndUpgradePreservesChoices(t *testing.T) {
	tmpHome := t.TempDir()
	buddyDir := filepath.Join(tmpHome, ".buddy")
	t.Setenv("HOME", tmpHome)
	t.Setenv("BUDDY_DIR", buddyDir)

	envMap := map[string]string{
		"BUDDY_PROJECTS_ROOTS":     filepath.Join(tmpHome, "r1") + ":" + filepath.Join(tmpHome, "r2"),
		"BUDDY_DEFAULT_ENGINE":     "opencode",
		"BUDDY_ORCHESTRATOR_AGENT": "opencode",
		"BUDDY_WORKER_AGENT":       "pi",
		"BUDDY_T3_MODEL":           "anthropic/claude-sonnet-4",
	}
	getenv := func(k string) string { return envMap[k] }

	res, err := Run(context.Background(), Options{
		Version:        "v0.2.0",
		NonInteractive: true,
		HomeDir:        tmpHome,
		BuddyDir:       buddyDir,
		Getenv:         getenv,
		LookPath:       func(string) (string, error) { return "", os.ErrNotExist },
		Out:            &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("non-interactive Run failed: %v", err)
	}
	if res.DefaultEngine != "opencode" || res.OrchestratorAgent != "opencode" || res.WorkerAgent != "pi" {
		t.Fatalf("unexpected non-interactive result: %+v", res)
	}
	if res.T3Provider != "anthropic" || res.T3Model != "claude-sonnet-4" {
		t.Fatalf("unexpected T3: %q/%q", res.T3Provider, res.T3Model)
	}

	// Now run --upgrade with empty env: must preserve all previous choices!
	upRes, err := Run(context.Background(), Options{
		Version:  "v0.2.1",
		Upgrade:  true,
		HomeDir:  tmpHome,
		BuddyDir: buddyDir,
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Out:      &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("upgrade Run failed: %v", err)
	}
	if upRes.Mode != "upgrade" {
		t.Fatalf("Mode = %q, want upgrade", upRes.Mode)
	}
	if upRes.DefaultEngine != "opencode" || upRes.WorkerAgent != "pi" {
		t.Fatalf("upgrade altered user choices: %+v", upRes)
	}
}
