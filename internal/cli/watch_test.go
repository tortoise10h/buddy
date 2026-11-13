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
	// Guard: Ensure notifyFn and syncRexFn are pointed to test-safe stubs by default
	// so real macOS desktop notifications and live Rex daemons are never touched.
	notifyFn = globalNotificationRecorder.Record
	syncRexFn = func(context.Context) error { return nil }
	os.Exit(m.Run())
}

type mockWatchBackend struct {
	captures         []string
	captureIdx       int
	captureErr       error
	sessions         []backend.SessionInfo
	windowsBySession map[string][]backend.WindowInfo
	programStatuses  [][]backend.ProgramStatusRecord
	programStatusIdx int
}

func (m *mockWatchBackend) Name() string                         { return "rex" }
func (m *mockWatchBackend) IsAvailable(ctx context.Context) bool { return true }
func (m *mockWatchBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	return backend.SessionInfo{SessionID: "sess_watch_1", Name: name, Path: cwd}, nil
}
func (m *mockWatchBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	return m.sessions, nil
}
func (m *mockWatchBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	if m.windowsBySession != nil {
		return m.windowsBySession[sessionID], nil
	}
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
func (m *mockWatchBackend) GetProgramStatus(ctx context.Context, sessionID, blockID string) ([]backend.ProgramStatusRecord, error) {
	if len(m.programStatuses) == 0 {
		return nil, nil
	}
	recs := m.programStatuses[m.programStatusIdx]
	if m.programStatusIdx < len(m.programStatuses)-1 {
		m.programStatusIdx++
	}
	return recs, nil
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
			name:     "Positive input: Which option prompt",
			line:     "? Which option would you like to select?",
			oldInput: true,
			newInput: true,
		},
		{
			name:     "Positive input: Bracket y/n prompt",
			line:     "[y/n] Proceed with migration?",
			oldInput: true,
			newInput: true,
		},
		{
			name:     "Regression: Go test no test files line",
			line:     "?       github.com/tortoise10h/buddy/internal/backend    [no test files]",
			oldInput: true,  // Old regex suffered false-positive
			newInput: false, // Must be rejected
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

// TestWatch_Positive_YesNoPrompt verifies that leading [y/n] prompts trigger waiting_for_input.
func TestWatch_Positive_YesNoPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		captures: []string{
			"[y/n] Proceed with migration?",
		},
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-yn", filepath.Join(tmpDir, "proj-yn"), "sess_yn", "sess_label_yn", state.TaskState{
			TaskSlug:     "task-yn",
			AgentBlockID: "blk_yn",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-yn",
		"--task", "task-yn",
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
	if resp["summary"] != "[y/n] Proceed with migration?" {
		t.Errorf("summary = %v, want '[y/n] Proceed with migration?'", resp["summary"])
	}

	notifs := recorder.All()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 recorded notification, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].subtitle, "waiting_for_input") {
		t.Errorf("notification subtitle = %q, want containing 'waiting_for_input'", notifs[0].subtitle)
	}
}

