package t3

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBuildThreadSettleArgs(t *testing.T) {
	got := BuildThreadSettleArgs("mcp:thread-1")
	want := []string{"thread", "settle", "mcp:thread-1", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildThreadSettleArgs = %v, want %v", got, want)
	}
}

func TestThreadSettle(t *testing.T) {
	t.Run("happy path and idempotent", func(t *testing.T) {
		var calls [][]string
		cr := CommandRunnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
			calls = append(calls, append([]string{name}, args...))
			return `{"threadId":"mcp:thread-abc","action":"settle","sequence":42}`, nil
		})
		runner := NewDefaultCommandRunner(WithBinPath("/fake/t3"), WithRunner(cr))

		if err := runner.ThreadSettle(context.Background(), "mcp:thread-abc"); err != nil {
			t.Fatalf("first ThreadSettle failed: %v", err)
		}
		if err := runner.ThreadSettle(context.Background(), "mcp:thread-abc"); err != nil {
			t.Fatalf("second ThreadSettle failed: %v", err)
		}
		if len(calls) != 2 {
			t.Fatalf("expected 2 CLI calls, got %d", len(calls))
		}
		want := []string{"/fake/t3", "thread", "settle", "mcp:thread-abc", "--json"}
		for i, c := range calls {
			if !reflect.DeepEqual(c, want) {
				t.Errorf("call %d = %v, want %v", i, c, want)
			}
		}
	})

	t.Run("missing thread maps to ErrThreadNotFound", func(t *testing.T) {
		cr := CommandRunnerFunc(func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", errors.New("t3 command failed (exit 1): The thread was not found.")
		})
		runner := NewDefaultCommandRunner(WithBinPath("/fake/t3"), WithRunner(cr))

		err := runner.ThreadSettle(context.Background(), "mcp:missing")
		if !errors.Is(err, ErrThreadNotFound) {
			t.Fatalf("expected ErrThreadNotFound, got %v", err)
		}
	})

	t.Run("empty thread ID returns error", func(t *testing.T) {
		runner := NewDefaultCommandRunner(WithBinPath("/fake/t3"))
		if err := runner.ThreadSettle(context.Background(), ""); err == nil {
			t.Fatal("expected error for empty thread ID")
		}
	})
}
