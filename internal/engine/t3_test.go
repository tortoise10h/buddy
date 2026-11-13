package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/t3"
)

type mockT3Runner struct {
	projectAddCalls  []struct{ path, title string }
	threadStartCalls []t3.ThreadStartOptions
	threadShowCalls  []string
	threadSendCalls  []struct{ threadID, message string }

	projectAddErr  error
	threadStartOut *t3.ThreadStartOutput
	threadStartErr error
	threadShowOut  *t3.ThreadShowOutput
	threadShowErr  error
	threadSendErr  error
}

func (m *mockT3Runner) ProjectAdd(_ context.Context, projectPath, title string) error {
	m.projectAddCalls = append(m.projectAddCalls, struct{ path, title string }{projectPath, title})
	return m.projectAddErr
}

func (m *mockT3Runner) ThreadStart(_ context.Context, opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	m.threadStartCalls = append(m.threadStartCalls, opts)
	if m.threadStartErr != nil {
		return nil, m.threadStartErr
	}
	return m.threadStartOut, nil
}

func (m *mockT3Runner) ThreadShow(_ context.Context, threadID string) (*t3.ThreadShowOutput, error) {
	m.threadShowCalls = append(m.threadShowCalls, threadID)
	if m.threadShowErr != nil {
		return nil, m.threadShowErr
	}
	return m.threadShowOut, nil
}

func (m *mockT3Runner) ThreadSend(_ context.Context, threadID, message string) error {
	m.threadSendCalls = append(m.threadSendCalls, struct{ threadID, message string }{threadID, message})
	return m.threadSendErr
}

func TestT3Engine_Dispatch_AddsProjectAndStartsThread(t *testing.T) {
	mock := &mockT3Runner{
		threadStartOut: &t3.ThreadStartOutput{
			ThreadID:  "mcp:thread-1",
			ProjectID: "proj-1",
			RunID:     "run-1",
		},
	}
	eng := NewT3EngineWithRunner(mock)

	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "buddy",
		ProjectPath: "/tmp/buddy",
		TaskSlug:    "feat-t3",
		Prompt:      "Implement T3 engine",
		AgentCmd:    []string{"antigravity/gemini-3.8-flash-medium"},
	})
	if err != nil {
		t.Fatalf("unexpected Dispatch error: %v", err)
	}

	if len(mock.projectAddCalls) != 1 || mock.projectAddCalls[0].path != "/tmp/buddy" || mock.projectAddCalls[0].title != "buddy" {
		t.Fatalf("unexpected ProjectAdd calls: %+v", mock.projectAddCalls)
	}
	if len(mock.threadStartCalls) != 1 {
		t.Fatalf("expected 1 ThreadStart call, got %d", len(mock.threadStartCalls))
	}
	startOpts := mock.threadStartCalls[0]
	if startOpts.ProjectPath != "/tmp/buddy" || startOpts.Prompt != "Implement T3 engine" {
		t.Errorf("unexpected ThreadStart path/prompt: %+v", startOpts)
	}
	if startOpts.Provider != "antigravity" || startOpts.Model != "gemini-3.8-flash-medium" {
		t.Errorf("expected provider/model parsed, got %q / %q", startOpts.Provider, startOpts.Model)
	}

	if res.SessionID != "proj-1" || res.WindowID != "mcp:thread-1" || res.BlockID != "mcp:thread-1" || !res.PickedUp {
		t.Errorf("unexpected DispatchResult: %+v", res)
	}
	if res.Extra["backend"] != "t3" || res.Extra["run_id"] != "run-1" {
		t.Errorf("unexpected DispatchResult.Extra: %+v", res.Extra)
	}
}

