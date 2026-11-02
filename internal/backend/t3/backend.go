package t3

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tortoise10h/buddy/internal/backend"
)

// Ensure compile-time interface conformance.
var _ backend.WorkerBackend = (*T3Backend)(nil)

// T3Backend implements backend.WorkerBackend for the T3 Code desktop app and server.
type T3Backend struct {
	mu            sync.RWMutex
	discovery     *ServerDiscovery
	httpClient    *HTTPClient
	mcpClient     *MCPClient
	tokenProvider *SessionTokenProvider
	initialized   bool
	serverURL     string
}

// Option configures T3Backend.
type Option func(*T3Backend)

// WithServerURL sets an explicit T3 server URL (e.g. "http://127.0.0.1:3773").
func WithServerURL(url string) Option {
	return func(b *T3Backend) {
		b.serverURL = strings.TrimRight(url, "/")
	}
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(client *HTTPClient) Option {
	return func(b *T3Backend) {
		b.httpClient = client
	}
}

// WithMCPClient overrides the MCP client.
func WithMCPClient(client *MCPClient) Option {
	return func(b *T3Backend) {
		b.mcpClient = client
	}
}

// WithDiscovery overrides the server discovery helper.
func WithDiscovery(d *ServerDiscovery) Option {
	return func(b *T3Backend) {
		b.discovery = d
	}
}

// WithTokenProvider overrides the session token provider.
func WithTokenProvider(tp *SessionTokenProvider) Option {
	return func(b *T3Backend) {
		b.tokenProvider = tp
	}
}

// NewT3Backend creates a new T3 Code WorkerBackend.
func NewT3Backend(opts ...Option) *T3Backend {
	b := &T3Backend{}
	for _, opt := range opts {
		opt(b)
	}

	if b.discovery == nil {
		b.discovery = NewServerDiscovery()
	}
	if b.tokenProvider == nil {
		b.tokenProvider = NewSessionTokenProvider()
	}
	if b.httpClient == nil {
		b.httpClient = NewHTTPClient(
			WithHTTPTokenProvider(b.tokenProvider),
		)
	}
	if b.mcpClient == nil {
		b.mcpClient = NewMCPClient()
	}

	if b.serverURL != "" {
		b.httpClient.SetBaseURL(b.serverURL)
		b.mcpClient.SetBaseURL(b.serverURL)
	}

	return b
}

// Name returns the backend identifier.
func (b *T3Backend) Name() string {
	return "t3code"
}

// Capabilities returns the feature capabilities of T3 Code as a WorkerBackend.
func (b *T3Backend) Capabilities() backend.WorkerCapabilities {
	return backend.WorkerCapabilities{
		ManagesWorktrees:  true,
		SupportsFocus:     false,
		SupportsDemoSplit: false,
		SupportsRawKeys:   false,
	}
}

// HTTPClient returns the underlying HTTP client.
func (b *T3Backend) HTTPClient() *HTTPClient {
	return b.httpClient
}

// MCPClient returns the underlying MCP client.
func (b *T3Backend) MCPClient() *MCPClient {
	return b.mcpClient
}

// EnsureInitialized ensures that server discovery has completed and the client
// is pointed to the running server origin.
func (b *T3Backend) EnsureInitialized(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.initialized && b.httpClient.BaseURL() != "" {
		return nil
	}

	if b.serverURL != "" {
		b.httpClient.SetBaseURL(b.serverURL)
		b.mcpClient.SetBaseURL(b.serverURL)
	} else {
		runtime, err := b.discovery.DiscoverRuntime()
		if err != nil {
			return err
		}
		b.httpClient.SetBaseURL(runtime.Origin)
		b.mcpClient.SetBaseURL(runtime.Origin)
	}

	// Discover protocol version asynchronously or silently; fallback is 2
	_, _ = b.httpClient.DiscoverProtocolVersion(ctx)
	b.initialized = true
	return nil
}

// EnsureProjectContainer verifies that a project exists in T3 Code and returns its ID.
// If the project does not exist, Phase 1 returns ErrNotImplementedPhase1 because
// project creation requires mutation tools (t3 project add or MCP).
func (b *T3Backend) EnsureProjectContainer(ctx context.Context, projectName, projectPath string) (string, error) {
	if err := b.EnsureInitialized(ctx); err != nil {
		return "", err
	}

	projects, err := b.ListLiveProjects(ctx)
	if err != nil {
		return "", err
	}

	absPath, _ := filepath.Abs(projectPath)

	for _, p := range projects {
		pAbs, _ := filepath.Abs(p.Path)
		if absPath != "" && pAbs == absPath {
			return p.ID, nil
		}
		if strings.EqualFold(p.Name, projectName) {
			return p.ID, nil
		}
	}

	return "", fmt.Errorf("%w: project '%s' (%s) not found in T3 Code (project creation requires mutation)",
		backend.ErrNotImplementedPhase1, projectName, projectPath)
}

// ListLiveProjects retrieves all active projects from GET /api/projects.
func (b *T3Backend) ListLiveProjects(ctx context.Context) ([]backend.ProjectContainer, error) {
	if err := b.EnsureInitialized(ctx); err != nil {
		return nil, err
	}

	snapshot, err := b.httpClient.GetProjects(ctx)
	if err != nil {
		return nil, err
	}

	res := make([]backend.ProjectContainer, 0, len(snapshot.Projects))
	for _, p := range snapshot.Projects {
		if p.DeletedAt != nil {
			continue
		}
		res = append(res, backend.ProjectContainer{
			ID:   p.ID,
			Name: p.Title,
			Path: p.WorkspaceRoot,
		})
	}
	return res, nil
}

// IsTaskAlive checks whether a thread exists and is neither deleted nor archived.
func (b *T3Backend) IsTaskAlive(ctx context.Context, handle backend.TaskHandle) (bool, error) {
	if handle.ThreadID == "" {
		return false, nil
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return false, err
	}

	shell, err := b.httpClient.GetShellSnapshot(ctx)
	if err != nil {
		// Fallback to thread snapshot
		detail, detailErr := b.httpClient.GetThreadSnapshot(ctx, handle.ThreadID)
		if detailErr != nil {
			if errors.Is(detailErr, ErrThreadNotFound) {
				return false, nil
			}
			return false, detailErr
		}
		return detail.Projection.Thread.DeletedAt == nil && detail.Projection.Thread.ArchivedAt == nil, nil
	}

	for _, t := range shell.Threads {
		if t.ID == handle.ThreadID {
			return t.DeletedAt == nil && t.ArchivedAt == nil, nil
		}
	}

	for _, t := range shell.ArchivedThreads {
		if t.ID == handle.ThreadID {
			return false, nil
		}
	}

	return false, nil
}

// GetAgentStatus returns the typed AgentStatusReport for a thread.
func (b *T3Backend) GetAgentStatus(ctx context.Context, handle backend.TaskHandle) (*backend.AgentStatusReport, error) {
	if handle.ThreadID == "" {
		return &backend.AgentStatusReport{
			Status:  "missing",
			Summary: "Empty thread ID in task handle",
			Settled: true,
		}, nil
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return &backend.AgentStatusReport{
			Status:  "missing",
			Summary: fmt.Sprintf("T3 server offline: %v", err),
			Settled: true,
		}, nil
	}

	shell, err := b.httpClient.GetShellSnapshot(ctx)
	if err == nil {
		for _, t := range shell.Threads {
			if t.ID == handle.ThreadID {
				return MapThreadStatus(&t), nil
			}
		}
		for _, t := range shell.ArchivedThreads {
			if t.ID == handle.ThreadID {
				return MapThreadStatus(&t), nil
			}
		}
	}

	// Fallback to individual thread snapshot
	detail, err := b.httpClient.GetThreadSnapshot(ctx, handle.ThreadID)
	if err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			return &backend.AgentStatusReport{
				Status:  "missing",
				Summary: "Thread not found in T3 Code",
				Settled: true,
			}, nil
		}
		return nil, err
	}

	return MapThreadStatus(&detail.Projection.Thread), nil
}

