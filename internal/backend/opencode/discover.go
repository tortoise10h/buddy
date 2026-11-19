package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const defaultUsername = "opencode"

// ServiceState mirrors ~/.local/state/opencode/service.json written by `opencode serve --service`.
type ServiceState struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	PID      int    `json:"pid"`
	Password string `json:"password"`
}

// EndpointConfig holds the resolved connection URL and Basic Auth credentials.
type EndpointConfig struct {
	URL      string
	Username string
	Password string
}

// DiscoverOptions configures how DiscoverServer locates or starts the shared local OpenCode server.
type DiscoverOptions struct {
	ServerURL        string
	Username         string
	Password         string
	AutoStartService bool
	ServiceJSONPath  string
	StartTimeout     time.Duration
	PollInterval     time.Duration

	// Injectable hooks for tests.
	GetEnv         func(string) string
	IsPIDAlive     func(int) bool
	StartServiceFn func(ctx context.Context) error
}

// DefaultServiceJSONPath returns ~/.local/state/opencode/service.json.
func DefaultServiceJSONPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "service.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".local", "state", "opencode", "service.json")
	}
	return filepath.Join(home, ".local", "state", "opencode", "service.json")
}

// DiscoverServer resolves the OpenCode local server URL and auth credentials:
// 1. Environment overrides: BUDDY_OPENCODE_URL / OPENCODE_SERVER_URL (+ password env vars)
// 2. Explicit config ServerURL
// 3. Shared background service state file (~/.local/state/opencode/service.json)
// 4. Optional auto-bootstrap via `opencode service start` if AutoStartService is enabled
func DiscoverServer(ctx context.Context, opts DiscoverOptions) (EndpointConfig, error) {
	getEnv := opts.GetEnv
	if getEnv == nil {
		getEnv = os.Getenv
	}
	isAlive := opts.IsPIDAlive
	if isAlive == nil {
		isAlive = defaultIsPIDAlive
	}
	username := strings.TrimSpace(opts.Username)
	if username == "" {
		username = strings.TrimSpace(getEnv("OPENCODE_SERVER_USERNAME"))
	}
	if username == "" {
		username = defaultUsername
	}
	envPassword := strings.TrimSpace(getEnv("BUDDY_OPENCODE_PASSWORD"))
	if envPassword == "" {
		envPassword = strings.TrimSpace(getEnv("OPENCODE_SERVER_PASSWORD"))
	}

	// 1. Explicit environment override
	envURL := strings.TrimSpace(getEnv("BUDDY_OPENCODE_URL"))
	if envURL == "" {
		envURL = strings.TrimSpace(getEnv("OPENCODE_SERVER_URL"))
	}
	if envURL != "" {
		pw := envPassword
		if pw == "" {
			pw = opts.Password
		}
		return EndpointConfig{
			URL:      strings.TrimRight(envURL, "/"),
			Username: username,
			Password: pw,
		}, nil
	}

	// 2. Explicit config override
	if cfgURL := strings.TrimSpace(opts.ServerURL); cfgURL != "" {
		pw := opts.Password
		if pw == "" {
			pw = envPassword
		}
		return EndpointConfig{
			URL:      strings.TrimRight(cfgURL, "/"),
			Username: username,
			Password: pw,
		}, nil
	}

	// 3. Shared local background service (~/.local/state/opencode/service.json)
	servicePath := opts.ServiceJSONPath
	if servicePath == "" {
		servicePath = DefaultServiceJSONPath()
	}
	if ep, ok := readLiveServiceState(servicePath, username, isAlive); ok {
		return ep, nil
	}

	// 4. Auto-start shared background service if allowed
	if opts.AutoStartService {
		startFn := opts.StartServiceFn
		if startFn == nil {
			startFn = defaultStartService
		}
		if err := startFn(ctx); err != nil {
			return EndpointConfig{}, fmt.Errorf("%w: failed to start background service: %v", ErrServiceNotRunning, err)
		}

		timeout := opts.StartTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		poll := opts.PollInterval
		if poll <= 0 {
			poll = 150 * time.Millisecond
		}

		deadline := time.Now().Add(timeout)
		for {
			if ep, ok := readLiveServiceState(servicePath, username, isAlive); ok {
				return ep, nil
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return EndpointConfig{}, ctx.Err()
			case <-time.After(poll):
			}
		}
	}

	return EndpointConfig{}, fmt.Errorf("%w (checked %s); start OpenCode Desktop or run 'opencode service start'", ErrServiceNotRunning, servicePath)
}

func readLiveServiceState(path, username string, isAlive func(int) bool) (EndpointConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EndpointConfig{}, false
	}
	var st ServiceState
	if err := json.Unmarshal(data, &st); err != nil {
		return EndpointConfig{}, false
	}
	url := strings.TrimRight(strings.TrimSpace(st.URL), "/")
	if url == "" {
		return EndpointConfig{}, false
	}
	if st.PID > 0 && !isAlive(st.PID) {
		return EndpointConfig{}, false
	}
	return EndpointConfig{
		URL:      url,
		Username: username,
		Password: st.Password,
	}, true
}

func defaultIsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func defaultStartService(ctx context.Context) error {
	bin, err := FindOpenCodeBinary()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "service", "start")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("opencode service start failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
