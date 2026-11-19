package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrBinaryNotFound is returned when the opencode CLI binary cannot be located.
	ErrBinaryNotFound = errors.New("opencode binary not found")

	// ErrServiceNotRunning is returned when no local OpenCode server can be discovered.
	ErrServiceNotRunning = errors.New("opencode background service is not running")

	// ErrUnauthorized is returned when the OpenCode server rejects credentials (HTTP 401).
	ErrUnauthorized = errors.New("opencode server authentication required")

	// ErrSessionNotFound is returned when a session ID does not exist on the server (HTTP 404).
	ErrSessionNotFound = errors.New("opencode session not found")
)

// APIError captures structured error payloads returned by the OpenCode v2 HTTP API.
type APIError struct {
	StatusCode int
	Tag        string `json:"_tag,omitempty"`
	Message    string `json:"message,omitempty"`
	RawBody    string `json:"-"`
}

func (e *APIError) Error() string {
	if e.Message != "" {
		if e.Tag != "" {
			return fmt.Sprintf("opencode api HTTP %d (%s): %s", e.StatusCode, e.Tag, e.Message)
		}
		return fmt.Sprintf("opencode api HTTP %d: %s", e.StatusCode, e.Message)
	}
	if e.RawBody != "" {
		return fmt.Sprintf("opencode api HTTP %d: %s", e.StatusCode, strings.TrimSpace(e.RawBody))
	}
	return fmt.Sprintf("opencode api HTTP %d", e.StatusCode)
}

func (e *APIError) Is(target error) bool {
	if target == ErrUnauthorized && e.StatusCode == 401 {
		return true
	}
	if target == ErrSessionNotFound && e.StatusCode == 404 {
		return true
	}
	return false
}

// ServerCapabilities describes optional server features returned by GET /api/info.
type ServerCapabilities struct {
	PersistentPTY bool `json:"persistentPty,omitempty"`
}

// ServerInfo represents the response from GET /api/info.
type ServerInfo struct {
	Version      string             `json:"version"`
	PID          int                `json:"pid,omitempty"`
	Hostname     string             `json:"hostname,omitempty"`
	URLs         []string           `json:"urls,omitempty"`
	Capabilities ServerCapabilities `json:"capabilities,omitempty"`
}

// Project represents an entry returned by GET /api/project.
type Project struct {
	ID        string   `json:"id"`
	Canonical string   `json:"canonical"`
	VCS       string   `json:"vcs,omitempty"`
	Sandboxes []string `json:"sandboxes,omitempty"`
}

// ModelRef identifies a provider/model pair in OpenCode v2.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant,omitempty"`
}

// SessionLocation scopes a session to a filesystem directory.
type SessionLocation struct {
	Directory string `json:"directory"`
}

// SessionTime tracks lifecycle timestamps (Unix milliseconds) for a session.
type SessionTime struct {
	Created  int64 `json:"created"`
	Updated  int64 `json:"updated"`
	Busy     int64 `json:"busy,omitempty"`
	Idle     int64 `json:"idle,omitempty"`
	Viewed   int64 `json:"viewed,omitempty"`
	Archived int64 `json:"archived,omitempty"`
}

// SessionInfo represents Session.Info from GET /api/session and GET /api/session/{id}.
type SessionInfo struct {
	ID        string           `json:"id"`
	ParentID  string           `json:"parentID,omitempty"`
	ProjectID string           `json:"projectID,omitempty"`
	Agent     string           `json:"agent,omitempty"`
	Model     *ModelRef        `json:"model,omitempty"`
	Outcome   string           `json:"outcome,omitempty"` // "succeeded", "failed", "interrupted"
	Time      SessionTime      `json:"time"`
	Title     string           `json:"title,omitempty"`
	Location  *SessionLocation `json:"location,omitempty"`
}

// SessionActive represents an entry in GET /api/session/active.
type SessionActive struct {
	Type string `json:"type"` // "running"
}

