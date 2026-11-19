package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/opencode"
)

// OpenCodeEngineOptions configures an OpenCodeEngine.
type OpenCodeEngineOptions struct {
	DefaultAgent string
	DefaultModel string // optional "<provider>/<model>" or "<provider>/<model>#<variant>"
	FocusDesktop bool
	FocusFn      func(ctx context.Context) error
}

// OpenCodeEngine implements engine.Engine and engine.TaskStatusProvider using
// OpenCode's shared local v2 HTTP/SSE server (shared by OpenCode Desktop and CLI).
type OpenCodeEngine struct {
	client       opencode.Client
	defaultAgent string
	defaultModel string
	focusDesktop bool
	focusFn      func(ctx context.Context) error
}

// NewOpenCodeEngine constructs an OpenCodeEngine backed by opencode.HTTPClient.
func NewOpenCodeEngine(clientOpts []opencode.ClientOption, engOpts OpenCodeEngineOptions) *OpenCodeEngine {
	return NewOpenCodeEngineWithClient(opencode.NewHTTPClient(clientOpts...), engOpts)
}

// NewOpenCodeEngineWithClient constructs an OpenCodeEngine with an injected opencode.Client (for tests).
func NewOpenCodeEngineWithClient(client opencode.Client, opts OpenCodeEngineOptions) *OpenCodeEngine {
	focusFn := opts.FocusFn
	if focusFn == nil {
		focusFn = defaultFocusOpenCodeDesktop
	}
	return &OpenCodeEngine{
		client:       client,
		defaultAgent: strings.TrimSpace(opts.DefaultAgent),
		defaultModel: strings.TrimSpace(opts.DefaultModel),
		focusDesktop: opts.FocusDesktop,
		focusFn:      focusFn,
	}
}

func (e *OpenCodeEngine) Name() string {
	return "opencode"
}

// Dispatch creates a session scoped to req.ProjectPath on the shared local OpenCode server
// and enqueues req.Prompt asynchronously. The session immediately appears in OpenCode Desktop.
func (e *OpenCodeEngine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	if strings.TrimSpace(req.ProjectPath) == "" {
		return nil, fmt.Errorf("opencode dispatch: project path is required")
	}

	agent, modelRef := e.resolveAgentAndModel(req.AgentCmd)

	title := strings.TrimSpace(req.TaskSlug)
	createReq := opencode.CreateSessionRequest{
		Title: title,
		Agent: agent,
		Model: modelRef,
		Location: &opencode.SessionLocation{
			Directory: req.ProjectPath,
		},
	}

	sess, err := e.client.CreateSession(ctx, createReq)
	if err != nil {
		return nil, fmt.Errorf("opencode create session: %w", err)
	}
	if sess == nil || sess.ID == "" {
		return nil, fmt.Errorf("opencode create session: empty session ID returned")
	}

	if strings.TrimSpace(req.Prompt) != "" {
		if err := e.client.Prompt(ctx, sess.ID, opencode.PromptRequest{Text: req.Prompt}); err != nil {
			return nil, fmt.Errorf("opencode prompt session %s: %w", sess.ID, err)
		}
	}

	return &DispatchResult{
		SessionID: sess.ProjectID,
		WindowID:  sess.ID,
		BlockID:   sess.ID,
		PickedUp:  true,
		Extra: map[string]any{
			"backend": "opencode",
		},
	}, nil
}

// Send delivers a follow-up prompt to an existing OpenCode session, or interrupts it on ctrl-c/esc.
func (e *OpenCodeEngine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	if strings.TrimSpace(blockID) == "" {
		return fmt.Errorf("opencode send: session ID (blockID) is required")
	}
	if input.Text != "" {
		return e.client.Prompt(ctx, blockID, opencode.PromptRequest{Text: input.Text})
	}
	key := strings.ToLower(strings.TrimSpace(input.Key))
	switch key {
	case "ctrl-c", "esc", "escape":
		return e.client.Interrupt(ctx, blockID)
	case "":
		return fmt.Errorf("opencode send: message text is required")
	default:
		return fmt.Errorf("opencode send: raw key input %q is not supported; provide text or ctrl-c/esc", input.Key)
	}
}

