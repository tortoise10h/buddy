package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/state"
)

// Options controls a `buddy-ctl setup` execution.
type Options struct {
	Version        string
	NonInteractive bool
	Upgrade        bool
	DryRun         bool
	Force          bool
	Only           []string // projects, engines, agents, skills, assets
	DevLink        string
	SkipSkills     bool
	SkipHooks      bool
	BuddyCtlPath   string

	// Configuration overrides (CLI flags)
	ProjectsRoots     []string
	ReplaceRoots      bool
	DefaultEngine     string
	OrchestratorAgent string
	WorkerAgent       string
	T3Provider        string
	T3Model           string
	OpenCodeURL       string
	OpenCodeAgent     string
	OpenCodeModel     string

	// Injectable dependencies for testability
	HomeDir  string
	BuddyDir string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Out      io.Writer
	Prompter Prompter
}

// Result summarizes what `buddy-ctl setup` configured and provisioned.
type Result struct {
	Status               string           `json:"status"`
	Mode                 string           `json:"mode"` // "interactive", "non-interactive", "upgrade"
	DryRun               bool             `json:"dry_run,omitempty"`
	BuddyDir             string           `json:"buddy_dir"`
	ConfigPath           string           `json:"config_path"`
	ProjectsRoots        []string         `json:"projects_roots"`
	DiscoveredRepos      int              `json:"discovered_repos"`
	SelectedEngines      []string         `json:"selected_engines,omitempty"`
	DefaultEngine        string           `json:"default_engine"`
	OrchestratorAgent    string           `json:"orchestrator_agent"`
	WorkerAgent          string           `json:"worker_agent"`
	T3Provider           string           `json:"t3_provider,omitempty"`
	T3Model              string           `json:"t3_model,omitempty"`
	OpenCodeModel        string           `json:"opencode_model,omitempty"`
	OpenCodeAgent        string           `json:"opencode_agent,omitempty"`
	NotificationsEnabled bool             `json:"notifications_enabled"`
	Provision            *ProvisionReport `json:"provision,omitempty"`
	PathWarning          string           `json:"path_warning,omitempty"`
}

