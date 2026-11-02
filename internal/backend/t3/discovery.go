package t3

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ServerDiscovery handles locating the running T3 Code server runtime state.
type ServerDiscovery struct {
	customStatePath string
	customHomeDir   string
}

// DiscoveryOption configures ServerDiscovery.
type DiscoveryOption func(*ServerDiscovery)

// WithStatePath sets an explicit path to the server-runtime.json file.
func WithStatePath(path string) DiscoveryOption {
	return func(d *ServerDiscovery) {
		d.customStatePath = path
	}
}

// WithHomeDir overrides the home directory for discovery (useful in tests).
func WithHomeDir(dir string) DiscoveryOption {
	return func(d *ServerDiscovery) {
		d.customHomeDir = dir
	}
}

// NewServerDiscovery creates a new discovery helper.
func NewServerDiscovery(opts ...DiscoveryOption) *ServerDiscovery {
	d := &ServerDiscovery{}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DiscoverRuntime attempts to read and parse server-runtime.json.
// It checks the primary path ~/.t3/server-runtime.json and the state-dir
// equivalent ~/.t3/userdata/server-runtime.json.
// If neither exists, it returns a graceful ErrServerNotRunning error.
func (d *ServerDiscovery) DiscoverRuntime() (*ServerRuntime, error) {
	candidates := d.candidatePaths()

	var lastErr error
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}

		var runtime ServerRuntime
		if err := json.Unmarshal(data, &runtime); err != nil {
			return nil, fmt.Errorf("failed to parse t3 server-runtime.json at %s: %w", path, err)
		}

		// Ensure Origin is populated
		if runtime.Origin == "" && runtime.Port > 0 {
			host := runtime.Host
			if host == "" {
				host = "127.0.0.1"
			}
			runtime.Origin = fmt.Sprintf("http://%s:%d", host, runtime.Port)
		}

		return &runtime, nil
	}

	if lastErr != nil && !os.IsNotExist(lastErr) {
		return nil, fmt.Errorf("%w: runtime state file not found (checked: %s, last error: %v). Start the T3 Code desktop app or run 't3'",
			ErrServerNotRunning, strings.Join(candidates, ", "), lastErr)
	}
	return nil, fmt.Errorf("%w: runtime state file not found (checked: %s). Start the T3 Code desktop app or run 't3'",
		ErrServerNotRunning, strings.Join(candidates, ", "))
}

func (d *ServerDiscovery) candidatePaths() []string {
	if d.customStatePath != "" {
		return []string{d.customStatePath}
	}

	home := d.customHomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		home = "."
	}

	return []string{
		filepath.Join(home, ".t3", "server-runtime.json"),
		filepath.Join(home, ".t3", "userdata", "server-runtime.json"),
	}
}

// LookPathFunc defines the function signature for resolving a binary on PATH.
type LookPathFunc func(file string) (string, error)

// FindT3Binary locates the 't3' binary on the host machine.
// It checks in order:
// 1. configuredBin (if non-empty)
// 2. BUDDY_T3_BIN or T3_BIN environment variables
// 3. System PATH via exec.LookPath
// 4. ~/.local/bin/t3 (required for non-login subshells where ~/.local/bin is omitted from PATH)
// 5. /usr/local/bin/t3
func FindT3Binary(configuredBin string, customHome ...string) (string, error) {
	return FindT3BinaryWithLookup(configuredBin, exec.LookPath, customHome...)
}

// FindT3BinaryWithLookup resolves the t3 binary using a custom LookPath function,
// enabling isolated testing without host PATH dependencies.
func FindT3BinaryWithLookup(configuredBin string, lookPath LookPathFunc, customHome ...string) (string, error) {
	if configuredBin != "" {
		if fi, err := os.Stat(configuredBin); err == nil && isExecutable(fi) {
			return configuredBin, nil
		}
		// If lookpath resolves configuredBin
		if lookPath != nil {
			if lp, err := lookPath(configuredBin); err == nil {
				return lp, nil
			}
		}
	}

	for _, envKey := range []string{"BUDDY_T3_BIN", "T3_BIN"} {
		if envVal := os.Getenv(envKey); envVal != "" {
			if fi, err := os.Stat(envVal); err == nil && isExecutable(fi) {
				return envVal, nil
			}
		}
	}

	home := ""
	if len(customHome) > 0 && customHome[0] != "" {
		home = customHome[0]
	} else {
		home, _ = os.UserHomeDir()
	}

	localBin := filepath.Join(home, ".local", "bin", "t3")

	if lookPath != nil {
		if lp, err := lookPath("t3"); err == nil {
			return lp, nil
		}
	}

	if fi, err := os.Stat(localBin); err == nil && isExecutable(fi) {
		return localBin, nil
	}

	usrLocalBin := "/usr/local/bin/t3"
	if fi, err := os.Stat(usrLocalBin); err == nil && isExecutable(fi) {
		return usrLocalBin, nil
	}

	return "", fmt.Errorf("t3 binary not found (checked PATH and %s)", localBin)
}

func isExecutable(fi os.FileInfo) bool {
	return fi.Mode().Perm()&0111 != 0
}
