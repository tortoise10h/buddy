package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/project"
	"github.com/tortoise10h/buddy/internal/setup"
	"github.com/tortoise10h/buddy/internal/state"
)

const (
	StatusPass = "pass"
	StatusWarn = "warn"
	StatusFail = "fail"
	StatusInfo = "info"
)

// CheckResult represents one diagnostic check outcome.
type CheckResult struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	FixHint  string `json:"fix_hint,omitempty"`
	Fixed    bool   `json:"fixed,omitempty"`
}

// Summary counts outcomes by status.
type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Info int `json:"info"`
}

// Report is the complete output of `buddy-ctl doctor`.
type Report struct {
	Status  string        `json:"status"` // "healthy", "warnings", "unhealthy"
	Summary Summary       `json:"summary"`
	Checks  []CheckResult `json:"checks"`
}

// Options controls `doctor.Run`.
type Options struct {
	HomeDir  string
	BuddyDir string
	Version  string
	Fix      bool
	Force    bool
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// Run executes all Buddy diagnostic checks, optionally applying safe repairs when opts.Fix is true.
func Run(ctx context.Context, opts Options) *Report {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	homeDir := opts.HomeDir
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	buddyDir := opts.BuddyDir
	if buddyDir == "" {
		buddyDir = state.GetBuddyDir()
	}

	var checks []CheckResult
	add := func(c CheckResult) {
		checks = append(checks, c)
	}

	// Load config if present
	configPath := filepath.Join(buddyDir, "config.json")
	var cfg *config.Config
	cfgData, cfgReadErr := os.ReadFile(configPath)
	cfgExists := cfgReadErr == nil
	if cfgExists {
		var loadErr error
		cfg, loadErr = config.LoadConfig()
		if loadErr != nil {
			cfg = nil
		}
	}

	probe := setup.Probe(ctx, setup.ProbeOptions{
		HomeDir:        homeDir,
		BuddyDir:       buddyDir,
		Getenv:         getenv,
		LookPath:       lookPath,
		ExistingConfig: cfg,
		ConfigExists:   &cfgExists,
	})

	// 1. Binaries & PATH
	if probe.BuddyCtlInPATH {
		add(CheckResult{
			Category: "Binaries & PATH",
			Name:     "buddy-ctl",
			Status:   StatusPass,
			Message:  fmt.Sprintf("found on PATH (%s)", setup.FormatHomePath(probe.BuddyCtlPath, homeDir)),
		})
	} else {
		shellRC := "~/.zshrc"
		if strings.Contains(getenv("SHELL"), "bash") {
			shellRC = "~/.bashrc"
		}
		add(CheckResult{
			Category: "Binaries & PATH",
			Name:     "buddy-ctl",
			Status:   StatusFail,
			Message:  "not found on PATH",
			FixHint:  fmt.Sprintf(`Add ~/.local/bin to %s: export PATH="$HOME/.local/bin:$PATH"`, shellRC),
		})
	}

	if probe.BuddyBinInPATH {
		add(CheckResult{
			Category: "Binaries & PATH",
			Name:     "buddy launcher",
			Status:   StatusPass,
			Message:  fmt.Sprintf("found on PATH (%s)", setup.FormatHomePath(probe.BuddyBinPath, homeDir)),
		})
	} else if probe.BuddyBinPath != "" {
		add(CheckResult{
			Category: "Binaries & PATH",
			Name:     "buddy launcher",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("exists at %s but not on PATH", setup.FormatHomePath(probe.BuddyBinPath, homeDir)),
			FixHint:  `Add ~/.local/bin to PATH`,
		})
	} else {
		add(CheckResult{
			Category: "Binaries & PATH",
			Name:     "buddy launcher",
			Status:   StatusWarn,
			Message:  "not found on PATH",
			FixHint:  "Re-run ./install.sh to install ~/.local/bin/buddy",
		})
	}

	// 2. Control Tower (~/.buddy/)
	if os.IsNotExist(cfgReadErr) {
		if opts.Fix {
			def := config.DefaultConfig()
			if err := config.SaveConfig(def); err == nil {
				cfg = def
				add(CheckResult{
					Category: "Control Tower",
					Name:     "config.json",
					Status:   StatusPass,
					Message:  "created default ~/.buddy/config.json",
					Fixed:    true,
				})
			} else {
				add(CheckResult{
					Category: "Control Tower",
					Name:     "config.json",
					Status:   StatusFail,
					Message:  fmt.Sprintf("missing and could not create: %v", err),
				})
			}
		} else {
			add(CheckResult{
				Category: "Control Tower",
				Name:     "config.json",
				Status:   StatusWarn,
				Message:  "not found (using built-in defaults)",
				FixHint:  "Run `buddy-ctl setup` or `buddy-ctl doctor --fix`",
			})
		}
	} else if cfgReadErr != nil {
		add(CheckResult{
			Category: "Control Tower",
			Name:     "config.json",
			Status:   StatusFail,
			Message:  fmt.Sprintf("unreadable: %v", cfgReadErr),
		})
	} else {
		var tmp map[string]any
		if err := json.Unmarshal(cfgData, &tmp); err != nil {
			add(CheckResult{
				Category: "Control Tower",
				Name:     "config.json",
				Status:   StatusFail,
				Message:  fmt.Sprintf("invalid JSON: %v", err),
				FixHint:  "Fix syntax in ~/.buddy/config.json or run `buddy-ctl init --force`",
			})
		} else {
			add(CheckResult{
				Category: "Control Tower",
				Name:     "config.json",
				Status:   StatusPass,
				Message:  setup.FormatHomePath(configPath, homeDir),
			})
		}
	}

	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// AGENTS.md check
	agentsPath := filepath.Join(buddyDir, "AGENTS.md")
	add(checkAgentsMD(agentsPath, buddyDir, homeDir, opts, probe))

	// hooks.json check
	hooksPath := filepath.Join(buddyDir, ".agents", "hooks.json")
	add(checkHooksJSON(hooksPath, buddyDir, homeDir, opts, probe))

	// state directory check
	activeEngine := cfg.DefaultEngine
	if activeEngine == "" {
		activeEngine = "rex"
	}
	statePath := state.GetStatePathForEngine(activeEngine)
	if _, err := state.LoadStateForEngine(activeEngine); err != nil {
		add(CheckResult{
			Category: "Control Tower",
			Name:     "state/" + state.NormalizeEngine(activeEngine) + ".json",
			Status:   StatusFail,
			Message:  fmt.Sprintf("invalid state file: %v", err),
			FixHint:  "Inspect or reset " + setup.FormatHomePath(statePath, homeDir),
		})
	} else {
		add(CheckResult{
			Category: "Control Tower",
			Name:     "state/" + state.NormalizeEngine(activeEngine) + ".json",
			Status:   StatusPass,
			Message:  "valid (" + setup.FormatHomePath(statePath, homeDir) + ")",
		})
	}

	// 3. Agent Skills
	add(checkUniversalSkill(buddyDir, homeDir, opts, probe))

	// 4. Project Roots
	add(checkProjectRoots(cfg, homeDir))

	// 5. Engines
	checks = append(checks, checkEngines(cfg, probe)...)

	// 6. Agent Harnesses
	checks = append(checks, checkHarnesses(cfg, probe, lookPath)...)

	// Build summary
	report := &Report{Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case StatusPass:
			report.Summary.Pass++
		case StatusWarn:
			report.Summary.Warn++
		case StatusFail:
			report.Summary.Fail++
		case StatusInfo:
			report.Summary.Info++
		}
	}
	switch {
	case report.Summary.Fail > 0:
		report.Status = "unhealthy"
	case report.Summary.Warn > 0:
		report.Status = "warnings"
	default:
		report.Status = "healthy"
	}
	return report
}

