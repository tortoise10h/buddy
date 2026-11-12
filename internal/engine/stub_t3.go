package engine

import (
	"context"
	"fmt"

	"github.com/tortoise10h/buddy/internal/backend"
)

// StubT3Engine implements engine.Engine for T3 Code environments during Step 1.
// Mutation and capture operations return typed ErrNotImplemented.
// Focus is a clean no-op returning nil.
type StubT3Engine struct{}

func NewStubT3Engine() *StubT3Engine {
	return &StubT3Engine{}
}

func (e *StubT3Engine) Name() string {
	return "t3"
}

func (e *StubT3Engine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	return nil, &ErrNotImplemented{
		Engine:    "t3",
		Operation: "dispatch",
		Message:   fmt.Sprintf("t3 engine mutation layer is not yet implemented in Step 1 (scheduled for Step 2). Task '%s' was not dispatched.", req.TaskSlug),
	}
}

func (e *StubT3Engine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	return "", &ErrNotImplemented{
		Engine:    "t3",
		Operation: "capture",
		Message:   "t3 engine capture is not yet implemented in Step 1 (scheduled for Step 2).",
	}
}

func (e *StubT3Engine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	return &ErrNotImplemented{
		Engine:    "t3",
		Operation: "send",
		Message:   "t3 engine send is not yet implemented in Step 1 (scheduled for Step 2).",
	}
}

func (e *StubT3Engine) Close(ctx context.Context, sessionID, windowID string) error {
	return &ErrNotImplemented{
		Engine:    "t3",
		Operation: "close",
		Message:   "t3 engine close is not yet implemented in Step 1 (scheduled for Step 2).",
	}
}

func (e *StubT3Engine) Focus(ctx context.Context, sessionID, windowID string) error {
	// Focus is a clean no-op for T3 (plan section 3)
	return nil
}

func (e *StubT3Engine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", &ErrNotImplemented{
		Engine:    "t3",
		Operation: "open_dev_split",
		Message:   "t3 engine open dev split is not yet implemented in Step 1 (scheduled for Step 2).",
	}
}
