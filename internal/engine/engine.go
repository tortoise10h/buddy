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
	SessionID string         `json:"session_id"`
	WindowID  string         `json:"window_id"`
	BlockID   string         `json:"block_id"`
	PickedUp  bool           `json:"picked_up"`
	Extra     map[string]any `json:"extra,omitempty"`
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