// Run executes the 5-step Buddy setup workflow.
func Run(ctx context.Context, opts Options) (*Result, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	homeDir := opts.HomeDir
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	buddyDir := opts.BuddyDir
	if buddyDir == "" {
		buddyDir = state.GetBuddyDir()
	}
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	prompter := opts.Prompter
	if prompter == nil {
		if opts.NonInteractive || opts.Upgrade {
			prompter = NewNonInteractivePrompter()
		} else {
			prompter = NewHuhPrompter(os.Stdin, out, false)
		}
	}

	sectionSet := parseOnlySections(opts.Only)
	wantSection := func(name string) bool {
		if len(sectionSet) == 0 {
			return true
		}
		return sectionSet[name]
	}

	// Load existing config (if present)
	configPath := filepath.Join(buddyDir, "config.json")
	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	var existingCfg *config.Config
	if configExists {
		var err error
		existingCfg, err = config.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load existing config.json: %w", err)
		}
	}

	// Environment Probe
	var probe *ProbeResult
	if err := prompter.Spin(ctx, "Scanning local environment...", func(c context.Context) error {
		probe = Probe(c, ProbeOptions{
			HomeDir:        homeDir,
			BuddyDir:       buddyDir,
			Getenv:         getenv,
			LookPath:       opts.LookPath,
			ExistingConfig: existingCfg,
			ConfigExists:   &configExists,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	if prompter.IsInteractive() && !opts.Upgrade {
		fmt.Fprintln(out, FormatProbeBanner(opts.Version, probe))
		fmt.Fprintln(out)
	}

	// Start with existing config or defaults
	cfg := config.DefaultConfig()
	if existingCfg != nil {
		cfg = existingCfg
	} else {
		cfg.ProjectsRoots = nil
	}

	modeName := "interactive"
	if opts.Upgrade {
		modeName = "upgrade"
	} else if !prompter.IsInteractive() || opts.NonInteractive {
		modeName = "non-interactive"
	}

	// Step 1: Project Directories (with interactive "+ Add directory..." loop)
	var selectedRoots []string
	if opts.Upgrade {
		selectedRoots = append([]string(nil), cfg.ProjectsRoots...)
		if len(selectedRoots) == 0 {
			selectedRoots = []string{ExpandPath("~/Dev/Personal/Projects", homeDir)}
		}
	} else if wantSection("projects") {
		var err error
		selectedRoots, err = resolveProjectRoots(ctx, opts, probe, cfg, homeDir, getenv, prompter, out)
		if err != nil {
			return nil, err
		}
	} else {
		selectedRoots = append([]string(nil), cfg.ProjectsRoots...)
	}

	// Build initial ConfigFormState using precedence: CLI flags > env vars > existing config > auto-detected defaults
	t3Prov, t3Mod := cfg.GetT3ProviderModel()
	_, _, ocAgent, ocModel, _ := cfg.GetOpenCodeConfig()

	explicitEngine := firstNonEmpty(opts.DefaultEngine, getenv("BUDDY_DEFAULT_ENGINE"))
	defEngine := firstNonEmpty(
		explicitEngine,
		existingEngineOrEmpty(existingCfg),
		probe.RecommendedEngine(""),
	)
	orchAgent := firstNonEmpty(
		opts.OrchestratorAgent,
		getenv("BUDDY_ORCHESTRATOR_AGENT"),
		existingTerminalAgent(existingCfg, true),
		probe.RecommendedHarness(""),
	)
	workerAgent := firstNonEmpty(
		opts.WorkerAgent,
		getenv("BUDDY_WORKER_AGENT"),
		existingTerminalAgent(existingCfg, false),
		orchAgent,
	)

	// Resolve T3 provider/model from flags/env/existing
	envT3Prov := strings.TrimSpace(getenv("BUDDY_T3_PROVIDER"))
	envT3Mod := strings.TrimSpace(getenv("BUDDY_T3_MODEL"))
	flagT3Prov := strings.TrimSpace(opts.T3Provider)
	flagT3Mod := strings.TrimSpace(opts.T3Model)
	if flagT3Prov != "" || flagT3Mod != "" {
		t3Prov, t3Mod = splitProviderModel(flagT3Prov, flagT3Mod, t3Prov)
	} else if envT3Prov != "" || envT3Mod != "" {
		t3Prov, t3Mod = splitProviderModel(envT3Prov, envT3Mod, t3Prov)
	}
	t3Spec := ""
	if t3Prov != "" && t3Mod != "" {
		t3Spec = t3Prov + "/" + t3Mod
	}

	// Resolve OpenCode defaults from flags/env/existing
	ocURL := firstNonEmpty(opts.OpenCodeURL, getenv("BUDDY_OPENCODE_URL"), cfg.GetEngineConfig("opencode").ServerURL)
	ocAgent = firstNonEmpty(opts.OpenCodeAgent, getenv("BUDDY_OPENCODE_AGENT"), ocAgent)
	ocModel = firstNonEmpty(opts.OpenCodeModel, getenv("BUDDY_OPENCODE_MODEL"), ocModel)

	// Default platform selection for Step 2 (only rex, opencode, t3)
	initialPlatforms := defaultSelectedPlatforms(probe, defEngine)

	// Notifications default: false on fresh interactive setup (Step 4 default); preserve on upgrade
	notificationsDefault := false
	if opts.Upgrade && existingCfg != nil && existingCfg.Notifications != nil {
		if v, ok := existingCfg.Notifications["desktop_enabled"].(bool); ok {
			notificationsDefault = v
		}
	}

	formState := &ConfigFormState{
		ShowEngines:          !opts.Upgrade && wantSection("engines"),
		ShowAgents:           !opts.Upgrade && wantSection("agents"),
		ShowSkills:           !opts.Upgrade && !opts.SkipSkills && wantSection("skills"),
		ShowAdvanced:         !opts.Upgrade && len(sectionSet) == 0,
		Probe:                probe,
		SelectedEngines:      initialPlatforms,
		OrchestratorAgent:    orchAgent,
		WorkerAgent:          workerAgent,
		OpenCodeModel:        ocModel,
		T3ModelSpec:          t3Spec,
		ConfigureAdvanced:    false,
		NotificationsEnabled: notificationsDefault,
		SkillTargets:         probe.DefaultSelectedSkillTargetDirs(),
	}

	// Steps 2-4: Platforms -> Per-Engine Config -> Optional/Advanced Config
	if !opts.Upgrade && prompter.IsInteractive() {
		if err := prompter.RunConfigForm(ctx, formState); err != nil {
			return nil, err
		}
	}

	// Apply custom harness overrides if selected
	if formState.OrchestratorAgent == CustomSentinel {
		formState.OrchestratorAgent = strings.TrimSpace(formState.OrchestratorCustom)
	}
	if formState.WorkerAgent == CustomSentinel {
		formState.WorkerAgent = strings.TrimSpace(formState.WorkerCustom)
	}

	// Derive default_engine from Step 2 selection in interactive mode (unless overridden via flag/env)
	resolvedDefaultEngine := defEngine
	if !opts.Upgrade && prompter.IsInteractive() && wantSection("engines") {
		if explicitEngine != "" {
			resolvedDefaultEngine = explicitEngine
		} else {
			resolvedDefaultEngine = formState.DeriveDefaultEngine()
		}
	}

	// Parse back T3 model spec (only if t3 was selected in Step 2 or set via flags/env)
	if formState.HasSelectedEngine("t3") || flagT3Prov != "" || flagT3Mod != "" || envT3Prov != "" || envT3Mod != "" {
		if strings.TrimSpace(formState.T3ModelSpec) != "" {
			t3Prov, t3Mod = splitProviderModel("", formState.T3ModelSpec, t3Prov)
		} else if !opts.Upgrade && wantSection("engines") && prompter.IsInteractive() && formState.HasSelectedEngine("t3") {
			t3Prov, t3Mod = "", ""
		}
	}

	// Update cfg struct
	if len(selectedRoots) > 0 {
		cfg.ProjectsRoots = selectedRoots
	}
	if !opts.Upgrade {
		if wantSection("engines") {
			cfg.DefaultEngine = resolvedDefaultEngine
			if cfg.Engines == nil {
				cfg.Engines = make(map[string]config.EngineConfig)
			}
			if t3Prov != "" || t3Mod != "" {
				t3Cfg := cfg.Engines["t3"]
				t3Cfg.Provider = t3Prov
				t3Cfg.Model = t3Mod
				cfg.Engines["t3"] = t3Cfg
			}
			if ocURL != "" || ocAgent != "" || formState.OpenCodeModel != "" {
				ocCfg := cfg.Engines["opencode"]
				if ocURL != "" {
					ocCfg.ServerURL = ocURL
				}
				if ocAgent != "" {
					ocCfg.DefaultAgent = strings.TrimSpace(ocAgent)
				}
				if formState.OpenCodeModel != "" {
					p, m := splitProviderModel("", formState.OpenCodeModel, "")
					if p != "" && m != "" {
						ocCfg.Provider = p
						ocCfg.Model = m
					} else {
						ocCfg.Model = strings.TrimSpace(formState.OpenCodeModel)
					}
				}
				cfg.Engines["opencode"] = ocCfg
			}
		}
		if wantSection("agents") && (formState.HasSelectedEngine("rex") || !prompter.IsInteractive()) {
			cfg.SetTerminalAgents(formState.OrchestratorAgent, formState.WorkerAgent)
		}
		if len(sectionSet) == 0 {
			if cfg.Notifications == nil {
				cfg.Notifications = make(map[string]any)
			}
			cfg.Notifications["desktop_enabled"] = formState.NotificationsEnabled
		}
	}

	// Resolve buddy-ctl path for hooks.json
	buddyCtlPath := strings.TrimSpace(opts.BuddyCtlPath)
	if buddyCtlPath == "" {
		buddyCtlPath = probe.BuddyCtlPath
	}

	// Resolve skill target dirs for provisioning
	skillDirs := formState.SkillTargets
	if opts.Upgrade || (!formState.ConfigureAdvanced && formState.ShowAdvanced) {
		skillDirs = probe.DefaultSelectedSkillTargetDirs()
	}

	// Step 5: Persist config, state, and managed assets
	var provReport *ProvisionReport
	shouldProvisionAssets := opts.Upgrade || wantSection("assets") || wantSection("skills")

	if err := prompter.Spin(ctx, "Provisioning ~/.buddy...", func(_ context.Context) error {
		if !opts.DryRun {
			if err := os.MkdirAll(buddyDir, 0755); err != nil {
				return err
			}
			if !opts.Upgrade || !configExists {
				if err := config.SaveConfig(cfg); err != nil {
					return fmt.Errorf("failed to save config.json: %w", err)
				}
			}
			// Ensure active engine state file exists
			activeEngine := cfg.DefaultEngine
			if activeEngine == "" {
				activeEngine = "rex"
			}
			_, _ = state.LoadStateForEngine(activeEngine)
			statePath := state.GetStatePathForEngine(activeEngine)
			if _, err := os.Stat(statePath); os.IsNotExist(err) {
				st := &state.State{
					Version:  1,
					Projects: make(map[string]state.ProjectState),
				}
				if err := state.SaveStateForEngine(activeEngine, st); err != nil {
					return fmt.Errorf("failed to initialize state file: %w", err)
				}
			}
		}

		if shouldProvisionAssets {
			var err error
			provReport, err = ProvisionAssets(ProvisionOptions{
				BuddyDir:        buddyDir,
				HomeDir:         homeDir,
				Version:         opts.Version,
				BuddyCtlPath:    buddyCtlPath,
				DevRepo:         opts.DevLink,
				SkillTargetDirs: skillDirs,
				SkipSkills:      opts.SkipSkills || (!opts.Upgrade && !wantSection("skills")),
				SkipHooks:       opts.SkipHooks,
				Force:           opts.Force,
				DryRun:          opts.DryRun,
			})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Count discovered projects across configured roots
	discovered, _ := project.ListProjects(cfg.ProjectsRoots, "", nil)

	pathWarning := ""
	if !probe.BuddyCtlInPATH && !probe.LocalBinInPATH {
		shellRC := "~/.zshrc"
		if strings.Contains(getenv("SHELL"), "bash") {
			shellRC = "~/.bashrc"
		}
		pathWarning = fmt.Sprintf("~/.local/bin is not in your PATH. Add it to %s:\n  export PATH=\"$HOME/.local/bin:$PATH\"", shellRC)
	}

	notifEnabled := false
	if cfg.Notifications != nil {
		if v, ok := cfg.Notifications["desktop_enabled"].(bool); ok {
			notifEnabled = v
		}
	}

	res := &Result{
		Status:               "ok",
		Mode:                 modeName,
		DryRun:               opts.DryRun,
		BuddyDir:             buddyDir,
		ConfigPath:           configPath,
		ProjectsRoots:        cfg.ProjectsRoots,
		DiscoveredRepos:      len(discovered),
		SelectedEngines:      formState.SelectedEngines,
		DefaultEngine:        cfg.DefaultEngine,
		OrchestratorAgent:    cfg.GetEffectiveOrchestratorAgent("rex"),
		WorkerAgent:          cfg.GetEffectiveWorkerAgent("rex"),
		T3Provider:           t3Prov,
		T3Model:              t3Mod,
		OpenCodeModel:        formState.OpenCodeModel,
		OpenCodeAgent:        ocAgent,
		NotificationsEnabled: notifEnabled,
		Provision:            provReport,
		PathWarning:          pathWarning,
	}

	return res, nil
}

func defaultSelectedPlatforms(probe *ProbeResult, existingDefault string) []string {
	var selected []string
	seen := make(map[string]bool)
	add := func(name string) {
		if name != "rex" && name != "opencode" && name != "t3" {
			return
		}
		if !seen[name] {
			seen[name] = true
			selected = append(selected, name)
		}
	}

	if probe != nil {
		if probe.HasRex {
			add("rex")
		}
		if probe.HasOpenCode {
			add("opencode")
		}
		if probe.HasT3 {
			add("t3")
		}
	}
	if existingDefault != "" {
		add(existingDefault)
	}
	if len(selected) == 0 {
		add("rex")
	}
	return selected
}

func resolveProjectRoots(
	ctx context.Context,
	opts Options,
	probe *ProbeResult,
	cfg *config.Config,
	homeDir string,
	getenv func(string) string,
	prompter Prompter,
	out io.Writer,
) ([]string, error) {
	// Collect explicit CLI/env roots first
	var explicit []string
	for _, r := range opts.ProjectsRoots {
		if exp := ExpandPath(r, homeDir); exp != "" {
			explicit = append(explicit, exp)
		}
	}
	if envRoots := getenv("BUDDY_PROJECTS_ROOTS"); envRoots != "" {
		for _, r := range SplitEnvRoots(envRoots) {
			if exp := ExpandPath(r, homeDir); exp != "" {
				explicit = append(explicit, exp)
			}
		}
	}

	// Non-interactive path
	if !prompter.IsInteractive() || opts.NonInteractive {
		var roots []string
		if opts.ReplaceRoots && len(explicit) > 0 {
			roots = MergeUniqueStrings(explicit)
		} else {
			roots = MergeUniqueStrings(cfg.ProjectsRoots, explicit)
		}
		if len(roots) == 0 {
			if len(probe.CandidateRoots) > 0 {
				roots = []string{probe.CandidateRoots[0].Path}
			} else {
				roots = []string{ExpandPath("~/Dev/Personal/Projects", homeDir)}
			}
		}
		if !opts.DryRun {
			for _, r := range roots {
				_ = os.MkdirAll(r, 0755)
			}
		}
		return roots, nil
	}

	// Interactive Step 1: Directories list with "+ Add directory..." loop
	type dirItem struct {
		Path        string
		DisplayPath string
		RepoCount   int
		Exists      bool
		Selected    bool
	}

	var items []dirItem
	seenPaths := make(map[string]int)
	addOrSelect := func(abs string, selected bool) {
		abs = filepath.Clean(abs)
		if idx, ok := seenPaths[abs]; ok {
			if selected {
				items[idx].Selected = true
			}
			return
		}
		fi, err := os.Stat(abs)
		exists := err == nil && fi.IsDir()
		count := 0
		if exists {
			count = countSubdirs(abs)
		}
		seenPaths[abs] = len(items)
		items = append(items, dirItem{
			Path:        abs,
			DisplayPath: FormatHomePath(abs, homeDir),
			RepoCount:   count,
			Exists:      exists,
			Selected:    selected,
		})
	}

	for _, cr := range probe.CandidateRoots {
		addOrSelect(cr.Path, cr.Configured)
	}
	for _, r := range explicit {
		addOrSelect(r, true)
	}
	// If nothing is pre-selected yet and we have candidates, pre-select the first existing one
	hasSelected := false
	for _, it := range items {
		if it.Selected {
			hasSelected = true
			break
		}
	}
	if !hasSelected && len(items) > 0 {
		items[0].Selected = true
	}

	promptAddDir := func() error {
		rawCustom, ierr := prompter.Input(ctx, InputReq{
			Title:       "Add project directory",
			Description: "Enter one or more paths (comma or space separated). Press Enter with empty input to go back.",
			Placeholder: "~/Dev/Work",
		})
		if ierr != nil {
			return ierr
		}
		for _, tok := range ParseRootInputs(rawCustom) {
			abs := ExpandPath(tok, homeDir)
			if abs == "" {
				continue
			}
			if fi, statErr := os.Stat(abs); statErr == nil && fi.IsDir() {
				addOrSelect(abs, true)
				continue
			}
			create, cerr := prompter.Confirm(ctx, ConfirmReq{
				Title:       fmt.Sprintf("Directory '%s' does not exist. Create it?", FormatHomePath(abs, homeDir)),
				Affirmative: "Create",
				Negative:    "Skip",
				Default:     true,
			})
			if cerr != nil {
				return cerr
			}
			if create {
				if !opts.DryRun {
					if mkErr := os.MkdirAll(abs, 0755); mkErr != nil {
						fmt.Fprintf(out, "✖ Could not create '%s': %v\n", abs, mkErr)
						continue
					}
				}
				addOrSelect(abs, true)
			}
		}
		return nil
	}

	for {
		// If no candidate directories exist yet, go straight to adding one
		if len(items) == 0 {
			if err := promptAddDir(); err != nil {
				return nil, err
			}
			if len(items) == 0 {
				fmt.Fprintln(out, "✖ Please enter at least one project directory.")
			}
			continue
		}

		msOpts := make([]Option, 0, len(items))
		for _, it := range items {
			label := it.DisplayPath
			if it.Exists {
				label = fmt.Sprintf("%-34s (%d repos)", it.DisplayPath, it.RepoCount)
			} else {
				label = fmt.Sprintf("%-34s (new)", it.DisplayPath)
			}
			msOpts = append(msOpts, Option{
				Label:    label,
				Value:    it.Path,
				Selected: it.Selected,
			})
		}

		resp, err := prompter.PickDirectories(ctx, DirPickerReq{
			Title:       "Step 1/5 · Project directories",
			Description: "space/x toggle • + add dir • enter continue",
			Options:     msOpts,
		})
		if err != nil {
			return nil, err
		}

		// Sync selection state
		pickedSet := make(map[string]bool, len(resp.Selected))
		for _, v := range resp.Selected {
			pickedSet[v] = true
		}
		for i := range items {
			items[i].Selected = pickedSet[items[i].Path]
		}

		if resp.AddRequested {
			if err := promptAddDir(); err != nil {
				return nil, err
			}
			// Return right back to the directories list with the new directory shown and selected
			continue
		}

		var chosen []string
		for _, it := range items {
			if it.Selected {
				chosen = append(chosen, it.Path)
			}
		}
		if len(chosen) > 0 {
			discovered, _ := project.ListProjects(chosen, "", nil)
			fmt.Fprintf(out, "✔ %d projects discovered across %d root(s)\n\n", len(discovered), len(chosen))
			return chosen, nil
		}
		fmt.Fprintln(out, "✖ Please select at least one directory (or press '+' to add one).")
	}
}

func parseOnlySections(only []string) map[string]bool {
	set := make(map[string]bool)
	for _, raw := range only {
		for _, part := range strings.Split(raw, ",") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p != "" {
				set[p] = true
			}
		}
	}
	return set
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func existingEngineOrEmpty(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.DefaultEngine)
}

func existingTerminalAgent(cfg *config.Config, orchestrator bool) string {
	if cfg == nil || cfg.Engines == nil {
		return ""
	}
	ec := cfg.Engines["rex"]
	if orchestrator {
		return strings.TrimSpace(ec.OrchestratorAgent)
	}
	return strings.TrimSpace(ec.WorkerAgent)
}

func splitProviderModel(prov, model, fallbackProv string) (string, string) {
	prov = strings.TrimSpace(prov)
	model = strings.TrimSpace(model)
	if prov == "" && strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if prov == "" {
		prov = fallbackProv
	}
	return prov, model
}
