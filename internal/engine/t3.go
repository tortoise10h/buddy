package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/t3"
)

// T3Runner defines the t3 CLI operations used by T3Engine.
type T3Runner interface {
	ProjectAdd(ctx context.Context, projectPath string, title string) error
	ThreadStart(ctx context.Context, opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error)
	ThreadShow(ctx context.Context, threadID string) (*t3.ThreadShowOutput, error)
	ThreadSend(ctx context.Context, threadID string, message string) error
}

// T3Engine implements engine.Engine using the local t3 CLI.
type T3Engine struct {
	runner T3Runner
}

// NewT3Engine constructs a T3Engine backed by DefaultCommandRunner.
func NewT3Engine(opts ...t3.Option) *T3Engine {
	return &T3Engine{
		runner: t3.NewDefaultCommandRunner(opts...),
	}
}

// NewT3EngineWithRunner constructs a T3Engine with an injected T3Runner (for tests).
func NewT3EngineWithRunner(r T3Runner) *T3Engine {
	return &T3Engine{runner: r}
}

func (e *T3Engine) Name() string {
	return "t3"
}

// Dispatch registers the project in T3 (idempotently) and starts a top-level project thread.
func (e *T3Engine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) {
	if req.ProjectPath == "" {
		return nil, fmt.Errorf("t3 dispatch: project path is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("t3 dispatch: prompt is required")
	}

	provider, model := parseProviderModel(req.AgentCmd)
	if provider == "" || model == "" {
		provider, model = resolveEnvProviderModel()
	}
	if provider == "" || model == "" {
		return nil, fmt.Errorf("t3 dispatch: missing provider/model. Set engines.t3.provider and engines.t3.model in ~/.buddy/config.json (via 'buddy-ctl config set'), pass --agent <provider>/<model>, or set BUDDY_T3_MODEL=<provider>/<model>")
	}

	if err := e.runner.ProjectAdd(ctx, req.ProjectPath, req.ProjectName); err != nil && !isProjectAlreadyExistsError(err) {
		return nil, fmt.Errorf("t3 project add: %w", err)
	}

	opts := t3.ThreadStartOptions{
		ProjectPath: req.ProjectPath,
		Prompt:      req.Prompt,
		Provider:    provider,
		Model:       model,
	}
	out, err := e.runner.ThreadStart(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("t3 thread start: %w", err)
	}

	return &DispatchResult{
		SessionID: out.ProjectID,
		WindowID:  out.ThreadID,
		BlockID:   out.ThreadID,
		PickedUp:  true,
		Extra: map[string]any{
			"backend": "t3",
			"run_id":  out.RunID,
		},
	}, nil
}

// Send delivers a follow-up text message to an existing T3 thread.
func (e *T3Engine) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	if blockID == "" {
		return fmt.Errorf("t3 send: thread ID (blockID) is required")
	}
	if input.Text == "" {
		if input.Key != "" {
			return fmt.Errorf("t3 send: raw key input %q is not supported; provide text", input.Key)
		}
		return fmt.Errorf("t3 send: message text is required")
	}
	return e.runner.ThreadSend(ctx, blockID, input.Text)
}

// Capture inspects a T3 thread's status and recent messages via `t3 thread show --json`.
func (e *T3Engine) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	if blockID == "" {
		return "", fmt.Errorf("t3 capture: thread ID (blockID) is required")
	}
	out, err := e.runner.ThreadShow(ctx, blockID)
	if err != nil {
		return "", fmt.Errorf("t3 thread show: %w", err)
	}
	return formatThreadShowOutput(out, lines), nil
}

