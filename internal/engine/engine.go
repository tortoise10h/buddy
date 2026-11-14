package engine

import (
	"context"

	"github.com/tortoise10h/buddy/internal/backend"
)

// DispatchRequest contains the parameters needed to start and initialize a task.
type DispatchRequest struct {
	ProjectName string
	ProjectPath string
	TaskSlug    string
	Prompt      string
	AgentCmd    []string
}

// DispatchResult represents the outcome of a task dispatch operation.
type DispatchResult struct {
	SessionID    string         `json:"session_id"`
	WindowID     string         `json:"window_id"`
	BlockID      string         `json:"block_id"`
	PickedUp     bool           `json:"picked_up"`
	PickupReason string         `json:"pickup_reason,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// Engine defines the high-level task lifecycle interface that CLI commands interact with.
// CLI commands never care whether a target is a terminal multiplexer (Rex, WezTerm)
// or an API-based thread engine (T3); the Engine handles the specific lifecycle flow.
type Engine interface {
	Name() string
	Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error)
	Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error)
	Send(ctx context.Context, sessionID, blockID string, input backend.Input) error
	Close(ctx context.Context, sessionID, windowID string) error
	Focus(ctx context.Context, sessionID, windowID string) error
	OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error)
}

// TaskStatusProbe represents a structured live status verdict for a tracked task.
// Status uses Buddy's task vocabulary: "running", "planning", "waiting_for_input",
// "ready_for_demo", "error", or "unknown".
type TaskStatusProbe struct {
	Status   string `json:"status"`
	Working  bool   `json:"working"`
	Event    string `json:"event,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}

// TaskStatusProvider is an optional capability interface implemented by engines
// that can query structured live task status directly (e.g. T3 thread API)
// instead of relying on terminal pane scraping.
//
// Implementations must never fail destructively: if live status cannot be
// determined (e.g. daemon unreachable or unknown thread), ProbeStatus should
// return an error so callers can treat the state as "unknown" rather than
// fabricating a completion such as "ready_for_demo".
type TaskStatusProvider interface {
	ProbeStatus(ctx context.Context, sessionID, blockID string) (TaskStatusProbe, error)
}
