package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/t3"
)

// T3Engine adapts t3.T3Backend (WorkerBackend) to the Engine interface.
type T3Engine struct {
	backend *t3.T3Backend
}

// NewT3Engine wraps a T3Backend to satisfy the Engine interface for CLI orchestration.
func NewT3Engine(backend *t3.T3Backend) *T3Engine {
	if backend == nil {
		backend = t3.NewT3Backend()
	}
	return &T3Engine{backend: backend}
}

func (e *T3Engine) Name() string {
	return "t3code"
}

func (e *T3Engine) Backend() *t3.T3Backend {
	return e.backend
}

func (e *T3Engine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	agentSpec := ""
	if len(req.AgentCmd) > 0 {
		agentSpec = req.AgentCmd[0]
	}

	spec := backend.DispatchSpec{
		ProjectName: req.ProjectName,
		ProjectPath: req.ProjectPath,
		TaskSlug:    req.TaskSlug,
		Prompt:      req.Prompt,
		AgentSpec:   agentSpec,
		UseWorktree: true,
	}

	res, err := e.backend.DispatchTask(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("t3 dispatch failed: %w", err)
	}

	result := &DispatchResult{
		SessionID: res.TaskHandle.ProjectID,
		WindowID:  res.TaskHandle.ThreadID,
		BlockID:   res.TaskHandle.ThreadID,
		PickedUp:  res.PickedUp,
		Extra: map[string]any{
			"backend":       "t3code",
			"thread_id":     res.TaskHandle.ThreadID,
			"run_id":        res.TaskHandle.ActiveRunID,
			"branch":        res.TaskHandle.Branch,
			"worktree_path": res.TaskHandle.WorktreePath,
		},
	}
	return result, nil
}

func (e *T3Engine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	return e.backend.CaptureOutput(ctx, backend.TaskHandle{ThreadID: blockID}, lines)
}

func (e *T3Engine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	handle := backend.TaskHandle{ThreadID: blockID}

	if strings.EqualFold(input.Key, "ctrl+c") || strings.EqualFold(input.Key, "esc") {
		return e.backend.InterruptTask(ctx, handle)
	}

	msg := backend.MessageInput{
		Text:         input.Text,
		Answers:      input.Answers,
		KeyedAnswers: input.KeyedAnswers,
	}
	return e.backend.SendMessage(ctx, handle, msg)
}

func (e *T3Engine) Close(ctx context.Context, sessionID, windowID string) error {
	return e.backend.CloseTask(ctx, backend.TaskHandle{ThreadID: windowID})
}

func (e *T3Engine) Focus(ctx context.Context, sessionID, windowID string) error {
	// Thread-level focus is descoped for T3 Code; clean no-op.
	return nil
}

func (e *T3Engine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", fmt.Errorf("t3code engine does not support native dev splits (use host terminal mux)")
}
