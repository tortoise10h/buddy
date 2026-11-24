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
	sentInputs             []backend.Input
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
	f.sentInputs = append(f.sentInputs, input)
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

func TestTerminalEngine_Send_And_Dispatch_PreserveExclamationPrompt(t *testing.T) {
	fake := &fakeTerminalBackend{}
	eng := NewTerminalEngine(fake, WithReadinessTimeout(300*time.Millisecond))

	prompt := "Please upgrade the notes to match this look!"
	if err := eng.Send(context.Background(), "sess_1", "blk_1", backend.Input{
		Text:  prompt,
		Enter: true,
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if fake.lastSentText != prompt {
		t.Errorf("Send lastSentText = %q, want %q", fake.lastSentText, prompt)
	}

	fake.lastSentText = ""
	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "my-project",
		ProjectPath: "/cwd",
		TaskSlug:    "feat-exclaim",
		Prompt:      prompt,
		AgentCmd:    []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if fake.lastSentText != prompt {
		t.Errorf("Dispatch lastSentText = %q, want %q", fake.lastSentText, prompt)
	}
	if !res.PickedUp {
		t.Errorf("expected Dispatch with '!' prompt to be picked up, got false (reason: %s)", res.PickupReason)
	}
}

const openCodeEmptySplash = `
                                                          ▄
                            █▀▀█ █▀▀█ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█ █▀▀█ █▀▀█
                            █  █ █  █ █▀▀▀ █  █ █    █  █ █  █ █▀▀▀
                            ▀▀▀▀ █▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀

                                   /update to install v2.4.1

          ┃
          ┃  Ask anything...
          ┃
          ┃  Build · MiMo-V2.6-Flash OpenCode Go
          ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
          …/board-resolu…/og-cards-and-handwriting  shift+tab agents  ctrl+p commands

  ⊙ 2 MCP /mcps                                                            2.3.2
`

const openCodeUnsubmittedPasteSplash = `
                                                          ▄
                            █▀▀█ █▀▀█ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█ █▀▀█ █▀▀█
                            █  █ █  █ █▀▀▀ █  █ █    █  █ █  █ █▀▀▀
                            ▀▀▀▀ █▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀

                                   /update to install v2.4.1

          ┃
          ┃  [Pasted ~19 lines]
          ┃
          ┃  Build · MiMo-V2.6-Flash OpenCode Go
          ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
          …/board-resolu…/og-cards-and-handwriting  shift+tab agents  ctrl+p commands

  ⊙ 2 MCP /mcps                                                            2.3.2
`

func TestTerminalEngine_Dispatch_OpenCodeUnsubmittedPaste(t *testing.T) {
	multiLinePrompt := "Reconcile localStorage owned note IDs with the server\nStep 1: inspect store\nStep 2: add tests"

	t.Run("retries enter when [Pasted ~19 lines] appears and confirms pickup after submission", func(t *testing.T) {
		fake := &fakeTerminalBackend{
			captures: []string{
				openCodeEmptySplash,
				openCodeUnsubmittedPasteSplash,
				"Reconcile localStorage owned note IDs with the server\n⠋ Thinking...\nshift+tab agents  ctrl+p commands",
			},
		}
		eng := NewTerminalEngine(fake,
			WithReadinessTimeout(200*time.Millisecond),
			WithPickupVerifyTimeout(120*time.Millisecond, 15*time.Millisecond),
		)

		res, err := eng.Dispatch(context.Background(), DispatchRequest{
			ProjectName: "board-resolution",
			ProjectPath: "/cwd",
			TaskSlug:    "owned-note-reconcile",
			Prompt:      multiLinePrompt,
			AgentCmd:    []string{"opencode"},
		})
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if !res.PickedUp {
			t.Fatalf("expected PickedUp=true after retry enter submitted paste, got false (reason: %q)", res.PickupReason)
		}
		if len(fake.sentInputs) != 2 {
			t.Fatalf("expected 2 Send calls (initial prompt + 1 enter retry), got %d: %+v", len(fake.sentInputs), fake.sentInputs)
		}
		if fake.sentInputs[1].Text != "" || !fake.sentInputs[1].Enter {
			t.Errorf("expected retry Send to be Enter-only, got %+v", fake.sentInputs[1])
		}
	})

	t.Run("stays unsubmitted as [Pasted ~19 lines] on splash -> picked_up false after retries", func(t *testing.T) {
		fake := &fakeTerminalBackend{
			captures: []string{
				openCodeEmptySplash,
				openCodeUnsubmittedPasteSplash,
			},
		}
		eng := NewTerminalEngine(fake,
			WithReadinessTimeout(200*time.Millisecond),
			WithPickupVerifyTimeout(80*time.Millisecond, 15*time.Millisecond),
		)

		res, err := eng.Dispatch(context.Background(), DispatchRequest{
			ProjectName: "board-resolution",
			ProjectPath: "/cwd",
			TaskSlug:    "owned-note-reconcile",
			Prompt:      multiLinePrompt,
			AgentCmd:    []string{"opencode"},
		})
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if res.PickedUp {
			t.Fatalf("expected PickedUp=false when [Pasted ~19 lines] stays unsubmitted on splash screen")
		}
		if !strings.Contains(res.PickupReason, "[Pasted ~19 lines]") || !strings.Contains(strings.ToLower(res.PickupReason), "unsubmitted") {
			t.Errorf("expected PickupReason to report unsubmitted [Pasted ~19 lines], got %q", res.PickupReason)
		}
		wantSends := 1 + maxUnsubmittedSubmitRetries
		if len(fake.sentInputs) != wantSends {
			t.Errorf("expected %d Send calls (1 initial + %d retries), got %d", wantSends, maxUnsubmittedSubmitRetries, len(fake.sentInputs))
		}
	})

	t.Run("stays on OpenCode splash even when status bar changes -> picked_up false", func(t *testing.T) {
		splashBefore := strings.Replace(openCodeEmptySplash, "2 MCP", "0 MCP", 1)
		fake := &fakeTerminalBackend{
			captures: []string{
				splashBefore,
				openCodeEmptySplash,
			},
		}
		eng := NewTerminalEngine(fake,
			WithReadinessTimeout(200*time.Millisecond),
			WithPickupVerifyTimeout(60*time.Millisecond, 15*time.Millisecond),
		)

		res, err := eng.Dispatch(context.Background(), DispatchRequest{
			ProjectName: "board-resolution",
			ProjectPath: "/cwd",
			TaskSlug:    "owned-note-reconcile",
			Prompt:      multiLinePrompt,
			AgentCmd:    []string{"opencode"},
		})
		if err != nil {
			t.Fatalf("unexpected dispatch error: %v", err)
		}
		if res.PickedUp {
			t.Fatalf("expected PickedUp=false when pane remains on OpenCode home/splash screen")
		}
		if !strings.Contains(strings.ToLower(res.PickupReason), "home/splash") {
			t.Errorf("expected PickupReason to mention home/splash screen, got %q", res.PickupReason)
		}
		if len(fake.sentInputs) != 1 {
			t.Errorf("expected no blind Enter retries on empty splash, got %d Send calls", len(fake.sentInputs))
		}
	})
}

func TestFindUnsubmittedPaste_MatchingAndFalsePositiveGuards(t *testing.T) {
	if got := FindUnsubmittedPaste("┃  [Pasted ~19 lines]  ", "some prompt"); got != "[Pasted ~19 lines]" {
		t.Errorf("OpenCode paste token = %q, want [Pasted ~19 lines]", got)
	}
	if got := FindUnsubmittedPaste("> [Pasted text #1 +19 lines]", "some prompt"); got != "[Pasted text #1 +19 lines]" {
		t.Errorf("Claude paste token = %q, want [Pasted text #1 +19 lines]", got)
	}
	if got := FindUnsubmittedPaste("PASS: Prompt pasted ([Pasted ~19 lines]) but not submitted", ""); got != "" {
		t.Errorf("prose line should not match unsubmitted paste input box, got %q", got)
	}
	if got := FindUnsubmittedPaste("┃  [Pasted ~19 lines]", "Fix [Pasted ~19 lines] issue"); got != "" {
		t.Errorf("placeholder literally present in prompt should be ignored, got %q", got)
	}
}
