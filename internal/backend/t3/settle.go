package t3

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrThreadNotFound is returned when T3 reports that the target thread no longer exists.
var ErrThreadNotFound = errors.New("t3 thread not found")

// BuildThreadSettleArgs builds argument list for `t3 thread settle <threadId> --json`.
func BuildThreadSettleArgs(threadID string) []string {
	return []string{"thread", "settle", threadID, "--json"}
}

// RunThreadSettle executes `t3 thread settle <threadId> --json`.
func RunThreadSettle(ctx context.Context, runner CommandRunner, bin string, threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return errors.New("thread ID is required for thread settle")
	}
	args := BuildThreadSettleArgs(threadID)
	_, err := runner.Run(ctx, bin, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return fmt.Errorf("%w: %v", ErrThreadNotFound, err)
		}
		return err
	}
	return nil
}
