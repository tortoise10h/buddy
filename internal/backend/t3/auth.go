package t3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tortoise10h/buddy/internal/state"
)

// DefaultSessionFilePath returns the standard location for persisting the T3 session token.
func DefaultSessionFilePath() string {
	return filepath.Join(state.GetBuddyDir(), "engine", "t3", "session.json")
}

// StoredSessionToken models the serialized session token persisted to disk with 0600 permissions.
type StoredSessionToken struct {
	Token     string    `json:"token"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CommandRunner defines the signature for executing external processes.
type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

// DefaultCommandRunner executes commands via os/exec.
func DefaultCommandRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), fmt.Errorf("command failed (exit %d): %s %v: %s", exitCode, name, args, errMsg)
	}
	return stdout.String(), nil
}

// SessionTokenProvider manages issuing and caching headless session bearer tokens
// via 't3 auth session issue --token-only'. It caches both in memory and on disk
// at ~/.buddy/engine/t3/session.json with 0600 permissions.
type SessionTokenProvider struct {
	mu              sync.RWMutex
	binPath         string
	runner          CommandRunner
	sessionFilePath string
	cachedToken     string
	tokenExpiry     time.Time
	issueTimeout    time.Duration
}

// SessionTokenOption configures the SessionTokenProvider.
type SessionTokenOption func(*SessionTokenProvider)

// WithBinPath sets the t3 binary path to use.
func WithBinPath(path string) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.binPath = path
	}
}

// WithCommandRunner overrides the runner (useful in tests).
func WithCommandRunner(r CommandRunner) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.runner = r
	}
}

// WithSessionFilePath sets a custom file path for disk caching.
func WithSessionFilePath(path string) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.sessionFilePath = path
	}
}

// WithDisableFileCache disables disk persistence (keeping cache in memory only).
func WithDisableFileCache() SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.sessionFilePath = ""
	}
}

// WithStaticSessionToken sets a pre-configured session token (bypassing CLI and disk calls).
func WithStaticSessionToken(token string) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.cachedToken = token
		p.tokenExpiry = time.Now().Add(24 * 365 * time.Hour)
		p.sessionFilePath = "" // static tokens do not need disk sync
	}
}

// WithIssueTimeout sets the execution timeout for issuing tokens.
func WithIssueTimeout(d time.Duration) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.issueTimeout = d
	}
}

// NewSessionTokenProvider creates a new provider.
func NewSessionTokenProvider(opts ...SessionTokenOption) *SessionTokenProvider {
	p := &SessionTokenProvider{
		runner:          DefaultCommandRunner,
		sessionFilePath: DefaultSessionFilePath(),
		issueTimeout:    10 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetToken returns the active session token, checking in-memory cache first,
// disk cache second, and issuing a new token via the CLI if both are absent or expired.
func (p *SessionTokenProvider) GetToken(ctx context.Context) (string, error) {
	// Fast Path 1: Check in-memory cache under read lock
	p.mu.RLock()
	if p.cachedToken != "" && (p.tokenExpiry.IsZero() || time.Now().Before(p.tokenExpiry)) {
		token := p.cachedToken
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check in-memory cache after acquiring write lock
	if p.cachedToken != "" && (p.tokenExpiry.IsZero() || time.Now().Before(p.tokenExpiry)) {
		return p.cachedToken, nil
	}

	// Fast Path 2: Check disk cache (~/.buddy/engine/t3/session.json)
	if p.sessionFilePath != "" {
		if stored, err := p.readDiskToken(); err == nil && stored != nil {
			if stored.Token != "" && (stored.ExpiresAt.IsZero() || time.Now().Before(stored.ExpiresAt)) {
				p.cachedToken = stored.Token
				p.tokenExpiry = stored.ExpiresAt
				return p.cachedToken, nil
			}
		}
	}

	// Slow Path: Issue new token via t3 CLI
	bin := p.binPath
	if bin == "" {
		resolved, err := FindT3Binary("")
		if err != nil {
			return "", err
		}
		bin = resolved
		p.binPath = bin
	}

	issueCtx, cancel := context.WithTimeout(ctx, p.issueTimeout)
	defer cancel()

	// t3 auth session issue --ttl 365d --token-only
	out, err := p.runner(issueCtx, bin, "auth", "session", "issue", "--ttl", "365d", "--token-only")
	if err != nil {
		return "", fmt.Errorf("failed to issue t3 session token: %w", err)
	}

	token := strings.TrimSpace(out)
	if token == "" {
		return "", fmt.Errorf("t3 auth session issue returned an empty token")
	}

	now := time.Now()
	expiry := now.Add(30 * 24 * time.Hour) // safe refresh window before 365d

	p.cachedToken = token
	p.tokenExpiry = expiry

	// Write to disk cache with 0600 permissions
	if p.sessionFilePath != "" {
		_ = p.writeDiskToken(StoredSessionToken{
			Token:     token,
			IssuedAt:  now,
			ExpiresAt: expiry,
		})
	}

	return token, nil
}

// Invalidate clears the currently cached token in memory and removes the disk cache file.
func (p *SessionTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedToken = ""
	p.tokenExpiry = time.Time{}
	if p.sessionFilePath != "" {
		_ = os.Remove(p.sessionFilePath)
	}
}

func (p *SessionTokenProvider) readDiskToken() (*StoredSessionToken, error) {
	data, err := os.ReadFile(p.sessionFilePath)
	if err != nil {
		return nil, err
	}
	var stored StoredSessionToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	return &stored, nil
}

func (p *SessionTokenProvider) writeDiskToken(stored StoredSessionToken) error {
	dir := filepath.Dir(p.sessionFilePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpFile := filepath.Join(dir, fmt.Sprintf(".session.json.tmp.%d", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpFile, p.sessionFilePath)
}
