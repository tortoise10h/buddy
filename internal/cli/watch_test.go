package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/notify"
	"github.com/tortoise10h/buddy/internal/state"
)

type recordedNotification struct {
	title    string
	subtitle string
	message  string
	sound    string
	enabled  bool
}

type notificationRecorder struct {
	mu            sync.Mutex
	notifications []recordedNotification
}

func (nr *notificationRecorder) Record(title, subtitle, message, sound string, enabled bool, customRunner ...notify.Runner) bool {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	nr.notifications = append(nr.notifications, recordedNotification{
		title:    title,
		subtitle: subtitle,
		message:  message,
		sound:    sound,
		enabled:  enabled,
	})
	return true
}

func (nr *notificationRecorder) Reset() {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	nr.notifications = nil
}

func (nr *notificationRecorder) All() []recordedNotification {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	copied := make([]recordedNotification, len(nr.notifications))
	copy(copied, nr.notifications)
	return copied
}

var globalNotificationRecorder = &notificationRecorder{}

func TestMain(m *testing.M) {
	// Guard: Ensure notifyFn is pointed to a recorder by default during tests
	// so real macOS desktop notifications are never sent.
	notifyFn = globalNotificationRecorder.Record
	os.Exit(m.Run())
}

type mockWatchBackend struct {
	captures   []string
	captureIdx int
	captureErr error
}

func (m *mockWatchBackend) Name() string                         { return "rex" }
func (m *mockWatchBackend) IsAvailable(ctx context.Context) bool { return true }
func (m *mockWatchBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	return backend.SessionInfo{SessionID: "sess_watch_1", Name: name, Path: cwd}, nil
}
func (m *mockWatchBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	return nil, nil
}
func (m *mockWatchBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	return nil, nil
}
func (m *mockWatchBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	return backend.WindowInfo{}, nil
}
func (m *mockWatchBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	return "win_watch_1", "blk_watch_1", nil
}
func (m *mockWatchBackend) Close(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockWatchBackend) Focus(ctx context.Context, sessionID, windowID string) error { return nil }
func (m *mockWatchBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	if m.captureErr != nil {
		return "", m.captureErr
	}
	if len(m.captures) == 0 {
		return "", nil
	}
	out := m.captures[m.captureIdx]
	if m.captureIdx < len(m.captures)-1 {
		m.captureIdx++
	}
	return out, nil
}
func (m *mockWatchBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	return nil
}
func (m *mockWatchBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	return "", nil
}
func (m *mockWatchBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	return true, nil
}

// TestWatchRegex_BeforeAfterProvesRegression verifies that old unanchored regexes matched
// prose and footers, while new anchored regexes correctly reject them.
func TestWatchRegex_BeforeAfterProvesRegression(t *testing.T) {
	oldDemoRe := regexp.MustCompile(`(?i)task\s+complete|implementation\s+complete|all\s+tests\s+pass|ready\s+for\s+(review|demo)|successfully\s+(implemented|built|completed)|done\s*!?|finished\s+implementing`)
	oldInputRe := regexp.MustCompile(`(?i)(\?|\[\?\])\s+|\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no\)|(select|choose)\s+(an?\s+)?(option|action|mode):?|proceed\s*\?|(clarification|approval)\s+needed|what\s+would\s+you\s+like|do\s+you\s+want\s+to|would\s+you\s+prefer|please\s+confirm|press\s+enter\s+to\s+continue`)
	oldErrorRe := regexp.MustCompile(`(?i)traceback\s+\(most\s+recent\s+call\s+last\)|fatal\s+error|build\s+failed|npm\s+err!|panic:\s+`)

	tests := []struct {
		name     string
		line     string
		oldDemo  bool
		newDemo  bool
		oldInput bool
		newInput bool
		oldError bool
		newError bool
	}{
		{
			name:     "Regression A: Prompt prose with 'done'",
			line:     "When done report the merge commit hash, the resolution",
			oldDemo:  true,  // Old regex suffered false-positive
			newDemo:  false, // New regex correctly rejects prose
		},
		{
			name:     "Regression B: Footer shortcut",
			line:     "? for shortcuts",
			oldInput: true,  // Old regex suffered false-positive
			newInput: false, // New regex correctly rejects footer
		},
		{
			name:     "Prose with 'panic:'",
			line:     "Please do not panic: this error is handled by fallback",
			oldError: true,  // Old regex suffered false-positive
			newError: false, // New regex correctly rejects non-line-start panic
		},
		{
			name:     "Positive demo: Task complete",
			line:     "Task complete!",
			oldDemo:  true,
			newDemo:  true,
		},
		{
			name:     "Positive demo: All tests pass",
			line:     "✓ All tests passed",
			oldDemo:  true,
			newDemo:  true,
		},
		{
			name:     "Positive input: Real question prompt",
			line:     "? Which deployment target would you like to use?",
			oldInput: true,
			newInput: true,
		},
		{
			name:     "Positive error: Panic at line start",
			line:     "panic: runtime error: index out of range",
			oldError: true,
			newError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if oldDemoRe.MatchString(tt.line) != tt.oldDemo {
				t.Errorf("oldDemoRe match = %v, want %v", oldDemoRe.MatchString(tt.line), tt.oldDemo)
			}
			if watchDemoLineRe.MatchString(tt.line) != tt.newDemo {
				t.Errorf("watchDemoLineRe match = %v, want %v", watchDemoLineRe.MatchString(tt.line), tt.newDemo)
			}
			if oldInputRe.MatchString(tt.line) != tt.oldInput {
				t.Errorf("oldInputRe match = %v, want %v", oldInputRe.MatchString(tt.line), tt.oldInput)
			}
			if matchInputLine(tt.line) != tt.newInput {
				t.Errorf("matchInputLine match = %v, want %v", matchInputLine(tt.line), tt.newInput)
			}
			if oldErrorRe.MatchString(tt.line) != tt.oldError {
				t.Errorf("oldErrorRe match = %v, want %v", oldErrorRe.MatchString(tt.line), tt.oldError)
			}
			if watchErrorLineRe.MatchString(tt.line) != tt.newError {
				t.Errorf("watchErrorLineRe match = %v, want %v", watchErrorLineRe.MatchString(tt.line), tt.newError)
			}
		})
	}
}

