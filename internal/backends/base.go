package backends

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SessionRef identifies a top-level multiplexer session.
type SessionRef struct {
	SessionID string         `json:"session_id"`
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// WindowRef identifies a window or tab within a session.
type WindowRef struct {
	WindowID  string         `json:"window_id"`
	Label     string         `json:"label"`
	SessionID string         `json:"session_id"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// BlockRef identifies a terminal pane or execution block.
type BlockRef struct {
	BlockID   string         `json:"block_id"`
	WindowID  string         `json:"window_id"`
	SessionID string         `json:"session_id"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// CaptureResult holds captured terminal text and process status.
type CaptureResult struct {
	Text    string   `json:"text"`
	Lines   []string `json:"lines"`
	IsAlive bool     `json:"is_alive"`
}

// BackendCapabilities declares backend-specific behaviors and timing profiles.
type BackendCapabilities struct {
	SupportsWorktree     bool `json:"supports_worktree"`
	SupportsSplit        bool `json:"supports_split"`
	CaptureHasScrollback bool `json:"capture_has_scrollback"`
	NeedsTUIProbe        bool `json:"needs_tui_probe"`
	DefaultSubmitDelayMS int  `json:"default_submit_delay_ms"`
	SupportsNativeWait   bool `json:"supports_native_wait"`
	SupportsFocus        bool `json:"supports_focus"`
}

var (
	ErrBackendUnavailable = errors.New("backend unavailable")
	ErrSessionNotFound    = errors.New("session not found")
	ErrWindowNotFound     = errors.New("window not found")
	ErrBlockNotFound      = errors.New("block not found")
	ErrUnsupportedSplit   = errors.New("backend does not support pane splits")
)

// BackendExecutionError represents a non-zero exit from an underlying multiplexer CLI.
type BackendExecutionError struct {
	Backend    string
	Command    string
	Args       []string
	ExitCode   int
	Stderr     string
	Stdout     string
	Underlying error
}

func (e *BackendExecutionError) Error() string {
	return fmt.Sprintf("%s command failed (exit %d): %s %v: %s",
		e.Backend, e.ExitCode, e.Command, e.Args, e.Stderr)
}

func (e *BackendExecutionError) Unwrap() error {
	return e.Underlying
}

// TerminalBackend defines the interface that all multiplexers (Rex, Herdr, WezTerm) must satisfy.
type TerminalBackend interface {
	// Name returns the canonical identifier (e.g., "rex", "herdr", "wezterm").
	Name() string

	// Capabilities returns features supported by this backend.
	Capabilities() BackendCapabilities

	// IsAvailable verifies if the multiplexer binary or socket is operational.
	IsAvailable(ctx context.Context) bool

	// EnsureSession returns an existing session by name or creates a new one.
	EnsureSession(ctx context.Context, name string, cwd string) (SessionRef, error)

	// ListSessions returns all active sessions managed by this multiplexer.
	ListSessions(ctx context.Context) ([]SessionRef, error)

	// InspectSession retrieves details of a specific session.
	InspectSession(ctx context.Context, sessionID string) (SessionRef, error)

	// CreateTaskWindow spawns a dedicated tab/window running the agent command without stealing focus.
	CreateTaskWindow(ctx context.Context, session SessionRef, taskSlug string, cwd string, agentCmd []string) (WindowRef, BlockRef, error)

	// SendText sends characters into the target pane without submitting.
	SendText(ctx context.Context, sessionID, blockID, text string) error

	// SendKey transmits special keys (e.g., "enter", "ctrl+c", "esc") into the pane.
	SendKey(ctx context.Context, sessionID, blockID, key string) error

	// Capture retrieves terminal scrollback lines from the pane.
	Capture(ctx context.Context, sessionID, blockID string, lines int) (CaptureResult, error)

	// Focus switches UI window/tab focus to the target session/window.
	Focus(ctx context.Context, sessionID string, windowID string) error

	// CloseWindow closes the target window/tab.
	CloseWindow(ctx context.Context, sessionID, windowID string) error

	// OpenDevSplit splits the worker pane (e.g. right 40%) to run a companion dev process.
	OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (BlockRef, error)

	// WaitReady polls until the agent TUI is responsive and ready to accept input.
	WaitReady(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error)

	// DetectPrompt checks if the active pane is currently idling at an interactive prompt.
	DetectPrompt(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error)
}