// ProbeStatus queries `t3 thread show --json` and maps T3 thread state into
// Buddy's structured task status vocabulary without terminal text scraping.
func (e *T3Engine) ProbeStatus(ctx context.Context, sessionID, blockID string) (TaskStatusProbe, error) {
	if blockID == "" {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("t3 probe status: thread ID (blockID) is required")
	}
	out, err := e.runner.ThreadShow(ctx, blockID)
	if err != nil {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("t3 thread show: %w", err)
	}
	if out == nil {
		return TaskStatusProbe{Status: "unknown"}, fmt.Errorf("t3 thread show: empty response")
	}

	snapshot := formatThreadShowOutput(out, 0)

	if out.Thread.PendingRequestCount > 0 {
		summary := summarizeThreadMessages(out.Messages, true)
		if summary == "" {
			summary = "Waiting for input (pending request)"
		}
		return TaskStatusProbe{
			Status:   "waiting_for_input",
			Working:  false,
			Event:    "waiting_for_input",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil
	}

	rawStatus := strings.ToLower(strings.TrimSpace(out.Thread.Status))
	switch rawStatus {
	case "error", "failed", "errored", "crashed", "cancelled", "canceled":
		summary := summarizeThreadMessages(out.Messages, true)
		if summary == "" {
			summary = "Thread failed with status: " + rawStatus
		}
		return TaskStatusProbe{
			Status:   "error",
			Working:  false,
			Event:    "error",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil

	case "planning":
		summary := summarizeThreadMessages(out.Messages, false)
		if summary == "" {
			summary = "Thread is planning"
		}
		return TaskStatusProbe{
			Status:   "planning",
			Working:  true,
			Summary:  summary,
			Snapshot: snapshot,
		}, nil

	case "running", "in_progress", "active", "queued", "starting", "pending":
		summary := summarizeThreadMessages(out.Messages, false)
		if summary == "" {
			summary = "Thread is running"
		}
		return TaskStatusProbe{
			Status:   "running",
			Working:  true,
			Summary:  summary,
			Snapshot: snapshot,
		}, nil

	case "waiting", "waiting_for_input", "blocked", "paused", "needs_input":
		summary := summarizeThreadMessages(out.Messages, true)
		if summary == "" {
			summary = "Waiting for input"
		}
		return TaskStatusProbe{
			Status:   "waiting_for_input",
			Working:  false,
			Event:    "waiting_for_input",
			Summary:  summary,
			Snapshot: snapshot,
		}, nil

	case "idle", "completed", "done", "finished", "settled", "ready":
		if summary := summarizeThreadMessages(out.Messages, true); summary != "" && hasAssistantMessage(out.Messages) {
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
			Summary:  summarizeThreadMessages(out.Messages, false),
			Snapshot: snapshot,
		}, nil

	default:
		return TaskStatusProbe{
			Status:   "unknown",
			Working:  false,
			Summary:  summarizeThreadMessages(out.Messages, false),
			Snapshot: snapshot,
		}, nil
	}
}

func hasAssistantMessage(messages []t3.MessageInfo) bool {
	for _, m := range messages {
		if strings.EqualFold(strings.TrimSpace(m.Role), "assistant") && oneLineSummary(m.Text) != "" {
			return true
		}
	}
	return false
}

func summarizeThreadMessages(messages []t3.MessageInfo, preferAssistant bool) string {
	if preferAssistant {
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(strings.TrimSpace(messages[i].Role), "assistant") {
				if s := oneLineSummary(messages[i].Text); s != "" {
					return s
				}
			}
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if s := oneLineSummary(messages[i].Text); s != "" {
			return s
		}
	}
	return ""
}

func oneLineSummary(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if collapsed == "" {
		return ""
	}
	if len(collapsed) > 240 {
		return collapsed[:237] + "..."
	}
	return collapsed
}

func formatThreadShowOutput(out *t3.ThreadShowOutput, lines int) string {
	var b strings.Builder
	header := fmt.Sprintf("[thread: %s | status: %s", out.Thread.ThreadID, out.Thread.Status)
	if out.Thread.Model != "" {
		header += fmt.Sprintf(" | model: %s", out.Thread.Model)
	}
	if out.Thread.PendingRequestCount > 0 {
		header += fmt.Sprintf(" | pending_requests: %d", out.Thread.PendingRequestCount)
	}
	header += "]"
	b.WriteString(header)

	for _, msg := range out.Messages {
		b.WriteString("\n\n[")
		b.WriteString(msg.Role)
		b.WriteString("]\n")
		b.WriteString(strings.TrimRight(msg.Text, "\r\n"))
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

// Close detaches the thread from Buddy tracking without modifying T3.
// T3 has no server-side close; Buddy marks the task closed and stops tracking the thread.
// Returning nil keeps close idempotent and offline-friendly so local state cleanup
// succeeds even when the t3 daemon is unavailable or the thread ID is empty/unknown.
func (e *T3Engine) Close(ctx context.Context, sessionID, windowID string) error {
	return nil
}

func (e *T3Engine) Focus(ctx context.Context, sessionID, windowID string) error {
	return nil
}

func (e *T3Engine) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", &ErrNotImplemented{
		Engine:    "t3",
		Operation: "open_dev_split",
		Message:   "t3 engine open dev split is not yet implemented.",
	}
}

func isProjectAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "ProjectAlreadyExistsError") || strings.Contains(msg, "already exists")
}

func resolveEnvProviderModel() (string, string) {
	if env := strings.TrimSpace(os.Getenv("BUDDY_T3_MODEL")); env != "" {
		if p, m, ok := strings.Cut(env, "/"); ok && p != "" && m != "" {
			return p, m
		}
	}
	return "", ""
}

// parseProviderModel extracts a <provider>/<model> pair from AgentCmd.
// Both values are returned only when both are non-empty, matching t3 CLI's requirement
// that --provider and --model be passed together or omitted together.
func parseProviderModel(agentCmd []string) (string, string) {
	if len(agentCmd) == 0 {
		return "", ""
	}
	spec := strings.TrimSpace(agentCmd[0])
	provider, model, ok := strings.Cut(spec, "/")
	if !ok || provider == "" || model == "" {
		return "", ""
	}
	return provider, model
}