// TestWatch_PaneLines_FilteringAndWorking tests ANSI stripping, rule/chrome filtering,
// and working detection on tail active signals.
func TestWatch_PaneLines_FilteringAndWorking(t *testing.T) {
	t.Run("Regression A fixture: echoed prompt plus running command", func(t *testing.T) {
		raw := "\x1b[32mWhen done report the merge commit hash, the resolution\x1b[0m\n" +
			"Running command: git merge main\n" +
			"⠋ In progress\n" +
			"(esc to cancel)"

		filtered, working := paneLines(raw)
		if !working {
			t.Errorf("expected working=true due to Running command and spinner glyphs, got false")
		}

		untilSet := map[string]bool{"any": true}
		matchedEvent, _ := detectEvent(filtered, untilSet)
		if matchedEvent == "ready_for_demo" {
			t.Errorf("expected no ready_for_demo event on prompt prose, got %q", matchedEvent)
		}
	})

	t.Run("Regression B fixture: footer ? for shortcuts", func(t *testing.T) {
		raw := "Step 1: compiled project\n" +
			"Tip: Use /diff to see changes\n" +
			"────────────────────────────────────────\n" +
			"? for shortcuts\n" +
			"ctrl+o to expand"

		filtered, working := paneLines(raw)
		if working {
			t.Errorf("expected working=false, got true")
		}

		for _, l := range filtered {
			if strings.Contains(l, "? for shortcuts") || strings.Contains(l, "ctrl+o") || strings.Contains(l, "Tip:") {
				t.Errorf("chrome line not dropped: %q", l)
			}
		}

		untilSet := map[string]bool{"any": true}
		matchedEvent, _ := detectEvent(filtered, untilSet)
		if matchedEvent == "waiting_for_input" {
			t.Errorf("expected no waiting_for_input event on footer shortcut, got %q", matchedEvent)
		}
	})

	t.Run("Active states in tail", func(t *testing.T) {
		fixtures := []struct {
			name    string
			raw     string
			working bool
		}{
			{
				name:    "Thought for indicator",
				raw:     "Analyzing repository...\nThought for 12s",
				working: true,
			},
			{
				name:    "Running command indicator",
				raw:     "Running command: npm test\nesc to cancel",
				working: true,
			},
			{
				name:    "Braille spinner",
				raw:     "Compiling assets\n⠹",
				working: true,
			},
			{
				name:    "Idle completed output",
				raw:     "Tests run: 45 passed\nTask complete!",
				working: false,
			},
		}

		for _, tc := range fixtures {
			t.Run(tc.name, func(t *testing.T) {
				_, working := paneLines(tc.raw)
				if working != tc.working {
					t.Errorf("working = %v, want %v", working, tc.working)
				}
			})
		}
	})
}

