package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backend"
)

type fakeTerminalBackend struct {
	ensureSessionCalled    bool
	createTaskWindowCalled bool
	sendCalled             bool
	captureCalled          bool
	lastSentText           string
	sentKeys               []string
	staticCapture          string
	captures               []string
	captureIdx             int
}

func (f *fakeTerminalBackend) Name() string {
	return "fake-rex"
}

func (f *fakeTerminalBackend) IsAvailable(ctx context.Context) bool {
	return true
}

func (f *fakeTerminalBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	f.ensureSessionCalled = true
	return backend.SessionInfo{
		SessionID: "sess_fake_1",
		Name:      name,
		Path:      cwd,
	}, nil
}

func (f *fakeTerminalBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	return []backend.SessionInfo{{SessionID: "sess_fake_1", Name: "my-project", Path: "/cwd"}}, nil
}

func (f *fakeTerminalBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	return nil, nil
}

func (f *fakeTerminalBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	return backend.WindowInfo{WindowID: windowID, SessionID: sessionID}, nil
}

func (f *fakeTerminalBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	f.createTaskWindowCalled = true
	return "win_fake_1", "block_fake_1", nil
}

func (f *fakeTerminalBackend) Close(ctx context.Context, sessionID, windowID string) error {
	return nil
}

func (f *fakeTerminalBackend) Focus(ctx context.Context, sessionID, windowID string) error {
	return nil
}

func (f *fakeTerminalBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	f.captureCalled = true
	if len(f.captures) > 0 {
		idx := f.captureIdx
		if idx >= len(f.captures) {
			idx = len(f.captures) - 1
		}
		f.captureIdx++
		return f.captures[idx], nil
	}
	if f.staticCapture != "" {
		return f.staticCapture, nil
	}
	if f.lastSentText != "" {
		return "> " + f.lastSentText + "\n⠋ Thinking... ? for shortcuts", nil
	}
	return "agent ready ? for shortcuts", nil
}

func (f *fakeTerminalBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	f.sendCalled = true
	if input.Text != "" {
		f.lastSentText = input.Text
	}
	if input.Key != "" {
		f.sentKeys = append(f.sentKeys, input.Key)
	}
	return nil
}

func (f *fakeTerminalBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "split_block_1", nil
}

func (f *fakeTerminalBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	return true, nil
}

func TestTerminalEngine_Dispatch(t *testing.T) {
	fake := &fakeTerminalBackend{}
	eng := NewTerminalEngine(fake, WithReadinessTimeout(1*time.Second))

	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "my-project",
		ProjectPath: "/cwd",
		TaskSlug:    "feat-x",
		Prompt:      "implement feature x",
		AgentCmd:    []string{"agy"},
	})

	if err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}

	if !fake.ensureSessionCalled {
		t.Errorf("expected EnsureSession to be called")
	}
	if !fake.createTaskWindowCalled {
		t.Errorf("expected CreateTaskWindow to be called")
	}
	if !fake.sendCalled {
		t.Errorf("expected Send to be called for prompt delivery")
	}
	if !fake.captureCalled {
		t.Errorf("expected Capture to be called for probe and pickup check")
	}
	if res.SessionID != "sess_fake_1" || res.WindowID != "win_fake_1" || res.BlockID != "block_fake_1" {
		t.Errorf("unexpected dispatch result: %+v", res)
	}
	if !res.PickedUp {
		t.Errorf("expected task to be marked picked up, got false (reason: %s)", res.PickupReason)
	}
}

func TestTerminalEngine_Dispatch_PickupVerification(t *testing.T) {
	tests := []struct {
		name               string
		captures           []string
		wantPickedUp       bool
		wantReasonContains string
	}{
		{
			name: "prompt echoed in pane confirms pickup",
			captures: []string{
				"agent ready ? for shortcuts",
				"> implement OAuth login flow\n? for shortcuts",
			},
			wantPickedUp:       true,
			wantReasonContains: "",
		},
		{
			name: "agent working indicator confirms pickup even without literal prompt echo",
			captures: []string{
				"agent ready ? for shortcuts",
				"⠋ Thinking about request (esc to cancel)\n? for shortcuts",
			},
			wantPickedUp:       true,
			wantReasonContains: "",
		},
		{
			name: "onboarding splash swallows prompt and remains visible -> picked_up false",
			captures: []string{
				"Auto mode: edits & safe commands auto-approved\nesc to dismiss\n? for shortcuts",
			},
			wantPickedUp:       false,
			wantReasonContains: "onboarding",
		},
		{
			name: "static idle prompt where prompt never appears -> picked_up false",
			captures: []string{
				"agent ready ? for shortcuts",
			},
			wantPickedUp:       false,
			wantReasonContains: "not echoed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTerminalBackend{captures: tc.captures}
			eng := NewTerminalEngine(fake, WithReadinessTimeout(300*time.Millisecond))

			res, err := eng.Dispatch(context.Background(), DispatchRequest{
				ProjectName: "my-project",
				ProjectPath: "/cwd",
				TaskSlug:    "feat-auth",
				Prompt:      "implement OAuth login flow",
				AgentCmd:    []string{"agy"},
			})
			if err != nil {
				t.Fatalf("unexpected dispatch error: %v", err)
			}
			if res.PickedUp != tc.wantPickedUp {
				t.Errorf("PickedUp = %v, want %v (PickupReason: %q)", res.PickedUp, tc.wantPickedUp, res.PickupReason)
			}
			if tc.wantReasonContains != "" && !strings.Contains(strings.ToLower(res.PickupReason), strings.ToLower(tc.wantReasonContains)) {
				t.Errorf("PickupReason = %q, want containing %q", res.PickupReason, tc.wantReasonContains)
			}
		})
	}
}