// TestWatch_GoTestOutputRegression verifies that go test lines starting with '? <pkg> [no test files]'
// do NOT produce waiting_for_input, and allow ready_for_demo to trigger if a completion report is present.
func TestWatch_GoTestOutputRegression(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		captures: []string{
			"?       github.com/tortoise10h/buddy/internal/backend    [no test files]\n" +
				"ok      github.com/tortoise10h/buddy/internal/backend/rex 0.151s\n" +
				"Task complete!",
		},
	}
	reg := getRegistry()
	reg.Register("rex", engine.NewTerminalEngine(mockBackend))

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-gotest", filepath.Join(tmpDir, "proj-gotest"), "sess_gt", "sess_label_gt", state.TaskState{
			TaskSlug:     "task-gotest",
			AgentBlockID: "blk_gt",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--project", "proj-gotest",
		"--task", "task-gotest",
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

	if resp["event"] != "ready_for_demo" {
		t.Errorf("event = %v, want ready_for_demo (must not be waiting_for_input)", resp["event"])
	}

	notifs := recorder.All()
	if len(notifs) != 1 {
		t.Fatalf("expected 1 recorded notification, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].subtitle, "ready_for_demo") {
		t.Errorf("notification subtitle = %q, want containing 'ready_for_demo'", notifs[0].subtitle)
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

// TestWatch_DetectionCases_TableDriven covers all key watchdog detection scenarios:
// - worker finishing without an explicit completion marker (idle prompt + output)
// - idle prompt after a working turn
// - worker needing input
// - already-terminal short-circuit (idle vs resumed working vs waiting_for_input)
func TestWatch_DetectionCases_TableDriven(t *testing.T) {
	tests := []struct {
		name                string
		initialStatus       string
		initialSummary      string
		captures            []string
		watchAll            bool
		debounce            string
		wantEvent           string
		wantSummaryContains string
		wantAlreadyReported bool
		wantSavedStatus     string
		wantNotifs          int
	}{
		{
			name:          "worker finishes without marker (test results + idle prompt)",
			initialStatus: "running",
			captures: []string{
				"Updated install.sh and init.go to preserve projects_roots.\n" +
					"ok  github.com/tortoise10h/buddy/internal/cli 1.195s\n" +
					"────────────────────────────────────────\n" +
					"│ > │\n" +
					"────────────────────────────────────────\n" +
					"? for shortcuts   auto · 100%",
				"Updated install.sh and init.go to preserve projects_roots.\n" +
					"ok  github.com/tortoise10h/buddy/internal/cli 1.195s\n" +
					"────────────────────────────────────────\n" +
					"│ > │\n" +
					"────────────────────────────────────────\n" +
					"? for shortcuts   auto · 100%",
			},
			debounce:            "2",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "internal/cli 1.195s",
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          1,
		},
		{
			name:          "idle prompt after working turn",
			initialStatus: "running",
			captures: []string{
				"Running command: go test ./...\n⠋ Testing... (esc to cancel)\n> Type a message",
				"All 18 checks passed cleanly.\n❯ ",
				"All 18 checks passed cleanly.\n❯ ",
			},
			debounce:            "2",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "All 18 checks passed cleanly.",
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          1,
		},
		{
			name:          "worker needs input even with TUI footer present",
			initialStatus: "running",
			captures: []string{
				"Which branch should I rebase onto?\n" +
					"1) main\n" +
					"2) develop\n" +
					"────────────────────────────────────────\n" +
					"> \n" +
					"? for shortcuts",
			},
			debounce:            "1",
			wantEvent:           "waiting_for_input",
			wantSummaryContains: "Which branch should I rebase onto?",
			wantSavedStatus:     "waiting_for_input",
			wantNotifs:          1,
		},
		{
			name:                "already-terminal short-circuit when pane is idle",
			initialStatus:       "ready_for_demo",
			initialSummary:      "Previously finished feature",
			captures:            []string{"All done.\n> \n? for shortcuts"},
			debounce:            "1",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Previously finished feature",
			wantAlreadyReported: true,
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          0,
		},
		{
			name:           "cached ready_for_demo does not short-circuit when worker is actively running",
			initialStatus:  "ready_for_demo",
			initialSummary: "Old finished turn",
			captures: []string{
				"Running command: make test\n⠋ Working (esc to cancel)",
				"Refactored watchdog loop.\n> \n? for shortcuts",
			},
			debounce:            "1",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Refactored watchdog loop.",
			wantAlreadyReported: false,
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          1,
		},
		{
			name:           "cached waiting_for_input is re-checked and detects completion",
			initialStatus:  "waiting_for_input",
			initialSummary: "Asked about target branch",
			captures: []string{
				"Rebased onto main cleanly.\n> \n? for shortcuts",
			},
			watchAll:            true,
			debounce:            "1",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Rebased onto main cleanly.",
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          1,
		},
		{
			name:           "watch --all re-checks ready_for_demo task if it resumes working",
			initialStatus:  "ready_for_demo",
			initialSummary: "First turn done",
			captures: []string{
				"Thought for 4s\n⠋ Editing watch.go",
				"Second turn completed.\n❯ ",
			},
			watchAll:            true,
			debounce:            "1",
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Second turn completed.",
			wantSavedStatus:     "ready_for_demo",
			wantNotifs:          1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("BUDDY_DIR", tmpDir)

			recorder := &notificationRecorder{}
			notifyFn = recorder.Record
			defer func() { notifyFn = globalNotificationRecorder.Record }()

			mockBackend := &mockWatchBackend{captures: tc.captures}
			getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend))

			_ = state.MutateState(func(s *state.State) error {
				s.RecordTask("proj-tbl", filepath.Join(tmpDir, "proj-tbl"), "sess_tbl", "proj-tbl", state.TaskState{
					TaskSlug:     "task-tbl",
					AgentBlockID: "blk_tbl",
					Status:       tc.initialStatus,
					LastSummary:  tc.initialSummary,
				})
				return nil
			})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			args := []string{
				"watch",
				"--debounce", tc.debounce,
				"--interval", "0",
				"--timeout", "5",
				"--notify=true",
			}
			if tc.watchAll {
				args = append(args, "--all")
			} else {
				args = append(args, "--project", "proj-tbl", "--task", "task-tbl")
			}
			rootCmd.SetArgs(args)

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("watch execute failed: %v", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
				t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
			}

			if resp["event"] != tc.wantEvent {
				t.Errorf("event = %v, want %v", resp["event"], tc.wantEvent)
			}
			summary, _ := resp["summary"].(string)
			if tc.wantSummaryContains != "" && !strings.Contains(summary, tc.wantSummaryContains) {
				t.Errorf("summary = %q, want containing %q", summary, tc.wantSummaryContains)
			}
			gotAlready, _ := resp["already_reported"].(bool)
			if gotAlready != tc.wantAlreadyReported {
				t.Errorf("already_reported = %v, want %v", gotAlready, tc.wantAlreadyReported)
			}

			st, _ := state.LoadState()
			p, _ := st.FindProject("proj-tbl")
			task, _ := p.FindTask("task-tbl")
			if task.Status != tc.wantSavedStatus {
				t.Errorf("saved task status = %q, want %q", task.Status, tc.wantSavedStatus)
			}

			if len(recorder.All()) != tc.wantNotifs {
				t.Errorf("notifications count = %d, want %d", len(recorder.All()), tc.wantNotifs)
			}
		})
	}
}

