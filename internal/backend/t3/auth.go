package t3

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

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
// via 't3 auth session issue --token-only'.
type SessionTokenProvider struct {
	mu           sync.RWMutex
	binPath      string
	runner       CommandRunner
	cachedToken  string
	tokenExpiry  time.Time
	issueTimeout time.Duration
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

// WithStaticSessionToken sets a pre-configured session token (bypassing CLI calls).
func WithStaticSessionToken(token string) SessionTokenOption {
	return func(p *SessionTokenProvider) {
		p.cachedToken = token
		p.tokenExpiry = time.Now().Add(24 * 365 * time.Hour)
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
		runner:       DefaultCommandRunner,
		issueTimeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetToken returns the active session token, issuing a new one if not cached.
func (p *SessionTokenProvider) GetToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	if p.cachedToken != "" && (p.tokenExpiry.IsZero() || time.Now().Before(p.tokenExpiry)) {
		token := p.cachedToken
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check after lock acquisition
	if p.cachedToken != "" && (p.tokenExpiry.IsZero() || time.Now().Before(p.tokenExpiry)) {
		return p.cachedToken, nil
	}

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

	p.cachedToken = token
	p.tokenExpiry = time.Now().Add(30 * 24 * time.Hour) // safe refresh window before 365d

	return token, nil
}

// Invalidate clears the currently cached token (e.g. upon receiving HTTP 401).
func (p *SessionTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedToken = ""
	p.tokenExpiry = time.Time{}
}
