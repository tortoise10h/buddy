package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
	"github.com/tortoise10h/buddy/internal/state"
)

type historySyncT3Runner struct {
	threads []t3.ThreadSummary
}

func (r *historySyncT3Runner) ProjectAdd(ctx context.Context, projectPath, title string) error {
	return nil
}
func (r *historySyncT3Runner) ThreadStart(ctx context.Context, opts t3.ThreadStartOptions) (*t3.ThreadStartOutput, error) {
	return &t3.ThreadStartOutput{}, nil
}
func (r *historySyncT3Runner) ThreadShow(ctx context.Context, threadID string) (*t3.ThreadShowOutput, error) {
	return &t3.ThreadShowOutput{}, nil
}
func (r *historySyncT3Runner) ThreadSend(ctx context.Context, threadID, message string) error {
	return nil
}
func (r *historySyncT3Runner) ThreadList(ctx context.Context, projectPath string) (*t3.ThreadListOutput, error) {
	return &t3.ThreadListOutput{Threads: r.threads}, nil
}
func (r *historySyncT3Runner) ThreadSettle(_ context.Context, _ string) error { return nil }

func TestTaskHistoryCmd_FiltersAndLimit(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")

	now := time.Now()
	records := []state.HistoryRecord{
		{
			CleanupVersion: 1,
			CleanupID:      "cln_1",
			Ts:             now.Add(-48 * time.Hour).Format(time.RFC3339Nano),
			Project:        "alpha",
			TaskSlug:       "old-task",
			Engine:         "rex",
			WindowID:       "win-1",
			FinalStatus:    "closed",
			Reason:         "old",
		},
		{
			CleanupVersion: 1,
			CleanupID:      "cln_2",
			Ts:             now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			Project:        "buddy",
			TaskSlug:       "feat-a",
			Engine:         "t3",
			WindowID:       "th-a",
			FinalStatus:    "closed",
			Reason:         "first",
		},
		{
			CleanupVersion: 1,
			CleanupID:      "cln_3",
			Ts:             now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
			Project:        "buddy",
			TaskSlug:       "feat-b",
			Engine:         "t3",
			WindowID:       "th-b",
			FinalStatus:    "done",
			Reason:         "second",
		},
	}
	if err := state.AppendHistoryRecords(records); err != nil {
		t.Fatalf("AppendHistoryRecords failed: %v", err)
	}

	// Filter by project + since + limit=1 -> newest first (feat-b).
	cmd := newTaskHistoryCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"-p", "buddy", "--since", "24h", "--limit", "1", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("task history failed: %v", err)
	}

	var got []state.HistoryRecord
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].TaskSlug != "feat-b" {
		t.Fatalf("got %+v, want single newest record feat-b", got)
	}

	// Table output check.
	tblCmd := newTaskHistoryCmd()
	var tblOut bytes.Buffer
	tblCmd.SetOut(&tblOut)
	tblCmd.SetArgs([]string{"-p", "alpha"})
	if err := tblCmd.Execute(); err != nil {
		t.Fatalf("task history table failed: %v", err)
	}
	if !strings.Contains(tblOut.String(), "old-task") || !strings.Contains(tblOut.String(), "alpha") {
		t.Errorf("unexpected table output: %s", tblOut.String())
	}
}

func TestSyncDedupe_CleanedT3ThreadNotResurfaced(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	t.Setenv("BUDDY_ENGINE", "t3")
	_ = config.SaveConfig(config.DefaultConfig())

	// Seed a closed task in project 'buddy', then clean it (keeping empty project entry).
	seedCleanTestState(t, "t3", "buddy", "feat-cleaned", "closed", "th_cleaned")
	cleanCmd := newTaskCleanCmd()
	var cleanOut bytes.Buffer
	cleanCmd.SetOut(&cleanOut)
	cleanCmd.SetArgs([]string{"-p", "buddy", "-t", "feat-cleaned", "--engine", "t3"})
	if err := cleanCmd.Execute(); err != nil {
		t.Fatalf("task clean failed: %v", err)
	}

	// T3 still lists th_cleaned (not settled) AND a genuinely new out-of-band thread th_new_oob.
	runner := &historySyncT3Runner{
		threads: []t3.ThreadSummary{
			{ThreadID: "th_cleaned", ProjectID: "sess-buddy", Title: "Cleaned task", Status: "completed", Settled: false},
			{ThreadID: "th_new_oob", ProjectID: "sess-buddy", Title: "New manual thread", Status: "running", Settled: false},
		},
	}
	t3Eng := engine.NewT3EngineWithRunner(runner)
	if err := reconcileT3WithEngine(context.Background(), t3Eng); err != nil {
		t.Fatalf("reconcileT3WithEngine failed: %v", err)
	}

	st, err := state.LoadStateForEngine("t3")
	if err != nil {
		t.Fatalf("LoadStateForEngine failed: %v", err)
	}
	proj := st.Projects["buddy"]
	if len(proj.UntrackedWindows) != 1 {
		t.Fatalf("expected 1 untracked window (th_new_oob only), got %+v", proj.UntrackedWindows)
	}
	if proj.UntrackedWindows[0].WindowID != "th_new_oob" {
		t.Errorf("expected th_new_oob, got %s", proj.UntrackedWindows[0].WindowID)
	}
}