// TestWatch_ReconcileRexCache verifies that stale 'missing' entries are recovered when Rex
// still has the session/window (even if session ID changed) and vanished windows are marked missing.
func TestWatch_ReconcileRexCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	recorder := &notificationRecorder{}
	notifyFn = recorder.Record
	defer func() { notifyFn = globalNotificationRecorder.Record }()

	mockBackend := &mockWatchBackend{
		sessions: []backend.SessionInfo{
			{SessionID: "sess_live_new", Name: "proj-sync", Path: filepath.Join(tmpDir, "proj-sync")},
		},
		windowsBySession: map[string][]backend.WindowInfo{
			"sess_live_new": {
				{WindowID: "win_live", Label: "recovered-task", SessionID: "sess_live_new", BlockIDs: []string{"blk_live"}},
			},
		},
		captures: []string{
			"Finished sync work.\n> \n? for shortcuts",
		},
	}
	getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend))

	origSync := syncRexFn
	syncRexFn = func(ctx context.Context) error {
		return reconcileRexWithBackend(ctx, mockBackend)
	}
	defer func() { syncRexFn = origSync }()

	_ = state.MutateState(func(s *state.State) error {
		s.RecordTask("proj-sync", filepath.Join(tmpDir, "proj-sync"), "sess_stale_old", "proj-sync", state.TaskState{
			TaskSlug:     "recovered-task",
			WindowID:     "win_live",
			WindowLabel:  "recovered-task",
			AgentBlockID: "blk_live",
			Status:       "missing",
			LastSummary:  "Rex session not found",
		})
		s.RecordTask("proj-sync", filepath.Join(tmpDir, "proj-sync"), "sess_stale_old", "proj-sync", state.TaskState{
			TaskSlug:     "gone-task",
			WindowID:     "win_gone",
			WindowLabel:  "gone-task",
			AgentBlockID: "blk_gone",
			Status:       "running",
		})
		return nil
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{
		"watch",
		"--all",
		"--debounce", "1",
		"--interval", "0",
		"--timeout", "5",
		"--notify=true",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("watch execute failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
	}

	// Either the vanished running task reports missing, or the recovered task reports ready_for_demo.
	// Verify state was reconciled: session ID updated, recovered-task restored, gone-task marked missing.
	st, _ := state.LoadState()
	p, _ := st.FindProject("proj-sync")
	if p.RexSessionID != "sess_live_new" {
		t.Errorf("RexSessionID = %q, want sess_live_new", p.RexSessionID)
	}
	goneTask, _ := p.FindTask("gone-task")
	if goneTask.Status != "missing" {
		t.Errorf("gone-task status = %q, want missing", goneTask.Status)
	}
	recTask, _ := p.FindTask("recovered-task")
	if recTask.Status == "missing" {
		t.Errorf("recovered-task status remained missing, expected recovery")
	}
}

// TestWatch_ProgramStatus_TableDriven verifies Rex native OSC 7501 program_status
// takes priority over screen text grep, and falls back to screen text grep when absent.
func TestWatch_ProgramStatus_TableDriven(t *testing.T) {
	tests := []struct {
		name                string
		programStatuses     [][]backend.ProgramStatusRecord
		captures            []string
		wantEvent           string
		wantSummaryContains string
	}{
		{
			name: "native working then done overrides screen text",
			programStatuses: [][]backend.ProgramStatusRecord{
				{{State: "working", App: "opencode", Title: "Working on fix"}},
				{{State: "done", App: "opencode", Title: "Fix implemented", Msg: "All checks green"}},
			},
			captures:            []string{"plain screen output without markers"},
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "All checks green",
		},
		{
			name: "native working then idle enriches summary from screen",
			programStatuses: [][]backend.ProgramStatusRecord{
				{{State: "working", App: "opencode"}},
				{{State: "idle", App: "opencode"}},
			},
			captures:            []string{"Updated watch.go and ran tests.\n> "},
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Updated watch.go and ran tests.",
		},
		{
			name: "native blocked triggers waiting_for_input",
			programStatuses: [][]backend.ProgramStatusRecord{
				{{State: "blocked", Kind: "question", App: "opencode", Msg: "Allow running git push?"}},
			},
			captures:            []string{"no question text in pane"},
			wantEvent:           "waiting_for_input",
			wantSummaryContains: "Allow running git push?",
		},
		{
			name: "native error triggers error",
			programStatuses: [][]backend.ProgramStatusRecord{
				{{State: "error", App: "opencode", Msg: "Process crashed"}},
			},
			captures:            []string{""},
			wantEvent:           "error",
			wantSummaryContains: "Process crashed",
		},
		{
			name:                "empty program_status falls back to screen text grep",
			programStatuses:     [][]backend.ProgramStatusRecord{{}},
			captures:            []string{"Tests passed.\n> \n? for shortcuts"},
			wantEvent:           "ready_for_demo",
			wantSummaryContains: "Tests passed.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("BUDDY_DIR", tmpDir)

			recorder := &notificationRecorder{}
			notifyFn = recorder.Record
			defer func() { notifyFn = globalNotificationRecorder.Record }()

			mockBackend := &mockWatchBackend{
				programStatuses: tc.programStatuses,
				captures:        tc.captures,
			}
			getRegistry().Register("rex", engine.NewTerminalEngine(mockBackend))

			_ = state.MutateState(func(s *state.State) error {
				s.RecordTask("proj-ps", filepath.Join(tmpDir, "proj-ps"), "sess_ps", "proj-ps", state.TaskState{
					TaskSlug:     "task-ps",
					AgentBlockID: "blk_ps",
					Status:       "running",
				})
				return nil
			})

			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)
			rootCmd.SetArgs([]string{
				"watch",
				"--project", "proj-ps",
				"--task", "task-ps",
				"--debounce", "1",
				"--interval", "0",
				"--timeout", "5",
				"--notify=true",
			})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("watch execute failed: %v", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
				t.Fatalf("invalid json: %v, raw: %s", err, buf.String())
			}

			if resp["event"] != tc.wantEvent {
				t.Errorf("event = %v, want %v", resp["event"], tc.wantEvent)
			}
			summary, _ := resp["summary"].(string)
			if tc.wantSummaryContains != "" && !strings.Contains(summary, tc.wantSummaryContains) {
				t.Errorf("summary = %q, want containing %q", summary, tc.wantSummaryContains)
			}
		})
	}
}