// CaptureOutput retrieves and formats the conversation transcript from GET /api/orchestration/threads/:id/transcript.
func (b *T3Backend) CaptureOutput(ctx context.Context, handle backend.TaskHandle, lines int) (string, error) {
	if handle.ThreadID == "" {
		return "", fmt.Errorf("empty thread ID in task handle")
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return "", err
	}

	transcript, err := b.httpClient.GetThreadTranscript(ctx, handle.ThreadID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Thread: %s (%s)\n", transcript.ThreadID, transcript.Title))
	sb.WriteString(strings.Repeat("-", 40) + "\n")

	for _, item := range transcript.Items {
		text := item.Item.ExtractText()
		if text == "" {
			continue
		}

		tag := "Message"
		switch item.Item.Type {
		case "user_message":
			tag = "User"
		case "assistant_message":
			tag = "Assistant"
		case "reasoning":
			tag = "Reasoning"
		case "proposed_plan":
			tag = "Plan"
		case "tool_call":
			tag = "Tool"
		case "notification":
			tag = "Notification"
		}

		sb.WriteString(fmt.Sprintf("[%s]: %s\n", tag, strings.TrimSpace(text)))
	}

	fullOutput := strings.TrimRight(sb.String(), "\n")
	if lines <= 0 {
		return fullOutput, nil
	}

	allLines := strings.Split(fullOutput, "\n")
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return strings.Join(allLines, "\n"), nil
}

// -------------------------------------------------------------------------
// Mutation-dependent methods (deferred to Phase 2; return ErrNotImplementedPhase1)
// -------------------------------------------------------------------------

// DispatchTask launches a task on T3 Code via MCP t3_thread_launch in Phase 2.
func (b *T3Backend) DispatchTask(ctx context.Context, spec backend.DispatchSpec) (*backend.DispatchResult, error) {
	return nil, fmt.Errorf("DispatchTask: %w", backend.ErrNotImplementedPhase1)
}

// CloseTask interrupts and archives a thread via MCP in Phase 2.
func (b *T3Backend) CloseTask(ctx context.Context, handle backend.TaskHandle) error {
	return fmt.Errorf("CloseTask: %w", backend.ErrNotImplementedPhase1)
}

// SendMessage delivers messages or answers via MCP t3_pending_request / t3_thread_send in Phase 2.
func (b *T3Backend) SendMessage(ctx context.Context, handle backend.TaskHandle, text string) error {
	return fmt.Errorf("SendMessage: %w", backend.ErrNotImplementedPhase1)
}

// InterruptTask stops a running thread via MCP t3_thread_interrupt in Phase 2.
func (b *T3Backend) InterruptTask(ctx context.Context, handle backend.TaskHandle) error {
	return fmt.Errorf("InterruptTask: %w", backend.ErrNotImplementedPhase1)
}
