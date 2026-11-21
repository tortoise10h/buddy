package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backend/opencode"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

func TestReconcileOpenCodeWithEngine_FilteringAndCap(t *testing.T) {
	t.Setenv("BUDDY_DIR", t.TempDir())

	fixedNow := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedNow }
	t.Cleanup(func() { nowFn = origNow })

	projDir := filepath.Join(t.TempDir(), "myapp")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Seed state with one tracked active task and one tracked missing task.
	err := state.MutateStateForEngine("opencode", func(s *state.State) error {
		s.RecordTask("myapp", projDir, "proj_1", "myapp", state.TaskState{
			TaskSlug:     "tracked-active",
			Engine:       "opencode",
			WindowID:     "ses_tracked",
			WindowLabel:  "tracked-active",
			AgentBlockID: "ses_tracked",
			Status:       "running",
		})
		s.RecordTask("myapp", projDir, "proj_1", "myapp", state.TaskState{
			TaskSlug:     "tracked-gone",
			Engine:       "opencode",
			WindowID:     "ses_gone",
			WindowLabel:  "tracked-gone",
			AgentBlockID: "ses_gone",
			Status:       "running",
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Build 15 untracked sessions:
	// - 1 old (>24h) but ACTIVE (must be included!)
	// - 1 old (>24h) and IDLE (must be excluded!)
	// - 1 child session with ParentID (must be excluded!)
	// - 1 archived session (must be excluded!)
	// - 12 recent (<24h) idle sessions (with active one = 13 eligible -> capped at 10!)
	sessions := []opencode.SessionInfo{
		{ID: "ses_tracked", ProjectID: "proj_1", Title: "Tracked", Time: opencode.SessionTime{Updated: fixedNow.UnixMilli()}},
		{ID: "ses_old_active", ProjectID: "proj_1", Title: "Old but active", Time: opencode.SessionTime{Updated: fixedNow.Add(-48 * time.Hour).UnixMilli()}},
		{ID: "ses_old_idle", ProjectID: "proj_1", Title: "Old idle", Outcome: "succeeded", Time: opencode.SessionTime{Updated: fixedNow.Add(-48 * time.Hour).UnixMilli()}},
		{ID: "ses_child", ProjectID: "proj_1", ParentID: "ses_tracked", Title: "Subagent", Time: opencode.SessionTime{Updated: fixedNow.UnixMilli()}},
		{ID: "ses_archived", ProjectID: "proj_1", Title: "Archived", Time: opencode.SessionTime{Updated: fixedNow.UnixMilli(), Archived: fixedNow.UnixMilli()}},
	}
	for i := 1; i <= 12; i++ {
		sessions = append(sessions, opencode.SessionInfo{
			ID:        fmt.Sprintf("ses_recent_%02d", i),
			ProjectID: "proj_1",
			Title:     fmt.Sprintf("Recent %02d", i),
			Outcome:   "succeeded",
			Time: opencode.SessionTime{
				Updated: fixedNow.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			},
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/session/active":
			_, _ = w.Write([]byte(`{"data":{"ses_old_active":{"type":"running"}}}`))
		case "/api/session/ses_tracked":
			_, _ = w.Write([]byte(`{"data":{"id":"ses_tracked","projectID":"proj_1","title":"Tracked","outcome":"succeeded"}}`))
		case "/api/session/ses_gone":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"_tag":"NotFoundError","message":"not found"}`))
		case "/api/session/ses_tracked/permission", "/api/session/ses_tracked/form":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/api/session/ses_tracked/message":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","type":"assistant","content":[{"type":"text","text":"All done!"}]}]}`))
		case "/api/session":
			resp := map[string]any{"data": sessions}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	eng := engine.NewOpenCodeEngine(
		[]opencode.ClientOption{opencode.WithEndpoint(opencode.EndpointConfig{URL: srv.URL})},
		engine.OpenCodeEngineOptions{},
	)

	if err := reconcileOpenCodeWithEngine(context.Background(), eng); err != nil {
		t.Fatalf("reconcileOpenCodeWithEngine failed: %v", err)
	}

	st, err := state.LoadStateForEngine("opencode")
	if err != nil {
		t.Fatal(err)
	}
	proj := st.Projects["myapp"]

	if proj.Tasks["tracked-active"].Status != "ready_for_demo" {
		t.Fatalf("expected tracked-active -> ready_for_demo, got %q", proj.Tasks["tracked-active"].Status)
	}
	if proj.Tasks["tracked-active"].LastSummary != "All done!" {
		t.Fatalf("expected summary 'All done!', got %q", proj.Tasks["tracked-active"].LastSummary)
	}
	if proj.Tasks["tracked-gone"].Status != "missing" {
		t.Fatalf("expected tracked-gone -> missing, got %q", proj.Tasks["tracked-gone"].Status)
	}

	// Untracked sessions must be capped at 10, newest first, and exclude old_idle/child/archived/tracked.
	if len(proj.UntrackedWindows) != openCodeUntrackedMaxPerProject {
		t.Fatalf("expected %d untracked windows, got %d: %+v", openCodeUntrackedMaxPerProject, len(proj.UntrackedWindows), proj.UntrackedWindows)
	}
	for _, uw := range proj.UntrackedWindows {
		if uw.WindowID == "ses_old_idle" || uw.WindowID == "ses_child" || uw.WindowID == "ses_archived" || uw.WindowID == "ses_tracked" {
			t.Fatalf("unexpected filtered session surfaced: %q", uw.WindowID)
		}
	}
	if proj.UntrackedWindows[0].WindowID != "ses_recent_01" {
		t.Fatalf("expected newest session first (ses_recent_01), got %q", proj.UntrackedWindows[0].WindowID)
	}
}

func TestCLI_OpenCodeFullLifecycle(t *testing.T) {
	buddyDir := t.TempDir()
	t.Setenv("BUDDY_DIR", buddyDir)
	t.Setenv("BUDDY_ENGINE", "opencode")

	projRoot := t.TempDir()
	projDir := filepath.Join(projRoot, "demoapp")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DefaultEngine = "opencode"
	cfg.ProjectsRoots = []string{projRoot}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	active := map[string]opencode.SessionActive{}
	interrupted := false
	prompts := []string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			active["ses_live1"] = opencode.SessionActive{Type: "running"}
			_, _ = w.Write([]byte(`{"data":{"id":"ses_live1","projectID":"proj_demo","title":"feat-login","agent":"build"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_live1/prompt":
			var req opencode.PromptRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			prompts = append(prompts, req.Text)
			_, _ = w.Write([]byte(`{"data":{"id":"in_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/active":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": active})
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_live1":
			_, _ = w.Write([]byte(`{"data":{"id":"ses_live1","projectID":"proj_demo","title":"feat-login","agent":"build","outcome":"succeeded"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_live1/message":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","type":"user","text":"build login"},{"id":"m2","type":"assistant","content":[{"type":"text","text":"Login implemented."}]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_live1/interrupt":
			interrupted = true
			delete(active, "ses_live1")
			_, _ = w.Write([]byte(`{"interrupted":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_oob1":
			_, _ = w.Write([]byte(`{"data":{"id":"ses_oob1","projectID":"proj_demo","title":"Manual desktop chat","agent":"build","outcome":"succeeded"}}`))
		case r.Method == http.MethodGet && (r.URL.Path == "/api/session/ses_oob1/permission" || r.URL.Path == "/api/session/ses_oob1/form"):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_oob1/message":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","type":"assistant","content":[{"type":"text","text":"Ready in Desktop."}]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session":
			_, _ = w.Write([]byte(`{"data":[{"id":"ses_oob1","projectID":"proj_demo","title":"Manual desktop chat","agent":"build","outcome":"succeeded"}]}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()

	// Inject test OpenCodeEngine into registry.
	origReg := defaultRegistry
	origSync := syncOpenCodeFn
	t.Cleanup(func() {
		defaultRegistry = origReg
		syncOpenCodeFn = origSync
	})
	ocEng := engine.NewOpenCodeEngine(
		[]opencode.ClientOption{opencode.WithEndpoint(opencode.EndpointConfig{URL: srv.URL})},
		engine.OpenCodeEngineOptions{FocusDesktop: false},
	)
	reg := engine.NewRegistry("opencode")
	reg.Register("opencode", ocEng)
	defaultRegistry = reg
	syncOpenCodeFn = func(ctx context.Context) error { return nil }

	runCmd := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetErr(&out)
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("command %v failed: %v\noutput: %s", args, err, out.String())
		}
		return out.String()
	}

	// 1. Dispatch
	dispOut := runCmd("dispatch", "-p", "demoapp", "-t", "feat-login", "-m", "build login", "--engine", "opencode")
	if !strings.Contains(dispOut, `"window_id": "ses_live1"`) {
		t.Fatalf("unexpected dispatch output:\n%s", dispOut)
	}

	// 2. Send follow-up
	_ = runCmd("send", "-p", "demoapp", "-t", "feat-login", "--text", "add tests", "--engine", "opencode")
	mu.Lock()
	if len(prompts) != 2 || prompts[1] != "add tests" {
		t.Fatalf("expected 2 prompts, got %+v", prompts)
	}
	mu.Unlock()

	// 3. Close (interrupts active session non-destructively and marks closed)
	_ = runCmd("task", "close", "-p", "demoapp", "-t", "feat-login", "--engine", "opencode")
	mu.Lock()
	if !interrupted {
		t.Fatalf("expected active session to be interrupted on task close")
	}
	mu.Unlock()

	// 4. Clean
	cleanOut := runCmd("task", "clean", "-p", "demoapp", "-t", "feat-login", "--reason", "done", "--engine", "opencode")
	if !strings.Contains(cleanOut, `"status": "cleaned"`) {
		t.Fatalf("unexpected clean output:\n%s", cleanOut)
	}

	// 5. Adopt out-of-band Desktop session
	adoptOut := runCmd("task", "adopt", "-p", "demoapp", "-w", "ses_oob1", "-t", "desktop-chat", "--engine", "opencode")
	if !strings.Contains(adoptOut, `"status": "adopted"`) || !strings.Contains(adoptOut, `"window_id": "ses_oob1"`) {
		t.Fatalf("unexpected adopt output:\n%s", adoptOut)
	}
}

func TestCLI_OpenCodeDispatch_CustomModelPreservesDefaultAgent(t *testing.T) {
	buddyDir := t.TempDir()
	t.Setenv("BUDDY_DIR", buddyDir)
	t.Setenv("BUDDY_ENGINE", "opencode")
	t.Setenv("BUDDY_MODEL", "")
	t.Setenv("BUDDY_OPENCODE_MODEL", "")

	projRoot := t.TempDir()
	projDir := filepath.Join(projRoot, "demoapp")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DefaultEngine = "opencode"
	cfg.ProjectsRoots = []string{projRoot}
	ec := cfg.Engines["opencode"]
	ec.DefaultAgent = "build"
	cfg.Engines["opencode"] = ec
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	var gotCreate opencode.CreateSessionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			_ = json.NewDecoder(r.Body).Decode(&gotCreate)
			_, _ = w.Write([]byte(`{"data":{"id":"ses_m1","projectID":"proj_demo","title":"feat-model","agent":"build"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_m1/prompt":
			_, _ = w.Write([]byte(`{"data":{"id":"in_1"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer srv.Close()

	origReg := defaultRegistry
	origSync := syncOpenCodeFn
	t.Cleanup(func() {
		defaultRegistry = origReg
		syncOpenCodeFn = origSync
	})
	ocEng := engine.NewOpenCodeEngine(
		[]opencode.ClientOption{opencode.WithEndpoint(opencode.EndpointConfig{URL: srv.URL})},
		engine.OpenCodeEngineOptions{DefaultAgent: "build", FocusDesktop: false},
	)
	reg := engine.NewRegistry("opencode")
	reg.Register("opencode", ocEng)
	defaultRegistry = reg
	syncOpenCodeFn = func(ctx context.Context) error { return nil }

	var out bytes.Buffer
	cmd := newDispatchCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"-p", "demoapp",
		"-t", "feat-model",
		"-m", "refactor router",
		"--engine", "opencode",
		"--model", "opencode-go/deepseek-v4.1-flash#high",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("opencode dispatch --model failed: %v", err)
	}

	if gotCreate.Agent != "build" {
		t.Fatalf("expected default_agent 'build' preserved, got %q", gotCreate.Agent)
	}
	if gotCreate.Model == nil || gotCreate.Model.ProviderID != "opencode-go" || gotCreate.Model.ID != "deepseek-v4.1-flash" || gotCreate.Model.Variant != "high" {
		t.Fatalf("unexpected ModelRef sent to OpenCode server: %+v", gotCreate.Model)
	}
	if !strings.Contains(out.String(), `"model": "opencode-go/deepseek-v4.1-flash#high"`) {
		t.Fatalf("expected model in dispatch JSON output, got:\n%s", out.String())
	}
}
