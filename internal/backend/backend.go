package backend

import (
	"context"
)

// Input represents text and/or keystrokes to send to a running terminal pane.
type Input struct {
	Text  string
	Key   string
	Enter bool
}

// SessionInfo represents a project session or workspace.
type SessionInfo struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

// WindowInfo represents a tab or window in a multiplexer session.
type WindowInfo struct {
	WindowID  string   `json:"window_id"`
	Label     string   `json:"label"`
	SessionID string   `json:"session_id"`
	BlockIDs  []string `json:"block_ids,omitempty"`
}

// TerminalBackend maps 1:1 to low-level multiplexer operations (Rex, WezTerm, Herdr).
// It contains pure terminal primitives and no agent or prompt orchestration logic.
type TerminalBackend interface {
	Name() string
	IsAvailable(ctx context.Context) bool

	// Session management
	EnsureSession(ctx context.Context, name string, cwd string) (SessionInfo, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)

	// Window & tab management
	ListWindows(ctx context.Context, sessionID string) ([]WindowInfo, error)
	InspectWindow(ctx context.Context, sessionID, windowID string) (WindowInfo, error)
	CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error)
	Close(ctx context.Context, sessionID, windowID string) error
	Focus(ctx context.Context, sessionID, windowID string) error

	// Pane / block operations
	Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error)
	Send(ctx context.Context, sessionID, blockID string, input Input) error
	OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error)
	IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error)
}

// ProgramStatusRecord represents an OSC 7501 status record reported by a program in a terminal block.
type ProgramStatusRecord struct {
	ID           string `json:"id,omitempty"`
	State        string `json:"state"`          // idle, working, done, blocked, error
	Kind         string `json:"kind,omitempty"` // permission, question, auth
	Progress     *int   `json:"progress,omitempty"`
	App          string `json:"app,omitempty"`
	EffectiveApp string `json:"effective_app,omitempty"`
	Title        string `json:"title,omitempty"`
	Msg          string `json:"msg,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// ProgramStatusProvider is an optional interface implemented by backends or engines
// that can query native terminal program status (e.g. Rex OSC 7501 program_status).
type ProgramStatusProvider interface {
	GetProgramStatus(ctx context.Context, sessionID, blockID string) ([]ProgramStatusRecord, error)
}
