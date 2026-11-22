package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend/opencode"
	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/host"
	"github.com/tortoise10h/buddy/internal/state"
)

// HarnessCandidates is the canonical ordered list of known terminal agent harnesses.
var HarnessCandidates = []string{"agy", "opencode", "pi", "claude", "codex", "cursor", "gemini"}

// CandidateRoot describes a discovered or configured project root directory.
type CandidateRoot struct {
	Path        string `json:"path"`
	DisplayPath string `json:"display_path"`
	RepoCount   int    `json:"repo_count"`
	Exists      bool   `json:"exists"`
	Configured  bool   `json:"configured"`
}

// SkillTarget describes an agent skill directory where skills/buddy/SKILL.md can be installed.
type SkillTarget struct {
	Name          string `json:"name"`
	Dir           string `json:"dir"`
	DisplayDir    string `json:"display_dir"`
	Detected      bool   `json:"detected"`
	AlwaysDefault bool   `json:"always_default"`
}

// ProbeOptions allows overriding OS lookups in tests.
type ProbeOptions struct {
	HomeDir        string
	BuddyDir       string
	Getenv         func(string) string
	LookPath       func(string) (string, error)
	ExistingConfig *config.Config
	ConfigExists   *bool
}

// ProbeResult captures the detected local machine capabilities for setup & doctor.
type ProbeResult struct {
	HomeDir      string `json:"home_dir"`
	BuddyDir     string `json:"buddy_dir"`
	ConfigExists bool   `json:"config_exists"`

	BuddyCtlPath   string   `json:"buddy_ctl_path,omitempty"`
	BuddyCtlInPATH bool     `json:"buddy_ctl_in_path"`
	BuddyBinPath   string   `json:"buddy_bin_path,omitempty"`
	BuddyBinInPATH bool     `json:"buddy_bin_in_path"`
	LocalBinInPATH bool     `json:"local_bin_in_path"`
	PATHDirs       []string `json:"-"`

	DetectedHost string `json:"detected_host"`

	HasRex             bool   `json:"has_rex"`
	RexPath            string `json:"rex_path,omitempty"`
	HasWezTerm         bool   `json:"has_wezterm"`
	WezTermPath        string `json:"wezterm_path,omitempty"`
	HasT3              bool   `json:"has_t3"`
	T3Path             string `json:"t3_path,omitempty"`
	HasOpenCode        bool   `json:"has_opencode"`
	OpenCodePath       string `json:"opencode_path,omitempty"`
	OpenCodeDesktopApp bool   `json:"opencode_desktop_app"`

	InstalledHarnesses []string          `json:"installed_harnesses"`
	HarnessPaths       map[string]string `json:"harness_paths,omitempty"`

	CandidateRoots []CandidateRoot `json:"candidate_roots"`
	SkillTargets   []SkillTarget   `json:"skill_targets"`
}

// Probe inspects the local environment for binaries, engines, harnesses, roots, and skill targets.
func Probe(_ context.Context, opts ProbeOptions) *ProbeResult {
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

	res := &ProbeResult{
		HomeDir:      homeDir,
		BuddyDir:     buddyDir,
		HarnessPaths: make(map[string]string),
	}

	if opts.ConfigExists != nil {
		res.ConfigExists = *opts.ConfigExists
	} else {
		_, err := os.Stat(filepath.Join(buddyDir, "config.json"))
		res.ConfigExists = err == nil
	}

	// PATH inspection
	pathEnv := getenv("PATH")
	res.PATHDirs = filepath.SplitList(pathEnv)
	localBin := filepath.Join(homeDir, ".local", "bin")
	for _, d := range res.PATHDirs {
		if ExpandPath(d, homeDir) == filepath.Clean(localBin) {
			res.LocalBinInPATH = true
			break
		}
	}

	if p, err := lookPath("buddy-ctl"); err == nil {
		res.BuddyCtlPath = p
		res.BuddyCtlInPATH = true
	} else if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			res.BuddyCtlPath = resolved
		} else {
			res.BuddyCtlPath = exe
		}
	}

	if p, err := lookPath("buddy"); err == nil {
		res.BuddyBinPath = p
		res.BuddyBinInPATH = true
	} else {
		candidate := filepath.Join(localBin, "buddy")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			res.BuddyBinPath = candidate
		}
	}

	// Host detection
	hostInfo := host.DetectHostFromEnv(getenv)
	res.DetectedHost = string(hostInfo.Kind)

	// Engines
	if p, err := lookPath("rex"); err == nil {
		res.HasRex = true
		res.RexPath = p
	}
	if p, err := lookPath("wezterm"); err == nil {
		res.HasWezTerm = true
		res.WezTermPath = p
	}
	if p, err := lookPath("t3"); err == nil {
		res.HasT3 = true
		res.T3Path = p
	} else if opts.LookPath == nil {
		if p, err := t3.FindT3Binary(); err == nil {
			res.HasT3 = true
			res.T3Path = p
		}
	}
	if p, err := lookPath("opencode"); err == nil {
		res.HasOpenCode = true
		res.OpenCodePath = p
	} else if opts.LookPath == nil {
		if p, err := opencode.FindOpenCodeBinary(); err == nil {
			res.HasOpenCode = true
			res.OpenCodePath = p
		}
	}
	if fi, err := os.Stat("/Applications/OpenCode.app"); err == nil && fi.IsDir() {
		res.OpenCodeDesktopApp = true
		res.HasOpenCode = true
	}

	// Agent harnesses
	for _, h := range HarnessCandidates {
		if p, err := lookPath(h); err == nil {
			res.InstalledHarnesses = append(res.InstalledHarnesses, h)
			res.HarnessPaths[h] = p
		}
	}

	// Candidate project roots
	var configuredRoots []string
	if opts.ExistingConfig != nil {
		configuredRoots = opts.ExistingConfig.ProjectsRoots
	}
	res.CandidateRoots = discoverCandidateRoots(homeDir, configuredRoots)

	// Skill targets
	res.SkillTargets = discoverSkillTargets(homeDir, res.HarnessPaths)

	return res
}

