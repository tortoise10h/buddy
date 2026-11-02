package t3

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Typed errors for T3 Code integration.
var (
	// ErrServerNotRunning indicates the T3 Code server is not running or its
	// runtime state file cannot be found.
	ErrServerNotRunning = errors.New("t3 server is not running")

	// ErrAuthInvalid indicates HTTP 401 Unauthorized from the T3 API.
	ErrAuthInvalid = errors.New("t3 authentication invalid (401)")

	// ErrMcpAuthRequired indicates that an MCP operation requires a valid
	// OAuth session token (HTTP 401 invalid_mcp_credential).
	ErrMcpAuthRequired = errors.New("t3 mcp oauth authentication required")

	// ErrProjectNotFound indicates the requested project does not exist in T3 Code.
	ErrProjectNotFound = errors.New("project not found in t3")

	// ErrThreadNotFound indicates the requested thread does not exist in T3 Code.
	ErrThreadNotFound = errors.New("thread not found in t3")
)

// ServerRuntime models the contents of ~/.t3/server-runtime.json or
// ~/.t3/userdata/server-runtime.json written by the T3 server upon startup.
type ServerRuntime struct {
	Version       int    `json:"version"`
	PID           int    `json:"pid"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Origin        string `json:"origin"`
	StartedAt     string `json:"startedAt"`
	ServerVersion string `json:"serverVersion"`
	OwnerID       string `json:"ownerId,omitempty"`
}

// EnvironmentDescriptor represents the payload returned by GET /.well-known/t3/environment.
type EnvironmentDescriptor struct {
	EnvironmentID                string         `json:"environmentId"`
	Label                        string         `json:"label"`
	ServerVersion                string         `json:"serverVersion"`
	OrchestrationProtocolVersion int            `json:"orchestrationProtocolVersion"`
	Capabilities                 map[string]any `json:"capabilities,omitempty"`
}

// T3Project represents a project returned by GET /api/projects.
type T3Project struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	WorkspaceRoot string  `json:"workspaceRoot"`
	DeletedAt     *string `json:"deletedAt,omitempty"`
	ArchivedAt    *string `json:"archivedAt,omitempty"`
	CreatedAt     string  `json:"createdAt,omitempty"`
	UpdatedAt     string  `json:"updatedAt,omitempty"`
}

// ProjectSnapshot represents the response wrapper for GET /api/projects.
type ProjectSnapshot struct {
	Projects  []T3Project `json:"projects"`
	UpdatedAt string      `json:"updatedAt"`
}

// PendingRuntimeRequestSummary models a pending interactive request on a thread.
type PendingRuntimeRequestSummary struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // e.g. "user_input"
	Blocking *bool  `json:"blocking,omitempty"`
}

// LatestVisibleMessageSummary represents a preview of the latest visible message.
type LatestVisibleMessageSummary struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
}

// ThreadShell models a thread projection in GET /api/orchestration/shell.
type ThreadShell struct {
	ID                    string                        `json:"id"`
	ProjectID             string                        `json:"projectId"`
	Title                 string                        `json:"title"`
	ProviderInstanceID    string                        `json:"providerInstanceId"`
	RuntimeMode           string                        `json:"runtimeMode,omitempty"`
	Branch                *string                       `json:"branch,omitempty"`
	WorktreePath          *string                       `json:"worktreePath,omitempty"`
	Status                string                        `json:"status"` // "preparing", "queued", "starting", "running", "waiting", "completed", "interrupted", "failed", "cancelled", "rolled_back", "idle"
	LatestRunID           *string                       `json:"latestRunId,omitempty"`
	ActiveRunID           *string                       `json:"activeRunId,omitempty"`
	LastError             *string                       `json:"lastError,omitempty"`
	PendingRuntimeRequest *PendingRuntimeRequestSummary `json:"pendingRuntimeRequest,omitempty"`
	PendingRequestCount   int                           `json:"pendingRequestCount,omitempty"`
	LatestVisibleMessage  *LatestVisibleMessageSummary  `json:"latestVisibleMessage,omitempty"`
	SettledAt             *string                       `json:"settledAt,omitempty"`
	SettledOverride       *string                       `json:"settledOverride,omitempty"`
	DeletedAt             *string                       `json:"deletedAt,omitempty"`
	ArchivedAt            *string                       `json:"archivedAt,omitempty"`
	CreatedAt             string                        `json:"createdAt,omitempty"`
	UpdatedAt             string                        `json:"updatedAt,omitempty"`
}

// EffectivePendingRequestCount returns the calculated pending request count,
// accounting for both explicit count fields and pendingRuntimeRequest presence.
func (t *ThreadShell) EffectivePendingRequestCount() int {
	if t.PendingRequestCount > 0 {
		return t.PendingRequestCount
	}
	if t.PendingRuntimeRequest != nil {
		return 1
	}
	return 0
}

// IsSettled returns true if the thread is settled.
func (t *ThreadShell) IsSettled() bool {
	if t.SettledOverride != nil {
		return *t.SettledOverride == "settled"
	}
	return t.SettledAt != nil
}

// OrchestrationShellSnapshot represents the response of GET /api/orchestration/shell.
type OrchestrationShellSnapshot struct {
	SchemaVersion    int           `json:"schemaVersion"`
	SnapshotSequence int           `json:"snapshotSequence"`
	Threads          []ThreadShell `json:"threads"`
	ArchivedThreads  []ThreadShell `json:"archivedThreads,omitempty"`
}

// ThreadDetailSnapshot represents the response of GET /api/orchestration/threads/:id.
type ThreadDetailSnapshot struct {
	SnapshotSequence int              `json:"snapshotSequence"`
	Projection       ThreadProjection `json:"projection"`
}

// ThreadProjection holds the full state graph of a thread.
type ThreadProjection struct {
	Thread          ThreadShell          `json:"thread"`
	RuntimeRequests []RuntimeRequest     `json:"runtimeRequests,omitempty"`
	Messages        []ThreadMessage      `json:"messages,omitempty"`
	VisibleTurnItems []TranscriptTurnItem `json:"visibleTurnItems,omitempty"`
	UpdatedAt       string               `json:"updatedAt"`
}

// RuntimeRequest represents a pending or completed runtime request.
type RuntimeRequest struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}

// ThreadMessage represents a conversational message in the thread projection.
type ThreadMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
}

// ThreadTranscript represents the response of GET /api/orchestration/threads/:id/transcript.
type ThreadTranscript struct {
	ThreadID  string               `json:"threadId"`
	Title     string               `json:"title"`
	UpdatedAt string               `json:"updatedAt"`
	Items     []TranscriptTurnItem `json:"items"`
}

// TranscriptTurnItem models a single projected item in a thread transcript.
type TranscriptTurnItem struct {
	Position       int             `json:"position"`
	Visibility     string          `json:"visibility"`
	SourceThreadID string          `json:"sourceThreadId"`
	SourceItemID   string          `json:"sourceItemId"`
	Item           TurnItemPayload `json:"item"`
}

// TurnItemPayload models the variable item in a transcript (user message, assistant, tool call, etc.).
type TurnItemPayload struct {
	Type      string `json:"type"` // "user_message", "assistant_message", "reasoning", "proposed_plan", "tool_call", "notification", etc.
	Text      string `json:"text,omitempty"`
	MessageID string `json:"messageId,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`
	Markdown  string `json:"markdown,omitempty"`
}

// ExtractText returns formatted text content from a TurnItemPayload.
func (p *TurnItemPayload) ExtractText() string {
	if p.Text != "" {
		return p.Text
	}
	if p.Markdown != "" {
		return p.Markdown
	}
	return ""
}

// McpRequest represents a JSON-RPC 2.0 request sent to POST /mcp.
type McpRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// McpResponse represents a JSON-RPC 2.0 response from POST /mcp.
type McpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *McpError       `json:"error,omitempty"`
}

// McpError represents a JSON-RPC 2.0 error object.
type McpError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *McpError) Error() string {
	return fmt.Sprintf("mcp error (code %d): %s", e.Code, e.Message)
}

// McpToolResult models the standard result of a tool call in MCP.
type McpToolResult struct {
	Content []McpContent `json:"content,omitempty"`
	IsError bool         `json:"isError,omitempty"`
}

// McpContent models a content item in an MCP tool response.
type McpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
