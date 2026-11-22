package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	buddy "github.com/tortoise10h/buddy"
)

const ManagedManifestName = ".managed.json"

// ManagedFile records the checksum and source template of a Buddy-managed file.
type ManagedFile struct {
	SHA256   string `json:"sha256"`
	Template string `json:"template"`
}

// ManagedManifest is persisted at ~/.buddy/.managed.json to enable safe, idempotent upgrades.
type ManagedManifest struct {
	Version string                 `json:"version"`
	Mode    string                 `json:"mode"` // "embedded" or "dev-link"
	DevRepo string                 `json:"dev_repo,omitempty"`
	Files   map[string]ManagedFile `json:"files"`
}

// ProvisionAction reports the outcome for a single provisioned file or symlink.
type ProvisionAction struct {
	Path   string `json:"path"`
	Status string `json:"status"` // created, updated, unchanged, preserved, backed_up, linked, would_create, would_update, would_link, skipped
	Detail string `json:"detail,omitempty"`
}

// ProvisionReport summarizes all asset provisioning operations.
type ProvisionReport struct {
	Mode    string            `json:"mode"`
	DryRun  bool              `json:"dry_run,omitempty"`
	Actions []ProvisionAction `json:"actions"`
}

// ProvisionOptions controls ~/.buddy/ asset and skill provisioning.
type ProvisionOptions struct {
	BuddyDir        string
	HomeDir         string
	Version         string
	BuddyCtlPath    string
	DevRepo         string
	SkillTargetDirs []string
	SkipSkills      bool
	SkipHooks       bool
	Force           bool
	DryRun          bool
	Now             func() time.Time
}

// LoadManagedManifest reads ~/.buddy/.managed.json if present.
func LoadManagedManifest(buddyDir string) (*ManagedManifest, error) {
	path := filepath.Join(buddyDir, ManagedManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ManagedManifest{Files: make(map[string]ManagedFile)}, nil
		}
		return nil, err
	}
	var m ManagedManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.Files == nil {
		m.Files = make(map[string]ManagedFile)
	}
	return &m, nil
}

