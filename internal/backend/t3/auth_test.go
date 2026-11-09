package t3

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionTokenProvider_CachingAndIssuance(t *testing.T) {
	callCount := 0
	fakeToken := "t3_session_token_12345"
	sessionPath := filepath.Join(t.TempDir(), "session.json")

	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		callCount++
		cmdStr := strings.Join(args, " ")
		if !strings.Contains(cmdStr, "auth session issue") {
			t.Fatalf("unexpected command args: %v", args)
		}
		if !strings.Contains(cmdStr, "--token-only") {
			t.Fatalf("expected --token-only flag, got: %v", args)
		}
		return fakeToken + "\n", nil
	}

	provider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
		WithSessionFilePath(sessionPath),
	)

	// First call should execute runner and persist to disk
	token1, err := provider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token1 != fakeToken {
		t.Errorf("expected %s, got %s", fakeToken, token1)
	}
	if callCount != 1 {
		t.Errorf("expected 1 runner call, got %d", callCount)
	}

	// Verify file was written to disk with 0600 permissions
	fi, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("expected session file on disk, got: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %o", fi.Mode().Perm())
	}

	// Second call on the same provider should return cached token from memory
	token2, err := provider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("second GetToken failed: %v", err)
	}
	if token2 != fakeToken {
		t.Errorf("expected cached token %s, got %s", fakeToken, token2)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 runner call due to in-memory cache, got %d", callCount)
	}

	// Simulate a BRAND NEW process: create a fresh provider with empty in-memory cache
	// pointing to the same disk file path. It should read from disk without calling runner!
	newProcessProvider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
		WithSessionFilePath(sessionPath),
	)

	diskToken, err := newProcessProvider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("disk GetToken failed: %v", err)
	}
	if diskToken != fakeToken {
		t.Errorf("expected disk token %s, got %s", fakeToken, diskToken)
	}
	if callCount != 1 {
		t.Errorf("expected runner call count still 1 (read from disk), got %d", callCount)
	}

	// Invalidate clears in-memory cache AND removes the file on disk
	newProcessProvider.Invalidate()
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Errorf("expected session file to be removed after Invalidate(), got: %v", err)
	}

	// Third call should re-invoke runner
	token3, err := newProcessProvider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("third GetToken failed: %v", err)
	}
	if token3 != fakeToken {
		t.Errorf("expected %s, got %s", fakeToken, token3)
	}
	if callCount != 2 {
		t.Errorf("expected 2 runner calls after invalidate, got %d", callCount)
	}
}

func TestSessionTokenProvider_ExpiredDiskToken(t *testing.T) {
	callCount := 0
	sessionPath := filepath.Join(t.TempDir(), "session.json")

	// Write an expired token to disk
	expired := StoredSessionToken{
		Token:     "old_expired_token",
		IssuedAt:  time.Now().Add(-48 * time.Hour),
		ExpiresAt: time.Now().Add(-24 * time.Hour),
	}
	data, _ := json.Marshal(expired)
	_ = os.WriteFile(sessionPath, data, 0600)

	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		callCount++
		return "fresh_issued_token\n", nil
	}

	provider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
		WithSessionFilePath(sessionPath),
	)

	token, err := provider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token != "fresh_issued_token" {
		t.Errorf("expected fresh token, got %s", token)
	}
	if callCount != 1 {
		t.Errorf("expected 1 runner call because disk token was expired, got %d", callCount)
	}
}

func TestSessionTokenProvider_RunnerError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		return "", errors.New("exit code 1: internal error")
	}

	provider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
		WithDisableFileCache(),
	)

	_, err := provider.GetToken(context.Background())
	if err == nil {
		t.Fatalf("expected error from runner, got nil")
	}
}

func TestSessionTokenProvider_Timeout(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return "token\n", nil
		}
	}

	provider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
		WithDisableFileCache(),
		WithIssueTimeout(5*time.Millisecond), // Fast timeout
	)

	_, err := provider.GetToken(context.Background())
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestSessionTokenProvider_StaticToken(t *testing.T) {
	staticToken := "static_secret_token"
	provider := NewSessionTokenProvider(
		WithStaticSessionToken(staticToken),
	)

	token, err := provider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token != staticToken {
		t.Errorf("expected %s, got %s", staticToken, token)
	}
}
