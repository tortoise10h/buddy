package opencode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Candidate system paths (variables for testability).
var (
	homebrewBinOpenCode = "/opt/homebrew/bin/opencode"
	usrLocalBinOpenCode = "/usr/local/bin/opencode"
)

// FindOpenCodeBinary locates the opencode executable binary following this precedence:
// 1. BUDDY_OPENCODE_BIN environment variable.
// 2. PATH resolution via exec.LookPath("opencode").
// 3. ~/.local/bin/opencode
// 4. ~/Library/pnpm/bin/opencode
// 5. /opt/homebrew/bin/opencode and /usr/local/bin/opencode
// 6. OpenCode Desktop bundled CLI: ~/Library/Application Support/ai.opencode.desktop/cli/*/opencode-cli
func FindOpenCodeBinary() (string, error) {
	if envBin := os.Getenv("BUDDY_OPENCODE_BIN"); envBin != "" {
		return envBin, nil
	}

	if path, err := exec.LookPath("opencode"); err == nil {
		return path, nil
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		candidates := []string{
			filepath.Join(home, ".local", "bin", "opencode"),
			filepath.Join(home, "Library", "pnpm", "bin", "opencode"),
		}
		for _, c := range candidates {
			if isExecutable(c) {
				return c, nil
			}
		}
	}

	for _, c := range []string{homebrewBinOpenCode, usrLocalBinOpenCode} {
		if isExecutable(c) {
			return c, nil
		}
	}

	if home != "" {
		if bundled := findDesktopBundledCLI(home); bundled != "" {
			return bundled, nil
		}
	}

	return "", fmt.Errorf("%w: checked BUDDY_OPENCODE_BIN, PATH, ~/.local/bin/opencode, ~/Library/pnpm/bin/opencode, %s, %s, and OpenCode Desktop bundled CLI", ErrBinaryNotFound, homebrewBinOpenCode, usrLocalBinOpenCode)
}

func findDesktopBundledCLI(home string) string {
	pattern := filepath.Join(home, "Library", "Application Support", "ai.opencode.desktop", "cli", "*", "opencode-cli")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	for i := len(matches) - 1; i >= 0; i-- {
		if isExecutable(matches[i]) {
			return matches[i]
		}
	}
	return ""
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.IsDir() && (fi.Mode().Perm()&0111 != 0)
}
