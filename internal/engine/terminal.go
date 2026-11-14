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
var ansiStripRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\].*?(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
var workingActivityRe = regexp.MustCompile(`[\x{2800}-\x{28ff}⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒]|(?i)(?:running\s+command|thought\s+for|thinking\.\.\.|esc\s+to\s+cancel)`)
var onboardingOverlayRe = regexp.MustCompile(`(?i)(?:esc\s+to\s+dismiss|press\s+enter\s+to\s+dismiss|auto\s+mode:\s+edits\s+&\s+safe\s+commands\s+auto-approved)`)

// TerminalEngine coordinates the task lifecycle on top of any terminal multiplexer (Rex, WezTerm, Herdr).
// It manages session resolution, window creation, reactive TUI readiness probing, and prompt delivery.
type TerminalEngine struct {
	backend                 backend.TerminalBackend
	readinessTimeout        time.Duration
	pickupVerifyTimeout     time.Duration
	pickupPollInterval      time.Duration
}

// NewTerminalEngine wraps any TerminalBackend with the terminal dispatch and prompt injection lifecycle.
func NewTerminalEngine(tb backend.TerminalBackend, opts ...func(*TerminalEngine)) *TerminalEngine {
	e := &TerminalEngine{
		backend:             tb,
		readinessTimeout:    10 * time.Second,
		pickupVerifyTimeout: 600 * time.Millisecond,
		pickupPollInterval:  100 * time.Millisecond,
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

// WithPickupVerifyTimeout sets how long Dispatch polls to confirm prompt pickup.
func WithPickupVerifyTimeout(timeout, pollInterval time.Duration) func(*TerminalEngine) {
	return func(e *TerminalEngine) {
		e.pickupVerifyTimeout = timeout
		if pollInterval > 0 {
			e.pickupPollInterval = pollInterval
		}
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

	// 3. Reactive TUI Probe: wait until agent prompt is ready (dismissing onboarding modal if present)
	preSendCap, _ := e. waitReadyAndCapture(ctx, sess.SessionID, blockID, e.readinessTimeout)

	// 4. Send prompt text and enter
	if err := e.backend.Send(ctx, sess.SessionID, blockID, backend.Input{
		Text:  req.Prompt,
		Enter: true,
	}); err != nil {
		result.PickupReason = fmt.Sprintf("failed to send prompt: %v", err)
		return result, fmt.Errorf("failed to deliver prompt: %w", err)
	}

	// 5. Real pickup verification: confirm prompt echo or agent working activity
	pickedUp, reason := e.verifyPickup(ctx, sess.SessionID, blockID, req.Prompt, preSendCap)
	result.PickedUp = pickedUp
	result.PickupReason = reason

	return result, nil
}

func normalizePaneText(s string) string {
	clean := ansiStripRe.ReplaceAllString(s, "")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	return strings.TrimSpace(clean)
}

func isPromptEchoed(paneText, prompt string) bool {
	cleanPane := strings.ToLower(normalizePaneText(paneText))
	cleanPrompt := strings.ToLower(strings.TrimSpace(prompt))
	if cleanPane == "" || cleanPrompt == "" {
		return false
	}

	firstLine := strings.Split(cleanPrompt, "\n")[0]
	firstLine = strings.TrimSpace(firstLine)
	if len(firstLine) > 36 {
		firstLine = strings.TrimSpace(firstLine[:36])
	}
	if firstLine != "" && strings.Contains(cleanPane, firstLine) {
		return true
	}

	words := strings.Fields(cleanPrompt)
	if len(words) >= 3 {
		prefix := strings.Join(words[:3], " ")
		if strings.Contains(cleanPane, prefix) {
			return true
		}
	}
	return false
}

func (e *TerminalEngine) verifyPickup(ctx context.Context, sessionID, blockID, prompt, preSendCap string) (bool, string) {
	timeout := e.pickupVerifyTimeout
	if timeout <= 0 {
		timeout = 600 * time.Millisecond
	}
	interval := e.pickupPollInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}

	normBefore := normalizePaneText(preSendCap)
	deadline := time.Now().Add(timeout)
	var lastCap string
	first := true

	for {
		if !first {
			select {
			case <-ctx.Done():
				return false, "context cancelled before pickup verification completed"
			case <-time.After(interval):
			}
		}
		first = false

		// Check native OSC 7501 program_status if available
		if recs, err := e.GetProgramStatus(ctx, sessionID, blockID); err == nil && len(recs) > 0 {
			for _, r := range recs {
				if r.State == "working" || r.State == "blocked" || r.State == "done" {
					return true, ""
				}
			}
		}

		capText, err := e.backend.Capture(ctx, sessionID, blockID, 30)
		if err == nil && capText != "" {
			lastCap = capText
			normAfter := normalizePaneText(capText)

			// 1. Prompt text echoed in pane
			if isPromptEchoed(normAfter, prompt) {
				return true, ""
			}

			// 2. Agent entered active working state (spinner / Running command / Thought for / esc to cancel)
			if workingActivityRe.MatchString(normAfter) {
				return true, ""
			}

			// 3. If pre-send capture was non-empty and pane changed meaningfully (not stuck on onboarding overlay)
			if normBefore != "" && normAfter != normBefore && !onboardingOverlayRe.MatchString(normAfter) {
				return true, ""
			}
		}

		if !time.Now().Before(deadline) {
			break
		}
	}

	normLast := normalizePaneText(lastCap)
	if normLast == "" {
		return false, "pane output is empty after sending prompt"
	}
	if onboardingOverlayRe.MatchString(normLast) {
		return false, "agent onboarding modal ('esc to dismiss') is still active and swallowed or blocked the prompt"
	}
	return false, "prompt was not echoed and no agent activity was detected in pane"
}

func (e *TerminalEngine) waitReadyAndCapture(ctx context.Context, sessionID, blockID string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	var lastOut string
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return lastOut, false
		default:
		}

		out, err := e.backend.Capture(ctx, sessionID, blockID, 0)
		if err == nil && out != "" {
			lastOut = out
			for _, ind := range readinessIndicators {
				if strings.Contains(out, ind) {
					return out, true
				}
			}
			if promptLineRe.MatchString(out) {
				return out, true
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return lastOut, false
}

// WaitReady polls until the worker agent TUI is responsive and ready to accept input.
func (e *TerminalEngine) WaitReady(ctx context.Context, sessionID, blockID string, timeout time.Duration) bool {
	_, ok := e.waitReadyAndCapture(ctx, sessionID, blockID, timeout)
	return ok
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