// TestWatch_SummaryExtraction joins wrapped continuation lines and avoids prompt echo.
func TestWatch_SummaryExtraction(t *testing.T) {
	lines := []string{
		"Starting verification suite",
		"All tests pass! The authentication flow has been verified",
		"with unit and integration tests.",
	}

	summary := extractSummary(lines, 1)
	expected := "All tests pass! The authentication flow has been verified with unit and integration tests."
	if summary != expected {
		t.Errorf("extractSummary = %q, want %q", summary, expected)
	}

	singleLine := []string{
		"Building project...",
		"Task complete!",
	}
	summarySingle := extractSummary(singleLine, 1)
	if summarySingle != "Task complete!" {
		t.Errorf("extractSummary single = %q, want 'Task complete!'", summarySingle)
	}
}

// TestWatch_PositiveC_CompletionWithDebounce verifies ready_for_demo triggers after debounce
// and delivers exactly one desktop notification with the matching event.
func TestWatch_PositiveC_CompletionWithDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	cfg := config.DefaultConfig()
	cfg.ProjectsRoots = []string{tmpDir}
	_ = config.SaveConfig(cfg)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		captures: []string{
			"Compiling...\nTask complete!",
			"Compiling...\nTask complete!",
		},
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	// Setup state with a running task
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-c", filepath.Join(tmpDir, "proj-c"), "sess_c", "sess_label_c", state.TaskState{
			TaskSlug:     "task-c",
			AgentBlockID: "blk_c",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-c",
		"--task", "task-c",
		"--debounce", "2",
		"--interval", "0",
		"--notify=true",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch execute failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
	}

	if resp["event"] != "ready_for_demo" {
		t.Errorf("event = %v, want ready_for_demo", resp["event"])
	}
	if !strings.Contains(resp["summary"].(string), "Task complete") {
		t.Errorf("summary = %v, want contain 'Task complete'", resp["summary"])
	}

	// Verify state updated
	st, _ := state.LoadState()
	p, _ := st.FindProject("proj-c")
	task, _ := p.FindTask("task-c")
	if task.Status != "ready_for_demo" {
		t.Errorf("saved task status = %v, want ready_for_demo", task.Status)
	}

	// Verify exactly one recorded notification delivered with matching event
	notifs := recorder.All()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 recorded notification, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].subtitle, "ready_for_demo") {
		t.Errorf("notification subtitle = %q, want containing 'ready_for_demo'", notifs[0].subtitle)
	}
	if !strings.Contains(notifs[0].message, "Task complete") {
		t.Errorf("notification message = %q, want containing 'Task complete'", notifs[0].message)
	}
}

// TestWatch_PositiveD_QuestionInput verifies waiting_for_input triggers on real question
// and delivers exactly one desktop notification with the matching event.
func TestWatch_PositiveD_QuestionInput(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		captures: []string{
			"? Which option would you like to select?\n1) Fast\n2) Full",
		},
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-d", filepath.Join(tmpDir, "proj-d"), "sess_d", "sess_label_d", state.TaskState{
			TaskSlug:     "task-d",
			AgentBlockID: "blk_d",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-d",
		"--task", "task-d",
		"--debounce", "1",
		"--interval", "0",
		"--notify=true",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch execute failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
	}

	if resp["event"] != "waiting_for_input" {
		t.Errorf("event = %v, want waiting_for_input", resp["event"])
	}
	if !strings.HasPrefix(resp["summary"].(string), "? Which option") {
		t.Errorf("summary = %v, want prompt text", resp["summary"])
	}

	// Verify exactly one recorded notification delivered with matching event
	notifs := recorder.All()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 recorded notification, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].subtitle, "waiting_for_input") {
		t.Errorf("notification subtitle = %q, want containing 'waiting_for_input'", notifs[0].subtitle)
	}
	if !strings.Contains(notifs[0].message, "? Which option") {
		t.Errorf("notification message = %q, want containing '? Which option'", notifs[0].message)
	}
}

