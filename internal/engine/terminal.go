package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
)

var readinessIndicators = []string{
	"? for shortcuts",
	"auto ·",
	"Ask anything",
	"Type a message",
	"Gemini",
	"OpenCode",
	"Anthropic",
	"Claude",
}

var promptLineRe = regexp.MustCompile(`(?m)^[>❯]\s*$`)

// TerminalEngine coordinates the task lifecycle on top of any terminal multiplexer (Rex, WezTerm, Herdr).
// It manages session resolution, window creation, reactive TUI readiness probing, and prompt delivery.
type TerminalEngine struct {
	backend          backend.TerminalBackend
	readinessTimeout time.Duration
}

// NewTerminalEngine wraps any TerminalBackend with the terminal dispatch and prompt injection lifecycle.
func NewTerminalEngine(tb backend.TerminalBackend, opts ...func(*TerminalEngine)) *TerminalEngine {
	e := &TerminalEngine{
		backend:          tb,
		readinessTimeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// WithReadinessTimeout sets how long the engine will wait for the agent TUI prompt to appear.
func WithReadinessTimeout(d time.Duration) func(*TerminalEngine) {
	return func(e *TerminalEngine) {
		e.readinessTimeout = d
	}
}

func (e *TerminalEngine) Name() string {
	return e.backend.Name()
}

func (e *TerminalEngine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	// 1. Ensure project session exists
	sess, err := e.backend.EnsureSession(ctx, req.ProjectName, req.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("ensure session failed: %w", err)
	}

	// 2. Spawn task window running the agent command
	winID, blockID, err := e.backend.CreateTaskWindow(ctx, sess.SessionID, req.TaskSlug, req.ProjectPath, req.AgentCmd)
	if err != nil {
		return nil, fmt.Errorf("create task window failed: %w", err)
	}

	result := &DispatchResult{
		SessionID: sess.SessionID,
		WindowID:  winID,
		BlockID:   blockID,
		PickedUp:  false,
	}

	if req.Prompt == "" {
		return result, nil
	}

	// 3. Reactive TUI Probe: wait until agent prompt is ready
	_ = e.WaitReady(ctx, sess.SessionID, blockID, e.readinessTimeout)

	// 4. Send prompt text and enter
	if err := e.backend.Send(ctx, sess.SessionID, blockID, backend.Input{
		Text:  req.Prompt,
		Enter: true,
	}); err != nil {
		return result, fmt.Errorf("failed to deliver prompt: %w", err)
	}

	// 5. Brief pickup verification
	time.Sleep(200 * time.Millisecond)
	capText, _ := e.backend.Capture(ctx, sess.SessionID, blockID, 20)
	result.PickedUp = len(capText) > 0

	return result, nil
}

// WaitReady polls until the worker agent TUI is responsive and ready to accept input.
func (e *TerminalEngine) WaitReady(ctx context.Context, sessionID, blockID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		out, err := e.backend.Capture(ctx, sessionID, blockID, 0)
		if err == nil && out != "" {
			for _, ind := range readinessIndicators {
				if strings.Contains(out, ind) {
					return true
				}
			}
			if promptLineRe.MatchString(out) {
				return true
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func (e *TerminalEngine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	return e.backend.Capture(ctx, sessionID, blockID, lines)
}

func (e *TerminalEngine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	return e.backend.Send(ctx, sessionID, blockID, input)
}

func (e *TerminalEngine) Close(ctx context.Context, sessionID, windowID string) error {
	return e.backend.Close(ctx, sessionID, windowID)
}

func (e *TerminalEngine) Focus(ctx context.Context, sessionID, windowID string) error {
	return e.backend.Focus(ctx, sessionID, windowID)
}

func (e *TerminalEngine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return e.backend.OpenDevSplit(ctx, sessionID, blockID, cwd, cmd)
}

// GetProgramStatus queries native terminal program status if the underlying backend supports it.
func (e *TerminalEngine) GetProgramStatus(ctx context.Context, sessionID, blockID string) ([]backend.ProgramStatusRecord, error) {
	if p, ok := e.backend.(backend.ProgramStatusProvider); ok {
		return p.GetProgramStatus(ctx, sessionID, blockID)
	}
	return nil, nil
}