// Capture fetches a session's metadata and recent messages and formats them as readable text.
func (e *OpenCodeEngine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	if strings.TrimSpace(blockID) == "" {
		return "", fmt.Errorf("opencode capture: session ID (blockID) is required")
	}

	sess, err := e.client.GetSession(ctx, blockID)
	if err != nil {
		return "", fmt.Errorf("opencode get session: %w", err)
	}

	activeMap, _ := e.client.GetActiveSessions(ctx)
	_, isActive := activeMap[blockID]

	msgs, err := e.client.ListMessages(ctx, blockID, 50, "desc")
	if err != nil {
		return "", fmt.Errorf("opencode list messages: %w", err)
	}

	return formatOpenCodeCapture(sess, isActive, msgs, lines), nil
}

// Close is non-destructive: if the session is actively running on the shared server,
// it interrupts the active turn via POST /api/session/{id}/interrupt, leaving the
// conversation history intact in OpenCode Desktop. Offline/404 errors are ignored
// so local state cleanup remains idempotent.
func (e *OpenCodeEngine) Close(ctx context.Context, sessionID, windowID string) error {
	if strings.TrimSpace(windowID) == "" {
		return nil
	}
	activeMap, err := e.client.GetActiveSessions(ctx)
	if err != nil {
		// Offline-friendly: allow local Buddy state close even when server is stopped.
		return nil
	}
	if _, running := activeMap[windowID]; running {
		if intErr := e.client.Interrupt(ctx, windowID); intErr != nil && !errors.Is(intErr, opencode.ErrSessionNotFound) {
			return fmt.Errorf("opencode interrupt session %s: %w", windowID, intErr)
		}
	}
	return nil
}

// Focus activates OpenCode Desktop on macOS when enabled; no-op elsewhere.
func (e *OpenCodeEngine) Focus(ctx context.Context, sessionID, windowID string) error {
	if !e.focusDesktop {
		return nil
	}
	if e.focusFn != nil {
		return e.focusFn(ctx)
	}
	return nil
}

// OpenDevSplit is intentionally skipped for OpenCode Desktop in this phase.
func (e *OpenCodeEngine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", &ErrNotImplemented{
		Engine:    "opencode",
		Operation: "open_dev_split",
		Message:   "opencode engine open dev split is not yet implemented.",
	}
}

// GetActiveSessions returns the map of currently running session IDs on the shared server.
func (e *OpenCodeEngine) GetActiveSessions(ctx context.Context) (map[string]opencode.SessionActive, error) {
	return e.client.GetActiveSessions(ctx)
}

// ListProjectSessions lists non-archived sessions scoped to a project directory.
func (e *OpenCodeEngine) ListProjectSessions(ctx context.Context, projectPath string, limit int) ([]opencode.SessionInfo, error) {
	if strings.TrimSpace(projectPath) == "" {
		return nil, fmt.Errorf("opencode list project sessions: project path is required")
	}
	archived := false
	return e.client.ListSessions(ctx, opencode.ListSessionsOptions{
		Directory: projectPath,
		Archived:  &archived,
		Limit:     limit,
	})
}

// ProbeStatus queries the shared OpenCode server and maps live session state into
// Buddy's structured task status vocabulary without terminal scraping.
func (e *OpenCodeEngine) ProbeStatus(ctx context.Context, sessionID, blockID string) (TaskStatusProbe, error) {
	if strings.TrimSpace(blockID) == "" {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("opencode probe status: session ID (blockID) is required")
	}
	activeMap, err := e.client.GetActiveSessions(ctx)
	if err != nil {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("opencode get active sessions: %w", err)
	}
	return e.ProbeStatusWithActive(ctx, blockID, activeMap)
}