func checkAgentsMD(agentsPath, buddyDir, homeDir string, opts Options, probe *setup.ProbeResult) CheckResult {
	lfi, lerr := os.Lstat(agentsPath)
	isBrokenLink := lerr == nil && lfi.Mode()&os.ModeSymlink != 0 && func() bool {
		_, err := os.Stat(agentsPath)
		return err != nil
	}()
	if os.IsNotExist(lerr) || isBrokenLink {
		reason := "missing"
		if isBrokenLink {
			reason = "broken symlink"
		}
		if opts.Fix {
			_, err := setup.ProvisionAssets(setup.ProvisionOptions{
				BuddyDir:     buddyDir,
				HomeDir:      homeDir,
				Version:      opts.Version,
				BuddyCtlPath: probe.BuddyCtlPath,
				SkipSkills:   true,
				SkipHooks:    true,
				Force:        opts.Force,
			})
			if err == nil {
				return CheckResult{
					Category: "Control Tower",
					Name:     "AGENTS.md",
					Status:   StatusPass,
					Message:  fmt.Sprintf("repaired (%s -> materialized)", reason),
					Fixed:    true,
				}
			}
		}
		return CheckResult{
			Category: "Control Tower",
			Name:     "AGENTS.md",
			Status:   StatusFail,
			Message:  reason,
			FixHint:  "Run `buddy-ctl doctor --fix` or `buddy-ctl setup --only assets`",
		}
	}
	if lerr != nil {
		return CheckResult{
			Category: "Control Tower",
			Name:     "AGENTS.md",
			Status:   StatusFail,
			Message:  fmt.Sprintf("unreadable: %v", lerr),
		}
	}
	detail := setup.FormatHomePath(agentsPath, homeDir)
	if lfi.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(agentsPath); err == nil {
			detail += " -> " + setup.FormatHomePath(target, homeDir)
		}
	}
	return CheckResult{
		Category: "Control Tower",
		Name:     "AGENTS.md",
		Status:   StatusPass,
		Message:  detail,
	}
}

