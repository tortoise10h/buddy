package t3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionTokenProvider_CachingAndIssuance(t *testing.T) {
	callCount := 0
	fakeToken := "t3_session_token_12345"

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
	)

	// First call should execute runner
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

	// Second call should return cached token without calling runner
	token2, err := provider.GetToken(context.Background())
	if err != nil {
		t.Fatalf("second GetToken failed: %v", err)
	}
	if token2 != fakeToken {
		t.Errorf("expected cached token %s, got %s", fakeToken, token2)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 runner call due to cache, got %d", callCount)
	}

	// Invalidate clears cache
	provider.Invalidate()

	// Third call should re-invoke runner
	token3, err := provider.GetToken(context.Background())
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

func TestSessionTokenProvider_RunnerError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		return "", errors.New("exit code 1: internal error")
	}

	provider := NewSessionTokenProvider(
		WithBinPath("/fake/bin/t3"),
		WithCommandRunner(runner),
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