// ProbeStatusWithActive evaluates a session's status using a pre-fetched active-sessions map
// (used by sync.go to avoid O(N) /api/session/active calls).
func (e *OpenCodeEngine) ProbeStatusWithActive(ctx context.Context, sessionID string, activeMap map[string]opencode.SessionActive) (TaskStatusProbe, error) {
	if strings.TrimSpace(sessionID) == "" {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("opencode probe status: session ID is required")
	}

	sess, err := e.client.GetSession(ctx, sessionID)
	if err != nil {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("opencode get session: %w", err)
	}
	if sess == nil {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("opencode get session: empty response")
	}

	// 1. Pending permissions or interactive forms always mean waiting_for_input.
	if perms, pErr := e.client.ListPermissions(ctx, sessionID); pErr == nil && len(perms) > 0 {
		return TaskStatusProbe{
			Status:  "waiting_for_input",
			Working: false,
			Event:   "waiting_for_input",
			Summary: oneLineSummary(perms[0].Summary()),
		}, nil
	}
	if forms, fErr := e.client.ListForms(ctx, sessionID); fErr == nil && len(forms) > 0 {
		return TaskStatusProbe{
			Status:  "waiting_for_input",
			Working: false,
			Event:   "waiting_for_input",
			Summary: oneLineSummary(forms[0].Summary()),
		}, nil
	}

	// 2. Actively running on the server.
	if act, running := activeMap[sessionID]; running && (act.Type == "" || strings.EqualFold(act.Type, "running")) {
		if strings.EqualFold(strings.TrimSpace(sess.Agent), "plan") {
			summary := "Session is planning"
			if sess.Title != "" {
				summary = oneLineSummary(sess.Title)
			}
			return TaskStatusProbe{
				Status:  "planning",
				Working: true,
				Summary: summary,
			}, nil
		}
		summary := "Session is running"
		if sess.Title != "" {
			summary = oneLineSummary(sess.Title)
		}
		return TaskStatusProbe{
			Status:  "running",
			Working: true,
			Summary: summary,
		}, nil
	}

	// 3. Idle / settled: inspect recent messages for summary or error details.
	msgs, _ := e.client.ListMessages(ctx, sessionID, 20, "desc")
	snapshot := formatOpenCodeCapture(sess, false, msgs, 0)

	outcome := strings.ToLower(strings.TrimSpace(sess.Outcome))
	switch outcome {
	case "failed", "interrupted":
		summary := extractOpenCodeErrorSummary(msgs, outcome)
		return TaskStatusProbe{
			Status:   "error",
			Working:  false,
			Event:    "error",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil

	case "succeeded":
		summary := summarizeOpenCodeMessages(msgs, true)
		if summary == "" {
			summary = oneLineSummary(sess.Title)
		}
		if summary == "" {
			summary = "Session completed"
		}
		return TaskStatusProbe{
			Status:   "ready_for_demo",
			Working:  false,
			Event:    "ready_for_demo",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil
	}

	// Fallback when outcome is omitted: if idle timestamp is set and there is an assistant message.
	if sess.Time.Idle > 0 && hasOpenCodeAssistantMessage(msgs) {
		summary := summarizeOpenCodeMessages(msgs, true)
		if summary == "" {
			summary = "Session ready"
		}
		return TaskStatusProbe{
			Status:   "ready_for_demo",
			Working:  false,
			Event:    "ready_for_demo",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil
	}

	return TaskStatusProbe{
		Status:   "unknown",
		Working:  false,
		Summary:  summarizeOpenCodeMessages(msgs, false),
		Snapshot: snapshot,
	}, nil
}

func (e *OpenCodeEngine) resolveAgentAndModel(agentCmd []string) (string, *opencode.ModelRef) {
	agent := e.defaultAgent
	modelSpec := e.defaultModel

	for _, raw := range agentCmd {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			modelSpec = s
		} else {
			agent = s
		}
	}

	return agent, parseOpenCodeModelRef(modelSpec)
}

func parseOpenCodeModelRef(spec string) *opencode.ModelRef {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	provider, rest, ok := strings.Cut(spec, "/")
	if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(rest) == "" {
		return nil
	}
	modelID := rest
	variant := ""
	if m, v, hasVariant := strings.Cut(rest, "#"); hasVariant {
		modelID = m
		variant = v
	}
	if strings.TrimSpace(modelID) == "" {
		return nil
	}
	return &opencode.ModelRef{
		ProviderID: strings.TrimSpace(provider),
		ID:         strings.TrimSpace(modelID),
		Variant:    strings.TrimSpace(variant),
	}
}

func sortMessagesChronologically(msgs []opencode.MessageInfo) []opencode.MessageInfo {
	out := make([]opencode.MessageInfo, len(msgs))
	copy(out, msgs)
	if len(out) < 2 {
		return out
	}
	// If timestamps are populated, sort ascending by created time.
	hasTimestamps := false
	for _, m := range out {
		if m.Time.Created > 0 {
			hasTimestamps = true
			break
		}
	}
	if hasTimestamps {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Time.Created < out[j].Time.Created
		})
	}
	return out
}