// RecommendedEngine returns the best default engine based on existing config, host, and installed binaries.
func (p *ProbeResult) RecommendedEngine(existing string) string {
	if existing != "" {
		return existing
	}
	switch p.DetectedHost {
	case "rex", "t3", "opencode":
		return p.DetectedHost
	}
	if p.HasRex {
		return "rex"
	}
	if p.HasOpenCode {
		return "opencode"
	}
	if p.HasT3 {
		return "t3"
	}
	if p.HasWezTerm {
		return "wezterm"
	}
	return "rex"
}

// RecommendedHarness returns the best default harness given an existing value.
func (p *ProbeResult) RecommendedHarness(existing string) string {
	if existing != "" {
		return existing
	}
	for _, h := range p.InstalledHarnesses {
		if h == "agy" {
			return "agy"
		}
	}
	if len(p.InstalledHarnesses) > 0 {
		return p.InstalledHarnesses[0]
	}
	return "agy"
}

func discoverCandidateRoots(homeDir string, configured []string) []CandidateRoot {
	configuredSet := make(map[string]bool)
	var ordered []string
	seen := make(map[string]bool)

	addCandidate := func(raw string, isConfigured bool) {
		abs := ExpandPath(raw, homeDir)
		if abs == "" {
			return
		}
		if isConfigured {
			configuredSet[abs] = true
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		ordered = append(ordered, abs)
	}

	for _, r := range configured {
		addCandidate(r, true)
	}

	commonRelative := []string{
		filepath.Join("Dev", "Personal", "Projects"),
		"Dev",
		"Projects",
		"Code",
		"Workspace",
		"src",
		"repos",
	}
	if homeDir != "" {
		for _, rel := range commonRelative {
			candidate := filepath.Join(homeDir, rel)
			fi, err := os.Stat(candidate)
			if err != nil || !fi.IsDir() {
				continue
			}
			// Only suggest common directories that actually have at least 1 project subdirectory,
			// unless already configured.
			if countSubdirs(candidate) > 0 || configuredSet[filepath.Clean(candidate)] {
				addCandidate(candidate, false)
			}
		}
	}

	out := make([]CandidateRoot, 0, len(ordered))
	for _, abs := range ordered {
		fi, err := os.Stat(abs)
		exists := err == nil && fi.IsDir()
		count := 0
		if exists {
			count = countSubdirs(abs)
		}
		out = append(out, CandidateRoot{
			Path:        abs,
			DisplayPath: FormatHomePath(abs, homeDir),
			RepoCount:   count,
			Exists:      exists,
			Configured:  configuredSet[abs],
		})
	}
	return out
}

func countSubdirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			count++
		}
	}
	return count
}

func discoverSkillTargets(homeDir string, harnessPaths map[string]string) []SkillTarget {
	if homeDir == "" {
		return nil
	}

	dirExists := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.IsDir()
	}

	_, hasAgy := harnessPaths["agy"]
	_, hasPi := harnessPaths["pi"]
	_, hasOpenCode := harnessPaths["opencode"]

	targets := []SkillTarget{
		{
			Name:          "Universal (~/.agents)",
			Dir:           filepath.Join(homeDir, ".agents", "skills", "buddy"),
			Detected:      true,
			AlwaysDefault: true,
		},
		{
			Name:     "Antigravity CLI",
			Dir:      filepath.Join(homeDir, ".gemini", "antigravity-cli", "skills", "buddy"),
			Detected: hasAgy || dirExists(filepath.Join(homeDir, ".gemini", "antigravity-cli")),
		},
		{
			Name:     "Gemini CLI",
			Dir:      filepath.Join(homeDir, ".gemini", "config", "skills", "buddy"),
			Detected: dirExists(filepath.Join(homeDir, ".gemini", "config")),
		},
		{
			Name:     "Pi Agent",
			Dir:      filepath.Join(homeDir, ".pi", "agent", "skills", "buddy"),
			Detected: hasPi || dirExists(filepath.Join(homeDir, ".pi", "agent")),
		},
		{
			Name:     "OpenCode",
			Dir:      filepath.Join(homeDir, ".config", "opencode", "skills", "buddy"),
			Detected: hasOpenCode || dirExists(filepath.Join(homeDir, ".config", "opencode")),
		},
	}

	for i := range targets {
		targets[i].DisplayDir = FormatHomePath(targets[i].Dir, homeDir)
	}

	return targets
}

// DefaultSelectedSkillTargetDirs returns the directories that should be pre-selected by default.
func (p *ProbeResult) DefaultSelectedSkillTargetDirs() []string {
	var dirs []string
	for _, t := range p.SkillTargets {
		if t.AlwaysDefault || t.Detected {
			dirs = append(dirs, t.Dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}
