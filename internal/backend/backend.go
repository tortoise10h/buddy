package backend

import (
	"context"
	"errors"
)

// ErrNotImplementedPhase1 indicates that an operation is a mutation or feature
// deferred to Phase 2 of the T3 Code integration.
var ErrNotImplementedPhase1 = errors.New("not implemented in phase 1")

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
//
// TerminalBackend vs WorkerBackend split:
//
// Buddy splits host environment interactions along two orthogonal axes:
//
// 1. Terminal Multiplexing (terminal_backend: "rex" | "herdr"):
//    Multiplexers host Buddy's interactive Control Tower TUI (bin/buddy),
//    manage tabs/windows/panes, send raw keystrokes/PTY input, and split
//    panes to run dev servers during demos (buddy-ctl demo --cmd).
//    TerminalBackend defines these raw terminal multiplexer operations.
//
// 2. Worker Task Execution (worker_backend: "rex" | "herdr" | "t3code"):
//    Worker execution hosts autonomous coding agents. While terminal multiplexers
//    can run worker agents by spawning CLI processes into PTY panes, API-driven
//    platforms like T3 Code run agents as server-managed threads with native
//    git worktrees, structured prompt dispatch, typed run lifecycles, and
//    deterministic state reporting.
//    WorkerBackend defines this high-level task lifecycle interface.
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

// WorkerCapabilities declares the optional or platform-specific feature flags
// supported by a WorkerBackend implementation.
type WorkerCapabilities struct {
	// ManagesWorktrees indicates whether the backend natively creates and manages
	// isolated git worktrees for tasks (e.g., T3 Code) rather than requiring
	// buddy-ctl to run 'git worktree add' directly.
	ManagesWorktrees bool `json:"manages_worktrees"`

	// SupportsFocus indicates whether the backend can focus the worker's UI tab/window.
	// True for terminal muxes (Rex/Herdr); false for T3 Code where thread-level focus is descoped.
	SupportsFocus bool `json:"supports_focus"`

	// SupportsDemoSplit indicates whether the backend can split a dev server pane
	// alongside the worker task. True for terminal muxes; false for T3 Code
	// (which delegates demo --cmd to the host terminal_backend).
	SupportsDemoSplit bool `json:"supports_demo_split"`

	// SupportsRawKeys indicates whether the backend accepts arbitrary terminal keystrokes
	// (e.g. enter, arrows) vs. structured message/interrupt commands.
	SupportsRawKeys bool `json:"supports_raw_keys"`
}

// ProjectContainer represents a project workspace registered on a backend.
type ProjectContainer struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Extra map[string]any `json:"extra,omitempty"`
}

// DispatchSpec defines all parameters required to launch an agent task on a WorkerBackend.
type DispatchSpec struct {
	ProjectName string         `json:"project_name"`
	ProjectPath string         `json:"project_path"`
	TaskSlug    string         `json:"task_slug"`
	Prompt      string         `json:"prompt"`
	AgentSpec   string         `json:"agent_spec,omitempty"`
	UseWorktree bool           `json:"use_worktree"`
	BaseRef     string         `json:"base_ref,omitempty"`
	Branch      string         `json:"branch,omitempty"`
	AgentCmd    []string       `json:"agent_cmd,omitempty"` // For terminal multiplexer backends (Rex/Herdr)
	Extra       map[string]any `json:"extra,omitempty"`
}

// TaskHandle uniquely identifies a task running on a backend across sessions.
type TaskHandle struct {
	Backend            string `json:"backend"`
	ProjectID          string `json:"project_id,omitempty"`
	TaskSlug           string `json:"task_slug,omitempty"`
	// Multiplexer backend fields (Rex / Herdr)
	SessionID          string `json:"session_id,omitempty"`
	WindowID           string `json:"window_id,omitempty"`
	BlockID            string `json:"block_id,omitempty"`
	// T3 Code backend fields
	ThreadID           string `json:"thread_id,omitempty"`
	ActiveRunID        string `json:"active_run_id,omitempty"`
	LatestRunID        string `json:"latest_run_id,omitempty"`
	ProviderInstanceID string `json:"provider_instance_id,omitempty"`
	Model              string `json:"model,omitempty"`
	WorktreePath       string `json:"worktree_path,omitempty"`
	Branch             string `json:"branch,omitempty"`
}

// DispatchResult represents the immediate outcome of a task dispatch operation.
type DispatchResult struct {
	TaskHandle TaskHandle     `json:"task_handle"`
	PickedUp   bool           `json:"picked_up"`
	Preview    string         `json:"preview,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// AgentStatusReport represents the current execution and lifecycle state of a worker task.
type AgentStatusReport struct {
	Status              string         `json:"status"` // "starting", "running", "waiting_for_input", "ready_for_demo", "error", "stopped", "missing"
	Summary             string         `json:"summary,omitempty"`
	PendingRequestCount int            `json:"pending_request_count"`
	Settled             bool           `json:"settled"`
	ActiveRunID         string         `json:"active_run_id,omitempty"`
	LatestRunID         string         `json:"latest_run_id,omitempty"`
	ErrorDetails        string         `json:"error_details,omitempty"`
	RawState            map[string]any `json:"raw_state,omitempty"`
}

// WorkerBackend defines the task execution abstraction for buddy-ctl.
//
// Implementations include:
//   - RexBackend / HerdrBackend: Terminal multiplexers running CLI agents in PTY panes.
//   - T3Backend: T3 Code server running agent threads via localhost HTTP & MCP APIs.
type WorkerBackend interface {
	Name() string
	Capabilities() WorkerCapabilities

	// Project management
	EnsureProjectContainer(ctx context.Context, projectName, projectPath string) (string, error)
	ListLiveProjects(ctx context.Context) ([]ProjectContainer, error)

	// Task lifecycle
	DispatchTask(ctx context.Context, spec DispatchSpec) (*DispatchResult, error)
	IsTaskAlive(ctx context.Context, handle TaskHandle) (bool, error)
	CloseTask(ctx context.Context, handle TaskHandle) error

	// Task interaction
	SendMessage(ctx context.Context, handle TaskHandle, text string) error
	InterruptTask(ctx context.Context, handle TaskHandle) error
	CaptureOutput(ctx context.Context, handle TaskHandle, lines int) (string, error)
	GetAgentStatus(ctx context.Context, handle TaskHandle) (*AgentStatusReport, error)
}