// TestWatch_ErrorE_PanicLineStart verifies panic at line start triggers error
// and delivers exactly one desktop notification with the matching event.
func TestWatch_ErrorE_PanicLineStart(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		captures: []string{
			"Building binary...\npanic: runtime error: invalid memory address",
		},
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-e", filepath.Join(tmpDir, "proj-e"), "sess_e", "sess_label_e", state.TaskState{
			TaskSlug:     "task-e",
			AgentBlockID: "blk_e",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-e",
		"--task", "task-e",
		"--debounce", "1",
		"--interval", "0",
		"--notify=true",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch execute failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
	}

	if resp["event"] != "error" {
		t.Errorf("event = %v, want error", resp["event"])
	}
	if !strings.HasPrefix(resp["summary"].(string), "panic:") {
		t.Errorf("summary = %v, want line starting with panic:", resp["summary"])
	}

	// Verify exactly one recorded notification delivered with matching event
	notifs := recorder.All()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 recorded notification, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].subtitle, "error") {
		t.Errorf("notification subtitle = %q, want containing 'error'", notifs[0].subtitle)
	}
	if !strings.Contains(notifs[0].message, "panic:") {
		t.Errorf("notification message = %q, want containing 'panic:'", notifs[0].message)
	}
}

// TestWatch_TestF_CachedTerminalStatusAtStart verifies already_reported=true,
// no state rewrite, and zero notifications delivered.
func TestWatch_TestF_CachedTerminalStatusAtStart(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-f", filepath.Join(tmpDir, "proj-f"), "sess_f", "sess_label_f", state.TaskState{
			TaskSlug:     "task-f",
			AgentBlockID: "blk_f",
			Status:       "ready_for_demo",
			LastSummary:  "Previously finished feature",
		})
		return nil
	})

	stBefore, err := state.LoadState()
	if err != nil {
		t.Fatalf("failed to load state before test: %v", err)
	}
	pBefore, _ := stBefore.FindProject("proj-f")
	tBefore, _ := pBefore.FindTask("task-f")
	beforeUpdatedAt := tBefore.UpdatedAt

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-f",
		"--task", "task-f",
		"--notify=false",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch execute failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
	}

	if resp["already_reported"] != true {
		t.Errorf("already_reported = %v, want true", resp["already_reported"])
	}
	if resp["event"] != "ready_for_demo" {
		t.Errorf("event = %v, want ready_for_demo", resp["event"])
	}
	if resp["summary"] != "Previously finished feature" {
		t.Errorf("summary = %v, want 'Previously finished feature'", resp["summary"])
	}

	// Verify state file was NOT rewritten
	stAfter, _ := state.LoadState()
	pAfter, _ := stAfter.FindProject("proj-f")
	taskAfter, _ := pAfter.FindTask("task-f")
	if taskAfter.UpdatedAt != beforeUpdatedAt {
		t.Errorf("task UpdatedAt changed from %v to %v, state should not have been rewritten", beforeUpdatedAt, taskAfter.UpdatedAt)
	}

	// Verify zero notifications delivered for already_reported status
	if len(recorder.All()) != 0 {
		t.Errorf("expected 0 recorded notifications for already_reported status, got %d", len(recorder.All()))
	}
}

// TestWatch_TestG_UnknownTargetFailsFast verifies fast error return rather than timeout.
func TestWatch_TestG_UnknownTargetFailsFast(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	// Case 1: Unknown project
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "nonexistent-project",
		"--task", "nonexistent-task",
		"--timeout", "1",
		"--notify=false",
	})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error for nonexistent project, got nil")
	}

	// Case 2: Project exists, unknown task
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-g", filepath.Join(tmpDir, "proj-g"), "sess_g", "sess_label_g", state.TaskState{
			TaskSlug:     "valid-task",
			AgentBlockID: "blk_g",
			Status:       "running",
		})
		return nil
	})

	buf.Reset()
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-g",
		"--task", "missing-task",
		"--timeout", "1",
		"--notify=false",
	})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error for missing task, got nil")
	}

	// Case 3: Task exists but has no AgentBlockID
	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-g", filepath.Join(tmpDir, "proj-g"), "sess_g", "sess_label_g", state.TaskState{
			TaskSlug:     "no-block-task",
			AgentBlockID: "",
			Status:       "running",
		})
		return nil
	})

	buf.Reset()
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-g",
		"--task", "no-block-task",
		"--timeout", "1",
		"--notify=false",
	})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error for task without agent block id, got nil")
	}

	// Case 4: Engine capture error fails fast in single target mode
	mockBackend := &mockWatchBackend{
		captureErr: fmt.Errorf("rex socket connection failed"),
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	buf.Reset()
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-g",
		"--task", "valid-task",
		"--timeout", "5",
		"--notify=false",
	})
	if err := rootCmd.Execute(); err == nil {
		t.Errorf("expected error when capture fails, got nil")
	}
}