func TestT3Engine_Dispatch_NonPairAgentCmdOmitsProviderAndModel(t *testing.T) {
	mock := &mockT3Runner{
		threadStartOut: &t3.ThreadStartOutput{ThreadID: "th-1", ProjectID: "p-1"},
	}
	eng := NewT3EngineWithRunner(mock)

	_, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "buddy",
		ProjectPath: "/tmp/buddy",
		Prompt:      "Hello",
		AgentCmd:    []string{"agy"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.threadStartCalls[0].Provider != "" || mock.threadStartCalls[0].Model != "" {
		t.Errorf("expected empty provider/model for non-pair AgentCmd, got %+v", mock.threadStartCalls[0])
	}
}

func TestT3Engine_Dispatch_ValidationAndErrors(t *testing.T) {
	eng := NewT3EngineWithRunner(&mockT3Runner{})
	ctx := context.Background()

	if _, err := eng.Dispatch(ctx, DispatchRequest{Prompt: "hi"}); err == nil {
		t.Error("expected error for missing ProjectPath")
	}
	if _, err := eng.Dispatch(ctx, DispatchRequest{ProjectPath: "/tmp/p"}); err == nil {
		t.Error("expected error for missing Prompt")
	}

	engAddFail := NewT3EngineWithRunner(&mockT3Runner{projectAddErr: errors.New("boom")})
	if _, err := engAddFail.Dispatch(ctx, DispatchRequest{ProjectPath: "/tmp/p", Prompt: "hi"}); err == nil {
		t.Error("expected error when ProjectAdd fails")
	}

	engAlreadyExists := NewT3EngineWithRunner(&mockT3Runner{
		projectAddErr:  errors.New("t3 command failed (exit 1): ProjectAlreadyExistsError: An active project already exists for '/tmp/p'."),
		threadStartOut: &t3.ThreadStartOutput{ThreadID: "th-existing", ProjectID: "proj-existing"},
	})
	res, err := engAlreadyExists.Dispatch(ctx, DispatchRequest{ProjectPath: "/tmp/p", Prompt: "hi"})
	if err != nil {
		t.Fatalf("expected Dispatch to ignore ProjectAlreadyExistsError, got: %v", err)
	}
	if res.WindowID != "th-existing" || res.SessionID != "proj-existing" {
		t.Errorf("unexpected DispatchResult: %+v", res)
	}
}

func TestT3Engine_Dispatch_RetriesWithFallbackWhenNoDefaultModel(t *testing.T) {
	calls := 0
	mock := &mockT3RunnerWithFn{
		threadStartFn: func(opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
			calls++
			if calls == 1 {
				if opts.Provider != "" || opts.Model != "" {
					t.Errorf("expected first attempt to omit provider/model, got %+v", opts)
				}
				return nil, errors.New("Pass modelSelection: the project has no default model.")
			}
			if opts.Provider != defaultT3Provider || opts.Model != defaultT3Model {
				t.Errorf("expected fallback %s/%s, got %s/%s", defaultT3Provider, defaultT3Model, opts.Provider, opts.Model)
			}
			return &t3.ThreadStartOutput{ThreadID: "th-fallback", ProjectID: "proj-1", RunID: "run-1"}, nil
		},
	}
	eng := NewT3EngineWithRunner(mock)

	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "docnotate",
		ProjectPath: "/tmp/docnotate",
		Prompt:      "Implement search",
		AgentCmd:    []string{"agy"},
	})
	if err != nil {
		t.Fatalf("unexpected Dispatch error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 ThreadStart calls, got %d", calls)
	}
	if res.WindowID != "th-fallback" {
		t.Errorf("expected thread ID th-fallback, got %s", res.WindowID)
	}
}

type mockT3RunnerWithFn struct {
	threadStartFn func(opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error)
}

func (m *mockT3RunnerWithFn) ProjectAdd(context.Context, string, string) error { return nil }
func (m *mockT3RunnerWithFn) ThreadStart(_ context.Context, opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	return m.threadStartFn(opts)
}
func (m *mockT3RunnerWithFn) ThreadShow(context.Context, string) (*t3.ThreadShowOutput, error) {
	return nil, nil
}
func (m *mockT3RunnerWithFn) ThreadSend(context.Context, string, string) error { return nil }

func TestT3Engine_Send(t *testing.T) {
	mock := &mockT3Runner{}
	eng := NewT3EngineWithRunner(mock)
	ctx := context.Background()

	if err := eng.Send(ctx, "proj-1", "mcp:thread-1", backend.Input{Text: "follow up", Enter: true}); err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	if len(mock.threadSendCalls) != 1 || mock.threadSendCalls[0].threadID != "mcp:thread-1" || mock.threadSendCalls[0].message != "follow up" {
		t.Fatalf("unexpected ThreadSend calls: %+v", mock.threadSendCalls)
	}

	if err := eng.Send(ctx, "proj-1", "", backend.Input{Text: "hi"}); err == nil {
		t.Error("expected error for empty blockID")
	}
	if err := eng.Send(ctx, "proj-1", "mcp:thread-1", backend.Input{Key: "enter"}); err == nil {
		t.Error("expected error for key-only input")
	}
	if err := eng.Send(ctx, "proj-1", "mcp:thread-1", backend.Input{}); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestT3Engine_Capture(t *testing.T) {
	mock := &mockT3Runner{
		threadShowOut: &t3.ThreadShowOutput{
			Thread: t3.ThreadInfo{
				ThreadID:            "mcp:thread-1",
				Status:              "running",
				Model:               "gemini-3.8-flash-medium",
				PendingRequestCount: 1,
			},
			Messages: []t3.MessageInfo{
				{Role: "user", Text: "Add auth"},
				{Role: "assistant", Text: "Line 1\nLine 2\nLine 3"},
			},
		},
	}
	eng := NewT3EngineWithRunner(mock)
	ctx := context.Background()

	out, err := eng.Capture(ctx, "proj-1", "mcp:thread-1", 100)
	if err != nil {
		t.Fatalf("unexpected Capture error: %v", err)
	}
	if len(mock.threadShowCalls) != 1 || mock.threadShowCalls[0] != "mcp:thread-1" {
		t.Fatalf("unexpected ThreadShow calls: %+v", mock.threadShowCalls)
	}
	for _, want := range []string{"status: running", "pending_requests: 1", "[user]\nAdd auth", "[assistant]\nLine 1\nLine 2\nLine 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected Capture output to contain %q, got:\n%s", want, out)
		}
	}

	tail, err := eng.Capture(ctx, "proj-1", "mcp:thread-1", 2)
	if err != nil {
		t.Fatalf("unexpected Capture(lines=2) error: %v", err)
	}
	if tail != "Line 2\nLine 3" {
		t.Errorf("expected last 2 lines 'Line 2\\nLine 3', got %q", tail)
	}

	if _, err := eng.Capture(ctx, "proj-1", "", 10); err == nil {
		t.Error("expected error for empty blockID")
	}
}