func hasOpenCodeAssistantMessage(msgs []opencode.MessageInfo) bool {
	for _, m := range msgs {
		if strings.EqualFold(m.Type, "assistant") && oneLineSummary(m.ExtractText()) != "" {
			return true
		}
	}
	return false
}

func summarizeOpenCodeMessages(msgs []opencode.MessageInfo, preferAssistant bool) string {
	ordered := sortMessagesChronologically(msgs)
	if preferAssistant {
		for i := len(ordered) - 1; i >= 0; i-- {
			if strings.EqualFold(ordered[i].Type, "assistant") {
				if s := oneLineSummary(ordered[i].ExtractText()); s != "" {
					return s
				}
			}
		}
	}
	for i := len(ordered) - 1; i >= 0; i-- {
		if strings.EqualFold(ordered[i].Type, "idle") {
			continue
		}
		if s := oneLineSummary(ordered[i].ExtractText()); s != "" {
			return s
		}
	}
	return ""
}

func extractOpenCodeErrorSummary(msgs []opencode.MessageInfo, outcome string) string {
	ordered := sortMessagesChronologically(msgs)
	for i := len(ordered) - 1; i >= 0; i-- {
		if errMsg := oneLineSummary(ordered[i].ExtractErrorMessage()); errMsg != "" {
			return errMsg
		}
	}
	if summary := summarizeOpenCodeMessages(msgs, true); summary != "" {
		return summary
	}
	return "Session ended with outcome: " + outcome
}

func formatOpenCodeCapture(sess *opencode.SessionInfo, isActive bool, msgs []opencode.MessageInfo, lines int) string {
	var b strings.Builder
	status := "idle"
	if isActive {
		status = "running"
	} else if sess.Outcome != "" {
		status = sess.Outcome
	}

	header := fmt.Sprintf("[session: %s | status: %s", sess.ID, status)
	if sess.Agent != "" {
		header += fmt.Sprintf(" | agent: %s", sess.Agent)
	}
	if sess.Model != nil && sess.Model.ProviderID != "" && sess.Model.ID != "" {
		header += fmt.Sprintf(" | model: %s/%s", sess.Model.ProviderID, sess.Model.ID)
	}
	if sess.Title != "" {
		header += fmt.Sprintf(" | title: %s", sess.Title)
	}
	header += "]"
	b.WriteString(header)

	ordered := sortMessagesChronologically(msgs)
	for _, msg := range ordered {
		switch strings.ToLower(msg.Type) {
		case "user", "assistant":
			text := strings.TrimRight(msg.ExtractText(), "\r\n")
			errText := msg.ExtractErrorMessage()
			if text == "" && errText == "" {
				continue
			}
			b.WriteString("\n\n[")
			b.WriteString(strings.ToLower(msg.Type))
			b.WriteString("]\n")
			if text != "" {
				b.WriteString(text)
			}
			if errText != "" {
				if text != "" {
					b.WriteString("\n")
				}
				b.WriteString("error: ")
				b.WriteString(errText)
			}
		}
	}

	formatted := b.String()
	if lines <= 0 {
		return formatted
	}
	allLines := strings.Split(formatted, "\n")
	if len(allLines) <= lines {
		return formatted
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

func defaultFocusOpenCodeDesktop(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	_ = exec.CommandContext(ctx, "open", "-a", "OpenCode").Run()
	return nil
}
