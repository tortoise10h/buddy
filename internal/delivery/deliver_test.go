package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backends"
)

type mockBackend struct {
	captureText string
	readyResult bool
	sentText    string
	sentKey     string
}

func (m *mockBackend) Name() string                                   { return "mock" }
func (m *mockBackend) Capabilities() backends.BackendCapabilities     { return backends.BackendCapabilities{} }
func (m *mockBackend) IsAvailable(ctx context.Context) bool           { return true }
func (m *mockBackend) ListSessions(ctx context.Context) ([]backends.SessionRef, error) {
	return nil, nil
}
func (m *mockBackend) EnsureSession(ctx context.Context, name, cwd string) (backends.SessionRef, error) {
	return backends.SessionRef{SessionID: "sess-1"}, nil
}
func (m *mockBackend) InspectSession(ctx context.Context, sessionID string) (backends.SessionRef, error) {
	return backends.SessionRef{SessionID: sessionID}, nil
}
func (m *mockBackend) ListWindows(ctx context.Context, sessionID string) ([]backends.WindowRef, error) {
	return nil, nil
}
func (m *mockBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backends.WindowRef, error) {
	return backends.WindowRef{WindowID: windowID}, nil
}
func (m *mockBackend) CreateTaskWindow(ctx context.Context, session backends.SessionRef, taskSlug, cwd string, agentCmd []string) (backends.WindowRef, backends.BlockRef, error) {
	return backends.WindowRef{WindowID: "win-1"}, backends.BlockRef{BlockID: "blk-1"}, nil
}
func (m *mockBackend) SendText(ctx context.Context, sessionID, blockID, text string) error {
	m.sentText = text
	return nil
}
func (m *mockBackend) SendKey(ctx context.Context, sessionID, blockID, key string) error {
	m.sentKey = key
	return nil
}
func (m *mockBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (backends.CaptureResult, error) {
	return backends.CaptureResult{
		Text:    m.captureText,
		Lines:   stringsLines(m.captureText),
		IsAlive: true,
	}, nil
}
func (m *mockBackend) WaitReady(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error) {
	return m.readyResult, nil
}
func (m *mockBackend) DetectPrompt(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error) {
	return m.readyResult, nil
}
func (m *mockBackend) CloseWindow(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockBackend) Focus(ctx context.Context, sessionID, windowID string) error       { return nil }
func (m *mockBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (backends.BlockRef, error) {
	return backends.BlockRef{BlockID: "dev-blk"}, nil
}

func stringsLines(s string) []string {
	var res []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			res = append(res, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		res = append(res, s[start:])
	}
	return res
}

func TestVerifyBlockPickup_SuccessWithActivity(t *testing.T) {
	mock := &mockBackend{
		captureText: "Agent output\nThinking about solution...\nReading files",
	}

	ctx := context.Background()
	pickedUp, preview := VerifyBlockPickup(ctx, mock, "sess-1", "blk-1", "my prompt", 500*time.Millisecond)

	if !pickedUp {
		t.Fatalf("expected pickedUp to be true")
	}
	if preview != "Thinking about solution... | Reading files" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}

func TestVerifyBlockPickup_TimeoutPending(t *testing.T) {
	mock := &mockBackend{
		captureText: "",
	}

	ctx := context.Background()
	pickedUp, preview := VerifyBlockPickup(ctx, mock, "sess-1", "blk-1", "my prompt", 200*time.Millisecond)

	if pickedUp {
		t.Fatalf("expected pickedUp to be false on empty capture")
	}
	if preview != "Prompt dispatched (verification pending)" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}

func TestSendPromptSmart(t *testing.T) {
	mock := &mockBackend{
		readyResult: true,
		captureText: "Line 1: reading files\nLine 2: done!",
	}

	ctx := context.Background()
	pickedUp, preview := SendPromptSmart(ctx, mock, "sess-1", "blk-1", "Do something", "agy", "enter", 50)

	if !pickedUp {
		t.Fatalf("expected pickedUp to be true")
	}
	if mock.sentText != "Do something" {
		t.Fatalf("expected sent text 'Do something', got %q", mock.sentText)
	}
	if mock.sentKey != "enter" {
		t.Fatalf("expected sent key 'enter', got %q", mock.sentKey)
	}
	if preview != "Line 1: reading files | Line 2: done!" {
		t.Fatalf("unexpected preview: %q", preview)
	}
}
