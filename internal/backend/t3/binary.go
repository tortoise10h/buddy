package t3

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrBinaryNotFound is returned when the t3 CLI binary cannot be located.
var ErrBinaryNotFound = errors.New("t3 binary not found")

// usrLocalBinT3 is the candidate path for /usr/local/bin/t3, variable for testing.
var usrLocalBinT3 = "/usr/local/bin/t3"

// FindT3Binary locates the t3 executable binary following this precedence:
// 1. BUDDY_T3_BIN environment variable.
// 2. T3CODE_CLI_PATH environment variable (if executable).
// 3. PATH resolution via exec.LookPath.
// 4. ~/.local/bin/t3.
// 5. /usr/local/bin/t3.
func FindT3Binary() (string, error) {
	// 1. BUDDY_T3_BIN environment variable override
	if envBin := os.Getenv("BUDDY_T3_BIN"); envBin != "" {
		return envBin, nil
	}

	// 2. T3CODE_CLI_PATH environment variable
	if cliPath := os.Getenv("T3CODE_CLI_PATH"); cliPath != "" {
		if isExecutable(cliPath) {
			return cliPath, nil
		}
	}

	// 3. PATH lookup
	if path, err := exec.LookPath("t3"); err == nil {
		return path, nil
	}

	// 4. ~/.local/bin/t3
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		localBin := filepath.Join(home, ".local", "bin", "t3")
		if isExecutable(localBin) {
			return localBin, nil
		}
	}

	// 5. /usr/local/bin/t3
	if isExecutable(usrLocalBinT3) {
		return usrLocalBinT3, nil
	}

	return "", fmt.Errorf("%w: checked BUDDY_T3_BIN, T3CODE_CLI_PATH, PATH, ~/.local/bin/t3, %s", ErrBinaryNotFound, usrLocalBinT3)
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.IsDir() && (fi.Mode().Perm()&0111 != 0)
}