func checkHooksJSON(hooksPath, buddyDir, homeDir string, opts Options, probe *setup.ProbeResult) CheckResult {
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if opts.Fix {
			_, perr := setup.ProvisionAssets(setup.ProvisionOptions{
				BuddyDir:     buddyDir,
				HomeDir:      homeDir,
				Version:      opts.Version,
				BuddyCtlPath: probe.BuddyCtlPath,
				SkipSkills:   true,
				Force:        true,
			})
			if perr == nil {
				return CheckResult{
					Category: "Control Tower",
					Name:     ".agents/hooks.json",
					Status:   StatusPass,
					Message:  "generated hooks.json",
					Fixed:    true,
				}
			}
		}
		return CheckResult{
			Category: "Control Tower",
			Name:     ".agents/hooks.json",
			Status:   StatusFail,
			Message:  "missing or unreadable",
			FixHint:  "Run `buddy-ctl doctor --fix` or `buddy-ctl setup --only assets`",
		}
	}

	var parsed map[string]struct {
		PreInvocation []struct {
			Command string `json:"command"`
		} `json:"PreInvocation"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return CheckResult{
			Category: "Control Tower",
			Name:     ".agents/hooks.json",
			Status:   StatusFail,
			Message:  fmt.Sprintf("invalid JSON: %v", err),
			FixHint:  "Run `buddy-ctl doctor --fix --force`",
		}
	}

	// Verify hook command binary exists
	hookCmd := ""
	for _, entry := range parsed {
		for _, h := range entry.PreInvocation {
			if fields := strings.Fields(h.Command); len(fields) > 0 {
				hookCmd = fields[0]
				break
			}
		}
	}
	if hookCmd != "" {
		if _, statErr := os.Stat(hookCmd); statErr != nil {
			if _, lookErr := exec.LookPath(hookCmd); lookErr != nil {
				if opts.Fix && probe.BuddyCtlPath != "" {
					_, perr := setup.ProvisionAssets(setup.ProvisionOptions{
						BuddyDir:     buddyDir,
						HomeDir:      homeDir,
						Version:      opts.Version,
						BuddyCtlPath: probe.BuddyCtlPath,
						SkipSkills:   true,
						Force:        true,
					})
					if perr == nil {
						return CheckResult{
							Category: "Control Tower",
							Name:     ".agents/hooks.json",
							Status:   StatusPass,
							Message:  fmt.Sprintf("repaired stale hook binary -> %s", setup.FormatHomePath(probe.BuddyCtlPath, homeDir)),
							Fixed:    true,
						}
					}
				}
				return CheckResult{
					Category: "Control Tower",
					Name:     ".agents/hooks.json",
					Status:   StatusFail,
					Message:  fmt.Sprintf("hook binary not found: %s", hookCmd),
					FixHint:  "Run `buddy-ctl doctor --fix` to update hook path",
				}
			}
		}
	}

	return CheckResult{
		Category: "Control Tower",
		Name:     ".agents/hooks.json",
		Status:   StatusPass,
		Message:  fmt.Sprintf("valid (command: %s)", setup.FormatHomePath(hookCmd, homeDir)),
	}
}

func checkUniversalSkill(buddyDir, homeDir string, opts Options, probe *setup.ProbeResult) CheckResult {
	universalDir := filepath.Join(homeDir, ".agents", "skills", "buddy")
	skillFile := filepath.Join(universalDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err != nil {
		if opts.Fix {
			_, perr := setup.ProvisionAssets(setup.ProvisionOptions{
				BuddyDir:        buddyDir,
				HomeDir:         homeDir,
				Version:         opts.Version,
				BuddyCtlPath:    probe.BuddyCtlPath,
				SkillTargetDirs: probe.DefaultSelectedSkillTargetDirs(),
				SkipHooks:       true,
				Force:           opts.Force,
			})
			if perr == nil {
				return CheckResult{
					Category: "Agent Skills",
					Name:     "buddy skill",
					Status:   StatusPass,
					Message:  "installed " + setup.FormatHomePath(skillFile, homeDir),
					Fixed:    true,
				}
			}
		}
		return CheckResult{
			Category: "Agent Skills",
			Name:     "buddy skill",
			Status:   StatusFail,
			Message:  fmt.Sprintf("missing %s", setup.FormatHomePath(skillFile, homeDir)),
			FixHint:  "Run `buddy-ctl doctor --fix` or `buddy-ctl setup --only skills`",
		}
	}

	installedCount := 0
	for _, t := range probe.SkillTargets {
		if _, err := os.Stat(filepath.Join(t.Dir, "SKILL.md")); err == nil {
			installedCount++
		}
	}
	return CheckResult{
		Category: "Agent Skills",
		Name:     "buddy skill",
		Status:   StatusPass,
		Message:  fmt.Sprintf("installed in %d target(s) including %s", installedCount, setup.FormatHomePath(universalDir, homeDir)),
	}
}

func checkProjectRoots(cfg *config.Config, homeDir string) CheckResult {
	if len(cfg.ProjectsRoots) == 0 {
		return CheckResult{
			Category: "Project Roots",
			Name:     "projects_roots",
			Status:   StatusFail,
			Message:  "no project roots configured",
			FixHint:  "Run `buddy-ctl setup --only projects` or `buddy-ctl projects add-root <path>`",
		}
	}

	validRoots := 0
	var missing []string
	for _, r := range cfg.ProjectsRoots {
		abs := setup.ExpandPath(r, homeDir)
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			validRoots++
		} else {
			missing = append(missing, setup.FormatHomePath(abs, homeDir))
		}
	}

	if validRoots == 0 {
		return CheckResult{
			Category: "Project Roots",
			Name:     "projects_roots",
			Status:   StatusFail,
			Message:  fmt.Sprintf("none of the %d configured root(s) exist: %s", len(cfg.ProjectsRoots), strings.Join(missing, ", ")),
			FixHint:  "Run `buddy-ctl setup --only projects`",
		}
	}

	projects, _ := project.ListProjects(cfg.ProjectsRoots, "", nil)
	if len(missing) > 0 {
		return CheckResult{
			Category: "Project Roots",
			Name:     "projects_roots",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("%d repo(s) across %d valid root(s); missing: %s", len(projects), validRoots, strings.Join(missing, ", ")),
			FixHint:  "Remove stale roots with `buddy-ctl projects remove-root <path>`",
		}
	}

	if len(projects) == 0 {
		return CheckResult{
			Category: "Project Roots",
			Name:     "projects_roots",
			Status:   StatusWarn,
			Message:  fmt.Sprintf("%d root(s) exist but 0 project directories found", validRoots),
		}
	}

	return CheckResult{
		Category: "Project Roots",
		Name:     "projects_roots",
		Status:   StatusPass,
		Message:  fmt.Sprintf("%d project(s) across %d root(s)", len(projects), validRoots),
	}
}

func checkEngines(cfg *config.Config, probe *setup.ProbeResult) []CheckResult {
	defEngine := cfg.DefaultEngine
	if defEngine == "" {
		defEngine = "rex"
	}

	type engEntry struct {
		name      string
		available bool
		detail    string
	}
	ocDetail := probe.OpenCodePath
	if probe.OpenCodeDesktopApp && ocDetail == "" {
		ocDetail = "/Applications/OpenCode.app"
	}
	entries := []engEntry{
		{"rex", probe.HasRex, probe.RexPath},
		{"opencode", probe.HasOpenCode, ocDetail},
		{"t3", probe.HasT3, probe.T3Path},
		{"wezterm", probe.HasWezTerm, probe.WezTermPath},
	}

	var out []CheckResult
	for _, e := range entries {
		isDefault := e.name == defEngine
		label := e.name
		if isDefault {
			label += " (default)"
		}
		if e.available {
			msg := "available"
			if e.detail != "" {
				msg = setup.FormatHomePath(e.detail, probe.HomeDir)
			}
			out = append(out, CheckResult{
				Category: "Engines",
				Name:     label,
				Status:   StatusPass,
				Message:  msg,
			})
		} else if isDefault {
			out = append(out, CheckResult{
				Category: "Engines",
				Name:     label,
				Status:   StatusFail,
				Message:  "default engine binary not found",
				FixHint:  fmt.Sprintf("Install %s or switch default via `buddy-ctl setup --only engines`", e.name),
			})
		} else {
			out = append(out, CheckResult{
				Category: "Engines",
				Name:     label,
				Status:   StatusInfo,
				Message:  "not installed",
			})
		}
	}
	return out
}

func checkHarnesses(cfg *config.Config, probe *setup.ProbeResult, lookPath func(string) (string, error)) []CheckResult {
	defEngine := cfg.DefaultEngine
	if defEngine == "" {
		defEngine = "rex"
	}
	if config.IsNonTerminalEngine(defEngine) && !probe.HasRex && !probe.HasWezTerm {
		return nil
	}

	checkRole := func(role, agentName string) CheckResult {
		cmd, _, _ := cfg.GetAgentCommand(agentName)
		bin := agentName
		if len(cmd) > 0 && cmd[0] != "" {
			bin = cmd[0]
		}
		if p, err := lookPath(bin); err == nil {
			return CheckResult{
				Category: "Agent Harnesses",
				Name:     role + " (" + agentName + ")",
				Status:   StatusPass,
				Message:  setup.FormatHomePath(p, probe.HomeDir),
			}
		}
		status := StatusFail
		if config.IsNonTerminalEngine(defEngine) {
			status = StatusWarn
		}
		return CheckResult{
			Category: "Agent Harnesses",
			Name:     role + " (" + agentName + ")",
			Status:   status,
			Message:  fmt.Sprintf("binary '%s' not found on PATH", bin),
			FixHint:  "Install the harness or run `buddy-ctl setup --only agents`",
		}
	}

	orch := cfg.GetEffectiveOrchestratorAgent("rex")
	worker := cfg.GetEffectiveWorkerAgent("rex")
	return []CheckResult{
		checkRole("orchestrator", orch),
		checkRole("worker", worker),
	}
}
