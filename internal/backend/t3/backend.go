package t3

import (
	"context"
	"encoding/json"
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
	mu               sync.RWMutex
	discovery        *ServerDiscovery
	httpClient       *HTTPClient
	mcpClient        *MCPClient
	tokenProvider    *SessionTokenProvider
	mcpTokenProvider MCPTokenProvider
	mcpOAuthPath     string
	binPath          string
	commandRunner    CommandRunner
	initialized      bool
	serverURL        string
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

// WithBackendMCPTokenProvider overrides the MCP token provider.
func WithBackendMCPTokenProvider(p MCPTokenProvider) Option {
	return func(b *T3Backend) {
		b.mcpTokenProvider = p
	}
}

// WithMCPOAuthPath sets a custom path for the t3_mcp_oauth.json file.
func WithMCPOAuthPath(path string) Option {
	return func(b *T3Backend) {
		b.mcpOAuthPath = path
	}
}

// WithBackendBinPath sets the binary path for the t3 executable.
func WithBackendBinPath(path string) Option {
	return func(b *T3Backend) {
		b.binPath = path
	}
}

// WithBackendCommandRunner sets a custom command runner for CLI executions.
func WithBackendCommandRunner(runner CommandRunner) Option {
	return func(b *T3Backend) {
		b.commandRunner = runner
	}
}

// NewT3Backend creates a new T3 Code WorkerBackend.
func NewT3Backend(opts ...Option) *T3Backend {
	b := &T3Backend{
		commandRunner: DefaultCommandRunner,
	}
	for _, opt := range opts {
		opt(b)
	}

	if b.discovery == nil {
		b.discovery = NewServerDiscovery()
	}
	if b.tokenProvider == nil {
		b.tokenProvider = NewSessionTokenProvider(
			WithBinPath(b.binPath),
			WithCommandRunner(b.commandRunner),
		)
	}
	if b.httpClient == nil {
		b.httpClient = NewHTTPClient(
			WithHTTPTokenProvider(b.tokenProvider),
		)
	}
	if b.mcpTokenProvider == nil {
		b.mcpTokenProvider = NewFileMCPTokenProvider(b.mcpOAuthPath)
	}
	if b.mcpClient == nil {
		b.mcpClient = NewMCPClient(
			WithMCPTokenProvider(b.mcpTokenProvider),
		)
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
// Per Decision 3, if the project is not registered, it auto-registers via MCP tool
// t3_project_create (preferred), falling back to 't3 project add' only when MCP auth
// is unavailable. Never fails with "project not registered".
func (b *T3Backend) EnsureProjectContainer(ctx context.Context, projectName, projectPath string) (string, error) {
	if err := b.EnsureInitialized(ctx); err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(projectPath)
	if err != nil || absPath == "" {
		absPath = projectPath
	}

	// 1. Check existing live projects
	projects, err := b.ListLiveProjects(ctx)
	if err == nil {
		for _, p := range projects {
			pAbs, _ := filepath.Abs(p.Path)
			if absPath != "" && pAbs == absPath {
				return p.ID, nil
			}
			if strings.EqualFold(p.Name, projectName) {
				return p.ID, nil
			}
		}
	}

	// 2. Unregistered: auto-register project (Decision 3)
	// Try MCP tool t3_project_create first
	proj, mcpErr := b.mcpClient.CreateProject(ctx, projectName, absPath)
	if mcpErr == nil && proj != nil && proj.ID != "" {
		return proj.ID, nil
	}

	// If MCP creation failed, fall back to CLI 't3 project add'
	cliID, cliErr := b.addProjectViaCLI(ctx, projectName, absPath)
	if cliErr == nil && cliID != "" {
		return cliID, nil
	}

	if mcpErr != nil {
		return "", fmt.Errorf("failed to auto-register project '%s' (mcp error: %v, cli fallback error: %v)", projectName, mcpErr, cliErr)
	}
	return "", fmt.Errorf("failed to auto-register project '%s': %w", projectName, cliErr)
}

func (b *T3Backend) addProjectViaCLI(ctx context.Context, projectName, absPath string) (string, error) {
	runner := b.commandRunner
	if runner == nil {
		runner = DefaultCommandRunner
	}
	bin := b.binPath
	if bin == "" {
		resolved, err := FindT3Binary("")
		if err != nil {
			return "", err
		}
		bin = resolved
	}

	out, err := runner(ctx, bin, "project", "add", absPath, "--title", projectName, "--json")
	if err != nil {
		return "", fmt.Errorf("cli 't3 project add' failed: %w", err)
	}

	var proj T3Project
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &proj); err != nil {
		return "", fmt.Errorf("failed to parse 't3 project add' json: %w", err)
	}
	if proj.ID == "" {
		return "", fmt.Errorf("empty project id returned from 't3 project add'")
	}
	return proj.ID, nil
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

// DispatchTask launches a task on T3 Code via MCP t3_thread_launch.
// Automatically auto-registers the project if unregistered (Decision 3),
// creates an isolated git worktree if spec.UseWorktree is set, and launches
// with runtimeMode "full-access".
func (b *T3Backend) DispatchTask(ctx context.Context, spec backend.DispatchSpec) (*backend.DispatchResult, error) {
	if err := b.EnsureInitialized(ctx); err != nil {
		return nil, err
	}

	projectID, err := b.EnsureProjectContainer(ctx, spec.ProjectName, spec.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("DispatchTask: %w", err)
	}

	// Prepare workspaceStrategy
	var wsStrategy map[string]any
	if spec.UseWorktree {
		baseRef := spec.BaseRef
		if baseRef == "" {
			baseRef = "HEAD"
		}
		branch := spec.Branch
		if branch == "" {
			branch = spec.TaskSlug
		}
		wsStrategy = map[string]any{
			"type":    "worktree",
			"baseRef": baseRef,
			"branch":  branch,
		}
	} else {
		wsStrategy = map[string]any{
			"type": "root",
		}
	}

	// Prepare modelSelection
	var modelSelection map[string]any
	if spec.AgentSpec != "" {
		if strings.Contains(spec.AgentSpec, "/") {
			parts := strings.SplitN(spec.AgentSpec, "/", 2)
			modelSelection = map[string]any{
				"instanceId": parts[0],
				"model":      parts[1],
			}
		} else {
			modelSelection = map[string]any{
				"instanceId": spec.AgentSpec,
			}
		}
	}
	if spec.Extra != nil && spec.Extra["modelSelection"] != nil {
		if ms, ok := spec.Extra["modelSelection"].(map[string]any); ok {
			modelSelection = ms
		}
	}

	title := spec.TaskSlug
	if title == "" {
		title = spec.ProjectName + "-task"
	}

	params := LaunchThreadParams{
		ProjectID:         projectID,
		Title:             title,
		Message:           spec.Prompt,
		RuntimeMode:       "full-access",
		ModelSelection:    modelSelection,
		WorkspaceStrategy: wsStrategy,
	}

	launchRes, err := b.mcpClient.LaunchThread(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("DispatchTask: %w", err)
	}

	runID := ""
	if launchRes.RunID != nil {
		runID = *launchRes.RunID
	}
	status := ""
	if launchRes.Status != nil {
		status = *launchRes.Status
	}

	handle := backend.TaskHandle{
		Backend:     "t3code",
		ProjectID:   projectID,
		TaskSlug:    spec.TaskSlug,
		ThreadID:    launchRes.ThreadID,
		ActiveRunID: runID,
		LatestRunID: runID,
		Branch:      spec.Branch,
	}

	return &backend.DispatchResult{
		TaskHandle: handle,
		PickedUp:   runID != "",
		Preview:    fmt.Sprintf("T3 thread %s (run %s, status %s)", launchRes.ThreadID, runID, status),
	}, nil
}

// CloseTask interrupts and archives a thread via MCP tools.
func (b *T3Backend) CloseTask(ctx context.Context, handle backend.TaskHandle) error {
	if handle.ThreadID == "" {
		return fmt.Errorf("empty thread ID in task handle")
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return err
	}

	// Best-effort interrupt
	_ = b.mcpClient.InterruptThread(ctx, handle.ThreadID)

	// Archive thread
	return b.mcpClient.ArchiveThread(ctx, handle.ThreadID)
}

// SendMessage delivers messages or answers via MCP t3_pending_request or t3_thread_send.
// Per Decision 2, multi-question answers are accepted as an ordered or keyed list
// rather than bare string concatenation.
func (b *T3Backend) SendMessage(ctx context.Context, handle backend.TaskHandle, msg backend.MessageInput) error {
	if handle.ThreadID == "" {
		return fmt.Errorf("empty thread ID in task handle")
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return err
	}

	// Detect if there are pending interactive requests on the thread
	reqIDs, err := b.mcpClient.ListPendingRequests(ctx, handle.ThreadID)
	if err != nil {
		if errors.Is(err, ErrMcpAuthRequired) {
			return err
		}
		// If listing pending requests fails for other reason, try normal send
		reqIDs = nil
	}

	if len(reqIDs) > 0 {
		reqID := reqIDs[0]
		detail, err := b.mcpClient.ReadPendingRequest(ctx, handle.ThreadID, reqID)
		if err != nil {
			return fmt.Errorf("failed to read pending question: %w", err)
		}

		answersMap := make(map[string]any)

		switch {
		case len(msg.KeyedAnswers) > 0:
			answersMap = msg.KeyedAnswers

		case len(msg.Answers) > 0:
			// Map ordered answers to questions
			for i, ans := range msg.Answers {
				if i < len(detail.Questions) {
					answersMap[detail.Questions[i].ID] = ans
				} else {
					answersMap[fmt.Sprintf("q%d", i+1)] = ans
				}
			}

		case msg.Text != "":
			if len(detail.Questions) == 1 {
				answersMap[detail.Questions[0].ID] = msg.Text
			} else if len(detail.Questions) > 1 {
				return fmt.Errorf("thread has %d pending questions; repeatable answers required (cannot use single message)", len(detail.Questions))
			} else {
				answersMap["answer"] = msg.Text
			}

		default:
			return fmt.Errorf("cannot send empty message or answers")
		}

		return b.mcpClient.RespondPendingRequest(ctx, handle.ThreadID, reqID, answersMap)
	}

	// Normal conversational turn (no pending requests)
	text := msg.Text
	if text == "" && len(msg.Answers) > 0 {
		text = strings.Join(msg.Answers, "\n")
	}
	if text == "" {
		return fmt.Errorf("cannot send empty message to thread")
	}

	_, err = b.mcpClient.SendThread(ctx, handle.ThreadID, text, "auto")
	return err
}

// InterruptTask stops a running thread turn via MCP t3_thread_interrupt (ctrl+c semantics).
func (b *T3Backend) InterruptTask(ctx context.Context, handle backend.TaskHandle) error {
	if handle.ThreadID == "" {
		return fmt.Errorf("empty thread ID in task handle")
	}

	if err := b.EnsureInitialized(ctx); err != nil {
		return err
	}

	return b.mcpClient.InterruptThread(ctx, handle.ThreadID)
}
