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

func TestT3Engine_Dispatch_MissingProviderModelFailsFast(t *testing.T) {
	t.Setenv("BUDDY_T3_MODEL", "")
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
	if err == nil {
		t.Fatalf("expected error for missing provider/model, got nil")
	}
	if !strings.Contains(err.Error(), "engines.t3.provider") {
		t.Errorf("expected actionable error mentioning engines.t3.provider, got: %v", err)
	}
	if len(mock.projectAddCalls) != 0 || len(mock.threadStartCalls) != 0 {
		t.Errorf("expected fail-fast before calling runner, got projectAdd=%d threadStart=%d", len(mock.projectAddCalls), len(mock.threadStartCalls))
	}
}

func TestT3Engine_Dispatch_ValidationAndErrors(t *testing.T) {
	t.Setenv("BUDDY_T3_MODEL", "")
	eng := NewT3EngineWithRunner(&mockT3Runner{})
	ctx := context.Background()

	if _, err := eng.Dispatch(ctx, DispatchRequest{Prompt: "hi"}); err == nil {
		t.Error("expected error for missing ProjectPath")
	}
	if _, err := eng.Dispatch(ctx, DispatchRequest{ProjectPath: "/tmp/p"}); err == nil {
		t.Error("expected error for missing Prompt")
	}

	engAddFail := NewT3EngineWithRunner(&mockT3Runner{projectAddErr: errors.New("boom")})
	if _, err := engAddFail.Dispatch(ctx, DispatchRequest{
		ProjectPath: "/tmp/p",
		Prompt:      "hi",
		AgentCmd:    []string{"antigravity/gemini-3.8-flash-medium"},
	}); err == nil {
		t.Error("expected error when ProjectAdd fails")
	}

	engAlreadyExists := NewT3EngineWithRunner(&mockT3Runner{
		projectAddErr:  errors.New("t3 command failed (exit 1): ProjectAlreadyExistsError: An active project already exists for '/tmp/p'."),
		threadStartOut: &t3.ThreadStartOutput{ThreadID: "th-existing", ProjectID: "proj-existing"},
	})
	res, err := engAlreadyExists.Dispatch(ctx, DispatchRequest{
		ProjectPath: "/tmp/p",
		Prompt:      "hi",
		AgentCmd:    []string{"antigravity/gemini-3.8-flash-medium"},
	})
	if err != nil {
		t.Fatalf("expected Dispatch to ignore ProjectAlreadyExistsError, got: %v", err)
	}
	if res.WindowID != "th-existing" || res.SessionID != "proj-existing" {
		t.Errorf("unexpected DispatchResult: %+v", res)
	}
}

func TestT3Engine_Dispatch_UsesEnvProviderModelFallback(t *testing.T) {
	t.Setenv("BUDDY_T3_MODEL", "anthropic/claude-sonnet-4-5")
	mock := &mockT3Runner{
		threadStartOut: &t3.ThreadStartOutput{ThreadID: "th-env", ProjectID: "proj-1", RunID: "run-1"},
	}
	eng := NewT3EngineWithRunner(mock)

	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "docnotate",
		ProjectPath: "/tmp/docnotate",
		Prompt:      "Implement search",
	})
	if err != nil {
		t.Fatalf("unexpected Dispatch error: %v", err)
	}
	if len(mock.threadStartCalls) != 1 {
		t.Fatalf("expected 1 ThreadStart call, got %d", len(mock.threadStartCalls))
	}
	if mock.threadStartCalls[0].Provider != "anthropic" || mock.threadStartCalls[0].Model != "claude-sonnet-4-5" {
		t.Errorf("expected anthropic/claude-sonnet-4-5, got %s/%s", mock.threadStartCalls[0].Provider, mock.threadStartCalls[0].Model)
	}
	if res.WindowID != "th-env" {
		t.Errorf("expected thread ID th-env, got %s", res.WindowID)
	}
}

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

func TestT3Engine_Close(t *testing.T) {
	mock := &mockT3Runner{
		threadShowErr: errors.New("t3 daemon unavailable"),
	}
	eng := NewT3EngineWithRunner(mock)
	ctx := context.Background()

	cases := []struct {
		name      string
		sessionID string
		windowID  string
	}{
		{name: "with session and thread id", sessionID: "proj-1", windowID: "mcp:thread-1"},
		{name: "without session id", sessionID: "", windowID: "mcp:thread-1"},
		{name: "empty thread id", sessionID: "", windowID: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := eng.Close(ctx, tc.sessionID, tc.windowID)
			if err != nil {
				t.Fatalf("expected nil error from Close, got: %v", err)
			}
			var notImpl *ErrNotImplemented
			if errors.As(err, &notImpl) {
				t.Fatalf("Close must not return ErrNotImplemented")
			}
		})
	}

	// Idempotent: calling Close twice on the same thread succeeds both times.
	if err := eng.Close(ctx, "proj-1", "mcp:thread-1"); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := eng.Close(ctx, "proj-1", "mcp:thread-1"); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