// CreateSessionRequest is the JSON payload for POST /api/session.
type CreateSessionRequest struct {
	Title    string           `json:"title,omitempty"`
	Agent    string           `json:"agent,omitempty"`
	Model    *ModelRef        `json:"model,omitempty"`
	Location *SessionLocation `json:"location,omitempty"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

// PromptRequest is the JSON payload for POST /api/session/{sessionID}/prompt.
type PromptRequest struct {
	Text     string         `json:"text"`
	Model    *ModelRef      `json:"model,omitempty"`
	Delivery string         `json:"delivery,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ListSessionsOptions specifies query parameters for GET /api/session.
type ListSessionsOptions struct {
	Directory string
	Project   string
	ParentID  string
	Archived  *bool
	Limit     int
	Order     string
}

// MessagePart represents a single content block inside an assistant message.
type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
}

// MessageInfo represents an item returned by GET /api/session/{sessionID}/message.
type MessageInfo struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"` // "user", "assistant", "idle", "system", "synthetic"
	Text        string          `json:"text,omitempty"`
	Description string          `json:"description,omitempty"`
	Content     []MessagePart   `json:"content,omitempty"`
	Outcome     string          `json:"outcome,omitempty"`
	Agent       string          `json:"agent,omitempty"`
	Error       json.RawMessage `json:"error,omitempty"`
	Time        struct {
		Created int64 `json:"created,omitempty"`
	} `json:"time,omitempty"`
}

// ExtractText returns the human-readable text body of a message across user/assistant/system types.
func (m MessageInfo) ExtractText() string {
	if s := strings.TrimSpace(m.Text); s != "" {
		return s
	}
	if len(m.Content) > 0 {
		var parts []string
		for _, p := range m.Content {
			if p.Type == "text" {
				if t := strings.TrimSpace(p.Text); t != "" {
					parts = append(parts, t)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if s := strings.TrimSpace(m.Description); s != "" {
		return s
	}
	return ""
}

// ExtractErrorMessage returns a concise error string if the message carries an error payload.
func (m MessageInfo) ExtractErrorMessage() string {
	if len(m.Error) == 0 || string(m.Error) == "null" {
		return ""
	}
	var asStr string
	if err := json.Unmarshal(m.Error, &asStr); err == nil && strings.TrimSpace(asStr) != "" {
		return strings.TrimSpace(asStr)
	}
	var asObj struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Tag     string `json:"_tag"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(m.Error, &asObj); err == nil {
		if strings.TrimSpace(asObj.Message) != "" {
			return strings.TrimSpace(asObj.Message)
		}
		if strings.TrimSpace(asObj.Data.Message) != "" {
			return strings.TrimSpace(asObj.Data.Message)
		}
		if strings.TrimSpace(asObj.Name) != "" {
			return strings.TrimSpace(asObj.Name)
		}
		if strings.TrimSpace(asObj.Tag) != "" {
			return strings.TrimSpace(asObj.Tag)
		}
	}
	return strings.TrimSpace(string(m.Error))
}

// PermissionRequest represents a pending permission request from GET /api/session/{id}/permission.
type PermissionRequest struct {
	ID          string `json:"id"`
	Action      string `json:"action,omitempty"`
	Resource    string `json:"resource,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Message     string `json:"message,omitempty"`
}

// Summary returns a concise description of the pending permission request.
func (p PermissionRequest) Summary() string {
	for _, s := range []string{p.Title, p.Message, p.Description} {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			return trimmed
		}
	}
	if p.Action != "" && p.Resource != "" {
		return fmt.Sprintf("Permission requested: %s (%s)", p.Action, p.Resource)
	}
	if p.Action != "" {
		return fmt.Sprintf("Permission requested: %s", p.Action)
	}
	return "Waiting for permission approval"
}

// FormRequest represents a pending interactive form from GET /api/session/{id}/form.
type FormRequest struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Message     string `json:"message,omitempty"`
}

// Summary returns a concise description of the pending form.
func (f FormRequest) Summary() string {
	for _, s := range []string{f.Title, f.Message, f.Description} {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			return trimmed
		}
	}
	return "Waiting for form input"
}

// SSEEvent represents a single Server-Sent Event frame from GET /api/event.
type SSEEvent struct {
	Event string
	Data  json.RawMessage
}
