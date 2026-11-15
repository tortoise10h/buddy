package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend/t3"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/engine"
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

func TestTaskAdoptCmd_T3_AdoptsUntrackedAndDirectThread(t *testing.T) {
	t.Run("adopts untracked thread from ThreadList and clears UntrackedWindows", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "t3")

		cfg := config.DefaultConfig()
		_ = config.SaveConfig(cfg)

		runner := &syncTestT3Runner{
			showByID: map[string]*t3.ThreadShowOutput{
				"th_untracked_1": {
					Thread: t3.ThreadInfo{
						ThreadID: "th_untracked_1",
						Status:   "completed",
					},
					Messages: []t3.MessageInfo{
						{Role: "assistant", Text: "Refactored sync loop"},
					},
				},
			},
			listByPath: map[string]*t3.ThreadListOutput{
				"/tmp/buddy": {
					Threads: []t3.ThreadSummary{
						{
							ThreadID:  "th_untracked_1",
							ProjectID: "proj-t3-99",
							Title:     "Refactor sync loop",
							Status:    "completed",
							Settled:   false,
						},
					},
				},
			},
		}

		reg := getRegistry()
		origT3, getErr := reg.Get("t3")
		t3Eng := engine.NewT3EngineWithRunner(runner)
		reg.Register("t3", t3Eng)
		origSyncT3 := syncT3Fn
		syncT3Fn = func(ctx context.Context) error {
			return reconcileT3WithEngine(ctx, t3Eng)
		}
		t.Cleanup(func() {
			syncT3Fn = origSyncT3
			if getErr == nil && origT3 != nil {
				reg.Register("t3", origT3)
			}
		})

		if err := state.MutateStateForEngine("t3", func(s *state.State) error {
			s.Projects["buddy"] = state.ProjectState{
				Name:         "buddy",
				ProjectPath:  "/tmp/buddy",
				RexSessionID: "proj-t3-99",
				Tasks:        map[string]state.TaskState{},
			}
			return nil
		}); err != nil {
			t.Fatalf("seed project failed: %v", err)
		}

		cmd := newTaskAdoptCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--engine", "t3", "-p", "buddy", "-w", "th_untracked_1", "-t", "sync-refactor"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("task adopt --engine t3 failed: %v", err)
		}

		var resp map[string]any
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse adopt JSON: %v (output: %q)", err, out.String())
		}
		if resp["status"] != "adopted" || resp["origin"] != "user" || resp["window_id"] != "th_untracked_1" || resp["agent_block_id"] != "th_untracked_1" {
			t.Errorf("unexpected adopt JSON response: %+v", resp)
		}

		st, err := state.LoadStateForEngine("t3")
		if err != nil {
			t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
		}
		proj := st.Projects["buddy"]
		if len(proj.UntrackedWindows) != 0 {
			t.Errorf("expected UntrackedWindows cleared after adopt, got %+v", proj.UntrackedWindows)
		}
		adopted, ok := proj.Tasks["sync-refactor"]
		if !ok {
			t.Fatalf("expected task sync-refactor in t3 state, got %+v", proj.Tasks)
		}
		if adopted.Engine != "t3" || adopted.WindowID != "th_untracked_1" || adopted.AgentBlockID != "th_untracked_1" || proj.RexSessionID != "proj-t3-99" || adopted.Origin != "user" {
			t.Errorf("unexpected adopted task fields: %+v (project session=%q)", adopted, proj.RexSessionID)
		}
		if adopted.Status != "ready_for_demo" || adopted.LastSummary != "Refactored sync loop" {
			t.Errorf("expected adopted status/summary from ThreadShow, got %+v", adopted)
		}
	})

	t.Run("falls back to direct ThreadShow when not in UntrackedWindows", func(t *testing.T) {
		clearAllHostSignals(t)
		t.Setenv("BUDDY_DIR", t.TempDir())
		t.Setenv("BUDDY_ENGINE", "t3")

		cfg := config.DefaultConfig()
		_ = config.SaveConfig(cfg)

		runner := &syncTestT3Runner{
			showByID: map[string]*t3.ThreadShowOutput{
				"th_direct_1": {
					Thread: t3.ThreadInfo{
						ThreadID: "th_direct_1",
						Status:   "waiting_for_input",
					},
					Messages: []t3.MessageInfo{
						{Role: "assistant", Text: "Which port should I bind?"},
					},
				},
			},
			showErrByID: map[string]error{},
			listByPath:  map[string]*t3.ThreadListOutput{},
		}

		reg := getRegistry()
		origT3, getErr := reg.Get("t3")
		t3Eng := engine.NewT3EngineWithRunner(runner)
		reg.Register("t3", t3Eng)
		origSyncT3 := syncT3Fn
		syncT3Fn = func(ctx context.Context) error {
			return reconcileT3WithEngine(ctx, t3Eng)
		}
		t.Cleanup(func() {
			syncT3Fn = origSyncT3
			if getErr == nil && origT3 != nil {
				reg.Register("t3", origT3)
			}
		})

		if err := state.MutateStateForEngine("t3", func(s *state.State) error {
			s.Projects["buddy"] = state.ProjectState{
				Name:        "buddy",
				ProjectPath: "/tmp/buddy",
				Tasks:       map[string]state.TaskState{},
			}
			return nil
		}); err != nil {
			t.Fatalf("seed project failed: %v", err)
		}

		cmd := newTaskAdoptCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--engine", "t3", "-p", "buddy", "-w", "th_direct_1", "-t", "direct-port"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("task adopt direct ThreadShow failed: %v", err)
		}

		st, err := state.LoadStateForEngine("t3")
		if err != nil {
			t.Fatalf("LoadStateForEngine(t3) failed: %v", err)
		}
		adopted := st.Projects["buddy"].Tasks["direct-port"]
		if adopted.Engine != "t3" || adopted.WindowID != "th_direct_1" || adopted.AgentBlockID != "th_direct_1" || adopted.Status != "waiting_for_input" {
			t.Errorf("unexpected direct adopted task: %+v", adopted)
		}

		// Unknown thread ID should still return not found error.
		cmdMissing := newTaskAdoptCmd()
		cmdMissing.SetArgs([]string{"--engine", "t3", "-p", "buddy", "-w", "th_nonexistent"})
		if err := cmdMissing.Execute(); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("window %q not found", "th_nonexistent")) {
			t.Errorf("expected not found error for unknown thread, got: %v", err)
		}
	})
}
