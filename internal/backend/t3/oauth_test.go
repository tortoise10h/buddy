package t3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/state"
)

func TestMCPOAuth_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")

	token := &MCPOAuthToken{
		AccessToken: "test-token-12345",
		Origin:      "http://127.0.0.1:3773",
		IssuedAt:    time.Now().Add(-1 * time.Hour),
		ExpiresAt:   time.Now().Add(29 * 24 * time.Hour),
		Scope:       "orchestration.operate orchestration.read",
	}

	if err := SaveMCPOAuthToken(tokenPath, token); err != nil {
		t.Fatalf("SaveMCPOAuthToken failed: %v", err)
	}

	// Verify file mode is strictly 0600
	fi, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file mode 0600, got %04o", perm)
	}

	loaded, err := LoadMCPOAuthToken(tokenPath)
	if err != nil {
		t.Fatalf("LoadMCPOAuthToken failed: %v", err)
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("expected access token %s, got %s", token.AccessToken, loaded.AccessToken)
	}
	if loaded.Origin != token.Origin {
		t.Errorf("expected origin %s, got %s", token.Origin, loaded.Origin)
	}
	if loaded.Scope != token.Scope {
		t.Errorf("expected scope %s, got %s", token.Scope, loaded.Scope)
	}
}

func TestMCPOAuth_NotStoredInState(t *testing.T) {
	tmpBuddyDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpBuddyDir)

	tokenPath := filepath.Join(tmpBuddyDir, "t3_mcp_oauth.json")
	secretToken := "secret-super-private-oauth-token-999"

	token := &MCPOAuthToken{
		AccessToken: secretToken,
		Origin:      "http://127.0.0.1:3773",
		ExpiresAt:   time.Now().Add(10 * time.Hour),
	}

	if err := SaveMCPOAuthToken(tokenPath, token); err != nil {
		t.Fatalf("SaveMCPOAuthToken failed: %v", err)
	}

	// Read state.json if it exists, or verify state save doesn't leak token
	statePath := filepath.Join(tmpBuddyDir, "state.json")
	if _, err := os.Stat(statePath); err == nil {
		content, _ := os.ReadFile(statePath)
		if strings.Contains(string(content), secretToken) {
			t.Fatalf("CRITICAL SECURITY VIOLATION: oauth token found inside state.json")
		}
	}

	// Ensure LoadState doesn't know about or touch t3_mcp_oauth.json
	st, err := state.LoadState()
	if err != nil {
		t.Fatalf("state.LoadState failed: %v", err)
	}
	_ = st

	// Explicitly verify state.json does NOT contain the oauth token
	if data, err := os.ReadFile(statePath); err == nil {
		if strings.Contains(string(data), secretToken) {
			t.Fatalf("CRITICAL: oauth token written to state.json!")
		}
	}
}

func TestMCPOAuth_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	missingPath := filepath.Join(tmpDir, "non_existent_token.json")

	_, err := LoadMCPOAuthToken(missingPath)
	if err == nil {
		t.Fatalf("expected error for missing token file, got nil")
	}
	if !errors.Is(err, ErrMcpAuthRequired) {
		t.Errorf("expected ErrMcpAuthRequired, got: %v", err)
	}
	if !strings.Contains(err.Error(), "buddy-ctl t3 auth login") {
		t.Errorf("error message must tell user to run 'buddy-ctl t3 auth login', got: %v", err)
	}

	// Also verify FileMCPTokenProvider returns the clear error
	provider := NewFileMCPTokenProvider(missingPath)
	_, err = provider.GetMCPToken(context.Background())
	if !errors.Is(err, ErrMcpAuthRequired) {
		t.Errorf("expected ErrMcpAuthRequired from provider, got: %v", err)
	}
}

func TestMCPOAuth_ExpiredToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "t3_mcp_oauth.json")

	expiredToken := &MCPOAuthToken{
		AccessToken: "expired-token",
		ExpiresAt:   time.Now().Add(-2 * time.Hour), // Expired 2 hours ago
	}

	if err := SaveMCPOAuthToken(tokenPath, expiredToken); err != nil {
		t.Fatalf("SaveMCPOAuthToken failed: %v", err)
	}

	_, err := LoadMCPOAuthToken(tokenPath)
	if err == nil {
		t.Fatalf("expected error for expired token, got nil")
	}
	if !errors.Is(err, ErrMcpAuthRequired) {
		t.Errorf("expected ErrMcpAuthRequired, got: %v", err)
	}
	if !strings.Contains(err.Error(), "buddy-ctl t3 auth login") {
		t.Errorf("error message must tell user to run 'buddy-ctl t3 auth login', got: %v", err)
	}
}

func TestMCPOAuth_FlexibleTimestamps(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Unix timestamp format
	jsonUnix := []byte(`{
		"access_token": "token-unix",
		"origin": "http://127.0.0.1:3773",
		"issued_at": 1700000000,
		"expires_at": 1900000000
	}`)
	path1 := filepath.Join(tmpDir, "token1.json")
	if err := os.WriteFile(path1, jsonUnix, 0600); err != nil {
		t.Fatal(err)
	}

	tok1, err := LoadMCPOAuthToken(path1)
	if err != nil {
		t.Fatalf("failed to load token with unix timestamp: %v", err)
	}
	if tok1.AccessToken != "token-unix" {
		t.Errorf("expected token-unix, got %s", tok1.AccessToken)
	}
	if tok1.ExpiresAt.Unix() != 1900000000 {
		t.Errorf("expected expires_at 1900000000, got %d", tok1.ExpiresAt.Unix())
	}

	// 2. expires_in format
	jsonExpiresIn := []byte(`{
		"access_token": "token-expires-in",
		"expires_in": 3600
	}`)
	path2 := filepath.Join(tmpDir, "token2.json")
	if err := os.WriteFile(path2, jsonExpiresIn, 0600); err != nil {
		t.Fatal(err)
	}

	tok2, err := LoadMCPOAuthToken(path2)
	if err != nil {
		t.Fatalf("failed to load token with expires_in: %v", err)
	}
	if tok2.AccessToken != "token-expires-in" {
		t.Errorf("expected token-expires-in, got %s", tok2.AccessToken)
	}
	if tok2.IsExpired() {
		t.Errorf("token should not be expired")
	}
}
