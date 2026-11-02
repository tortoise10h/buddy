package t3

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MCPOAuthToken models the 30-day OAuth credentials stored at ~/.buddy/t3_mcp_oauth.json.
// Per Decision 1, this token is stored with file mode 0600 and NEVER in state.json.
type MCPOAuthToken struct {
	AccessToken string    `json:"access_token"`
	Origin      string    `json:"origin,omitempty"`
	IssuedAt    time.Time `json:"issued_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scope       string    `json:"scope,omitempty"`
}

// rawOAuthToken is an internal helper for flexible timestamp decoding.
type rawOAuthToken struct {
	AccessToken string          `json:"access_token"`
	AccessTokenAlt string       `json:"accessToken"`
	Origin      string          `json:"origin,omitempty"`
	IssuedAt    json.RawMessage `json:"issued_at,omitempty"`
	ExpiresAt   json.RawMessage `json:"expires_at,omitempty"`
	ExpiresIn   json.RawMessage `json:"expires_in,omitempty"`
	Scope       string          `json:"scope,omitempty"`
}

func parseFlexibleTime(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	str := strings.Trim(string(raw), "\"")
	// Try RFC3339 / ISO8601
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
		return t, nil
	}
	// Try integer unix timestamp (seconds or milliseconds)
	if num, err := strconv.ParseInt(str, 10, 64); err == nil {
		if num > 1e11 { // milliseconds
			return time.UnixMilli(num), nil
		}
		return time.Unix(num, 0), nil
	}
	// Try float
	if flt, err := strconv.ParseFloat(str, 64); err == nil {
		return time.Unix(int64(flt), 0), nil
	}
	return time.Time{}, fmt.Errorf("unable to parse time: %s", str)
}

// UnmarshalJSON implements custom JSON unmarshaling to handle flexible time formats.
func (t *MCPOAuthToken) UnmarshalJSON(data []byte) error {
	var raw rawOAuthToken
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	token := raw.AccessToken
	if token == "" {
		token = raw.AccessTokenAlt
	}
	t.AccessToken = token
	t.Origin = raw.Origin
	t.Scope = raw.Scope

	if len(raw.IssuedAt) > 0 {
		issued, err := parseFlexibleTime(raw.IssuedAt)
		if err == nil {
			t.IssuedAt = issued
		}
	}

	if len(raw.ExpiresAt) > 0 {
		expires, err := parseFlexibleTime(raw.ExpiresAt)
		if err == nil {
			t.ExpiresAt = expires
		}
	} else if len(raw.ExpiresIn) > 0 {
		var secs int64
		if err := json.Unmarshal(raw.ExpiresIn, &secs); err == nil && secs > 0 {
			base := t.IssuedAt
			if base.IsZero() {
				base = time.Now()
			}
			t.ExpiresAt = base.Add(time.Duration(secs) * time.Second)
		}
	}

	return nil
}

// IsExpired returns true if the token is past its expiration time.
func (t *MCPOAuthToken) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// DefaultMCPOAuthPath returns the canonical path for ~/.buddy/t3_mcp_oauth.json.
func DefaultMCPOAuthPath() string {
	if dir := os.Getenv("BUDDY_DIR"); dir != "" {
		return filepath.Join(dir, "t3_mcp_oauth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".buddy", "t3_mcp_oauth.json")
	}
	return filepath.Join(home, ".buddy", "t3_mcp_oauth.json")
}

// SaveMCPOAuthToken writes the OAuth token to disk with strict 0600 file permissions.
func SaveMCPOAuthToken(path string, token *MCPOAuthToken) error {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory for mcp oauth token: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp oauth token: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open mcp oauth token file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write mcp oauth token: %w", err)
	}

	// Ensure permissions are strictly 0600 even if file already existed
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("failed to set 0600 permissions on mcp oauth token: %w", err)
	}

	return nil
}

// LoadMCPOAuthToken reads the OAuth token from disk and validates expiry.
func LoadMCPOAuthToken(path string) (*MCPOAuthToken, error) {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token is missing (file %s not found). Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired, path)
		}
		return nil, fmt.Errorf("failed to read mcp oauth token from %s: %w", path, err)
	}

	var token MCPOAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("malformed mcp oauth token at %s: %w", path, err)
	}

	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token is empty. Run 'buddy-ctl t3 auth login' to authenticate", ErrMcpAuthRequired)
	}

	if token.IsExpired() {
		return nil, fmt.Errorf("%w: T3 Code /mcp OAuth token expired at %s. Run 'buddy-ctl t3 auth login' to re-authenticate", ErrMcpAuthRequired, token.ExpiresAt.Format(time.RFC3339))
	}

	return &token, nil
}

// FileMCPTokenProvider implements MCPTokenProvider by loading ~/.buddy/t3_mcp_oauth.json.
type FileMCPTokenProvider struct {
	mu       sync.RWMutex
	filePath string
}

// NewFileMCPTokenProvider creates a provider for the given file path.
func NewFileMCPTokenProvider(path string) *FileMCPTokenProvider {
	if path == "" {
		path = DefaultMCPOAuthPath()
	}
	return &FileMCPTokenProvider{filePath: path}
}

// GetMCPToken reads and validates the token from file.
func (p *FileMCPTokenProvider) GetMCPToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	token, err := LoadMCPOAuthToken(p.filePath)
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// SetFilePath updates the target file path.
func (p *FileMCPTokenProvider) SetFilePath(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filePath = path
}