// SaveManagedManifest atomically writes ~/.buddy/.managed.json.
func SaveManagedManifest(buddyDir string, m *ManagedManifest) error {
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := filepath.Join(buddyDir, ".managed.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(buddyDir, ManagedManifestName))
}

// RenderHooksJSON substitutes __BUDDY_CTL__ in the embedded hooks template.
func RenderHooksJSON(buddyCtlPath string) []byte {
	if strings.TrimSpace(buddyCtlPath) == "" {
		buddyCtlPath = "buddy-ctl"
	}
	return bytes.ReplaceAll(buddy.HooksJSONTemplate, []byte("__BUDDY_CTL__"), []byte(buddyCtlPath))
}

// ProvisionAssets materializes or symlinks ~/.buddy/AGENTS.md, ~/.buddy/.agents/hooks.json,
// and selected agent skill directories while preserving user customizations.
func ProvisionAssets(opts ProvisionOptions) (*ProvisionReport, error) {
	if opts.BuddyDir == "" {
		return nil, fmt.Errorf("BuddyDir is required")
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	mode := "embedded"
	var devRepo string
	if strings.TrimSpace(opts.DevRepo) != "" {
		mode = "dev-link"
		devRepo = ExpandPath(opts.DevRepo, opts.HomeDir)
	}

	manifest, err := LoadManagedManifest(opts.BuddyDir)
	if err != nil {
		manifest = &ManagedManifest{Files: make(map[string]ManagedFile)}
	}
	manifest.Version = opts.Version
	manifest.Mode = mode
	manifest.DevRepo = devRepo

	report := &ProvisionReport{
		Mode:   mode,
		DryRun: opts.DryRun,
	}

	if !opts.DryRun {
		if err := os.MkdirAll(filepath.Join(opts.BuddyDir, ".agents"), 0755); err != nil {
			return nil, fmt.Errorf("failed to create %s/.agents: %w", opts.BuddyDir, err)
		}
	}

	// 1. ~/.buddy/AGENTS.md
	agentsDest := filepath.Join(opts.BuddyDir, "AGENTS.md")
	if mode == "dev-link" {
		src := filepath.Join(devRepo, "templates", "orchestrator-AGENTS.md")
		act, err := ensureSymlink(agentsDest, src, opts.Force, opts.DryRun, nowFn)
		if err != nil {
			return nil, err
		}
		report.Actions = append(report.Actions, act)
	} else {
		act, err := ensureManagedFile(agentsDest, "templates/orchestrator-AGENTS.md", buddy.OrchestratorAgentsMD, 0644, manifest, opts.Force, opts.DryRun, nowFn)
		if err != nil {
			return nil, err
		}
		report.Actions = append(report.Actions, act)
	}

	// 2. ~/.buddy/.agents/hooks.json (always rendered, never symlinked)
	hooksDest := filepath.Join(opts.BuddyDir, ".agents", "hooks.json")
	if opts.SkipHooks {
		report.Actions = append(report.Actions, ProvisionAction{
			Path:   hooksDest,
			Status: "skipped",
			Detail: "--skip-hooks",
		})
	} else {
		rendered := RenderHooksJSON(opts.BuddyCtlPath)
		act, err := ensureManagedFile(hooksDest, "templates/hooks.json", rendered, 0644, manifest, opts.Force, opts.DryRun, nowFn)
		if err != nil {
			return nil, err
		}
		report.Actions = append(report.Actions, act)
	}

	// 3. Agent skills (SKILL.md or symlinked skills/buddy directory)
	if !opts.SkipSkills {
		for _, targetDir := range opts.SkillTargetDirs {
			targetDir = strings.TrimSpace(targetDir)
			if targetDir == "" {
				continue
			}
			if mode == "dev-link" {
				srcDir := filepath.Join(devRepo, "skills", "buddy")
				act, err := ensureSymlink(targetDir, srcDir, opts.Force, opts.DryRun, nowFn)
				if err != nil {
					return nil, err
				}
				report.Actions = append(report.Actions, act)
			} else {
				// If targetDir is currently a symlink (e.g. from legacy install.sh), replace with a real dir
				if fi, lerr := os.Lstat(targetDir); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
					if !opts.DryRun {
						if err := os.Remove(targetDir); err != nil {
							return nil, fmt.Errorf("failed to remove legacy skill symlink %s: %w", targetDir, err)
						}
					}
				}
				skillDest := filepath.Join(targetDir, "SKILL.md")
				act, err := ensureManagedFile(skillDest, "skills/buddy/SKILL.md", buddy.SkillMD, 0644, manifest, opts.Force, opts.DryRun, nowFn)
				if err != nil {
					return nil, err
				}
				report.Actions = append(report.Actions, act)
			}
		}
	}

	if !opts.DryRun {
		if err := SaveManagedManifest(opts.BuddyDir, manifest); err != nil {
			return nil, fmt.Errorf("failed to save managed manifest: %w", err)
		}
	}

	return report, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ensureManagedFile(dest, templateName string, content []byte, perm os.FileMode, manifest *ManagedManifest, force, dryRun bool, nowFn func() time.Time) (ProvisionAction, error) {
	desiredHash := sha256Hex(content)

	lfi, lerr := os.Lstat(dest)
	if os.IsNotExist(lerr) {
		if dryRun {
			return ProvisionAction{Path: dest, Status: "would_create"}, nil
		}
		if err := writeFileAtomic(dest, content, perm); err != nil {
			return ProvisionAction{}, err
		}
		manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
		return ProvisionAction{Path: dest, Status: "created"}, nil
	}
	if lerr != nil {
		return ProvisionAction{}, lerr
	}

	// Existing symlink (from legacy install.sh or broken symlink): replace cleanly if it's a buddy symlink or broken
	if lfi.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(dest)
		_, statErr := os.Stat(dest)
		isBroken := statErr != nil
		isLegacyBuddyLink := strings.HasSuffix(target, "orchestrator-AGENTS.md") ||
			strings.HasSuffix(target, "/AGENTS.md") ||
			strings.HasSuffix(target, ".agents/hooks.json") ||
			strings.HasSuffix(target, "skills/buddy")

		if !isBroken && !isLegacyBuddyLink && !force {
			return ProvisionAction{
				Path:   dest,
				Status: "preserved",
				Detail: fmt.Sprintf("custom symlink -> %s (use --force to replace)", target),
			}, nil
		}
		if dryRun {
			return ProvisionAction{Path: dest, Status: "would_update", Detail: "replace symlink with managed file"}, nil
		}
		if !isBroken && !isLegacyBuddyLink && force {
			backupPath := backupFile(dest, nowFn)
			if err := writeFileAtomic(dest, content, perm); err != nil {
				return ProvisionAction{}, err
			}
			manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
			return ProvisionAction{Path: dest, Status: "backed_up", Detail: "backup: " + backupPath}, nil
		}
		_ = os.Remove(dest)
		if err := writeFileAtomic(dest, content, perm); err != nil {
			return ProvisionAction{}, err
		}
		manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
		return ProvisionAction{Path: dest, Status: "updated", Detail: "migrated from symlink"}, nil
	}

	// Regular file exists: compare checksum
	existing, err := os.ReadFile(dest)
	if err != nil {
		return ProvisionAction{}, err
	}
	existingHash := sha256Hex(existing)
	if existingHash == desiredHash {
		manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
		return ProvisionAction{Path: dest, Status: "unchanged"}, nil
	}

	// Check whether file matches last recorded managed hash
	recorded, isTracked := manifest.Files[dest]
	if isTracked && recorded.SHA256 == existingHash {
		if dryRun {
			return ProvisionAction{Path: dest, Status: "would_update"}, nil
		}
		if err := writeFileAtomic(dest, content, perm); err != nil {
			return ProvisionAction{}, err
		}
		manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
		return ProvisionAction{Path: dest, Status: "updated"}, nil
	}

	// User-modified (or untracked) file
	if !force {
		return ProvisionAction{
			Path:   dest,
			Status: "preserved",
			Detail: "customized locally (use --force to overwrite with backup)",
		}, nil
	}

	if dryRun {
		return ProvisionAction{Path: dest, Status: "would_update", Detail: "would back up and overwrite (--force)"}, nil
	}
	backupPath := backupFile(dest, nowFn)
	if err := writeFileAtomic(dest, content, perm); err != nil {
		return ProvisionAction{}, err
	}
	manifest.Files[dest] = ManagedFile{SHA256: desiredHash, Template: templateName}
	return ProvisionAction{Path: dest, Status: "backed_up", Detail: "backup: " + backupPath}, nil
}

func ensureSymlink(dest, src string, force, dryRun bool, nowFn func() time.Time) (ProvisionAction, error) {
	if _, err := os.Stat(src); err != nil {
		return ProvisionAction{}, fmt.Errorf("dev-link source not found: %s", src)
	}

	lfi, lerr := os.Lstat(dest)
	if lerr == nil {
		if lfi.Mode()&os.ModeSymlink != 0 {
			curTarget, _ := os.Readlink(dest)
			if filepath.Clean(curTarget) == filepath.Clean(src) {
				return ProvisionAction{Path: dest, Status: "unchanged", Detail: "-> " + src}, nil
			}
		} else if !force {
			// Regular file or directory exists; back up if force, else preserve
			if dryRun {
				return ProvisionAction{Path: dest, Status: "preserved", Detail: "existing file/dir (use --force to replace with dev-link)"}, nil
			}
			// If it's a managed skill directory containing only SKILL.md, safe to replace
			if !isManagedSkillDir(dest) {
				return ProvisionAction{Path: dest, Status: "preserved", Detail: "existing file/dir (use --force to replace with dev-link)"}, nil
			}
		}
		if dryRun {
			return ProvisionAction{Path: dest, Status: "would_link", Detail: "-> " + src}, nil
		}
		if lfi.Mode()&os.ModeSymlink == 0 && force && !isManagedSkillDir(dest) {
			backupPath := backupFile(dest, nowFn)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return ProvisionAction{}, err
			}
			if err := os.Symlink(src, dest); err != nil {
				return ProvisionAction{}, err
			}
			return ProvisionAction{Path: dest, Status: "linked", Detail: fmt.Sprintf("-> %s (backup: %s)", src, backupPath)}, nil
		}
		if err := os.RemoveAll(dest); err != nil {
			return ProvisionAction{}, err
		}
	} else if !os.IsNotExist(lerr) {
		return ProvisionAction{}, lerr
	}

	if dryRun {
		return ProvisionAction{Path: dest, Status: "would_link", Detail: "-> " + src}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return ProvisionAction{}, err
	}
	if err := os.Symlink(src, dest); err != nil {
		return ProvisionAction{}, err
	}
	return ProvisionAction{Path: dest, Status: "linked", Detail: "-> " + src}, nil
}

func isManagedSkillDir(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) == 1 && entries[0].Name() == "SKILL.md"
}

func backupFile(path string, nowFn func() time.Time) string {
	backup := fmt.Sprintf("%s.bak-%s", path, nowFn().Format("20060102150405"))
	_ = os.Rename(path, backup)
	return backup
}

func writeFileAtomic(dest string, content []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, content, perm); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
