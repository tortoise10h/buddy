package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

func TestTaskCloseCmd_T3(t *testing.T) {
	cases := []struct {
		name          string
		seedSessionID string
		args          []string
		wantProj      string
		wantTask      string
		wantWindowID  string
	}{
		{
			name:          "by project and task with session id",
			seedSessionID: "proj-1",
			args:          []string{"-p", "buddy", "-t", "feat-a", "--engine", "t3"},
			wantProj:      "buddy",
			wantTask:      "feat-a",
			wantWindowID:  "th_a",
		},
		{
			name:          "by project and task without session id",
			seedSessionID: "",
			args:          []string{"-p", "buddy", "-t", "feat-b", "--engine", "t3"},
			wantProj:      "buddy",
			wantTask:      "feat-b",
			wantWindowID:  "th_b",
		},
		{
			name:          "by window/thread id only without project or session",
			seedSessionID: "",
			args:          []string{"-w", "th_c", "--engine", "t3"},
			wantProj:      "",
			wantTask:      "",
			wantWindowID:  "th_c",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAllHostSignals(t)
			t.Setenv("BUDDY_DIR", t.TempDir())
			_ = config.SaveConfig(config.DefaultConfig())

			taskSlug := tc.wantTask
			if taskSlug == "" {
				taskSlug = "feat-c"
			}
			if err := state.MutateStateForEngine("t3", func(s *state.State) error {
				s.RecordTask("buddy", "/tmp/buddy", tc.seedSessionID, "buddy", state.TaskState{
					TaskSlug:     taskSlug,
					Engine:       "t3",
					WindowID:     tc.wantWindowID,
					AgentBlockID: tc.wantWindowID,
					Status:       "running",
				})
				return nil
			}); err != nil {
				t.Fatalf("seed state failed: %v", err)
			}

			cmd := newTaskCloseCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("task close failed: %v", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse JSON response: %v (output: %q)", err, out.String())
			}
			if resp["status"] != "closed" {
				t.Errorf("expected response status 'closed', got %v", resp["status"])
			}
			if resp["window_id"] != tc.wantWindowID {
				t.Errorf("expected response window_id %q, got %v", tc.wantWindowID, resp["window_id"])
			}

			st, err := state.LoadStateForEngine("t3")
			if err != nil {
				t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
			}
			gotTask, ok := st.Projects["buddy"].Tasks[taskSlug]
			if !ok {
				t.Fatalf("expected task %q in t3 state", taskSlug)
			}
			if gotTask.Status != "closed" {
				t.Errorf("expected persisted task status 'closed', got %q", gotTask.Status)
			}
			if isLiveOpenTaskStatus(gotTask.Status) {
				t.Errorf("expected closed task not to be considered live open")
			}
		})
	}
}

func TestTaskCloseCmd_RexStillRequiresSessionID(t *testing.T) {
	clearAllHostSignals(t)
	t.Setenv("BUDDY_DIR", t.TempDir())
	_ = config.SaveConfig(config.DefaultConfig())

	if err := state.MutateStateForEngine("rex", func(s *state.State) error {
		s.RecordTask("buddy", "/tmp/buddy", "", "buddy", state.TaskState{
			TaskSlug:     "feat-rex",
			Engine:       "rex",
			WindowID:     "win_1",
			AgentBlockID: "blk_1",
			Status:       "running",
		})
		return nil
	}); err != nil {
		t.Fatalf("seed rex state failed: %v", err)
	}

	cmd := newTaskCloseCmd()
	cmd.SetArgs([]string{"-p", "buddy", "-t", "feat-rex", "--engine", "rex"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing session_id") {
		t.Fatalf("expected missing session_id error for rex without session, got: %v", err)
	}
}
