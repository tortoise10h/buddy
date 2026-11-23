package rex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

func TestRexBackend_Name(t *testing.T) {
	b := NewRexBackend()
	if b.Name() != "rex" {
		t.Fatalf("expected rex, got %s", b.Name())
	}
}

func TestRexBackend_ListSessions(t *testing.T) {
	mockJSON := `{"sessions":[{"session_id":"s_123","label":"my-project","cwd":"/path/to/project"}]}`
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		if len(args) == 2 && args[0] == "ls" && args[1] == "--json" {
			return mockJSON, nil
		}
		t.Fatalf("unexpected call: %v", args)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "s_123" || sessions[0].Name != "my-project" {
		t.Fatalf("unexpected session info: %+v", sessions[0])
	}
}

func TestRexBackend_CreateTaskWindow(t *testing.T) {
	windowNewJSON := `{"window_id":"w_456","block_ids":["b_789"],"label":"test-task"}`

	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.HasPrefix(cmdStr, "window new") {
			return windowNewJSON, nil
		}
		t.Fatalf("unexpected command: %s", cmdStr)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	winID, blockID, err := b.CreateTaskWindow(context.Background(), "s_123", "test-task", "/path/to/project", []string{"agy"})
	if err != nil {
		t.Fatalf("CreateTaskWindow failed: %v", err)
	}
	if winID != "w_456" {
		t.Errorf("expected window w_456, got %s", winID)
	}
	if blockID != "b_789" {
		t.Errorf("expected block b_789, got %s", blockID)
	}
}

func TestRexBackend_Live_ListSessions(t *testing.T) {
	b := NewRexBackend()
	if !b.IsAvailable(context.Background()) {
		t.Skip("rex is not available on this machine")
	}
	sessions, err := b.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("live ListSessions failed: %v", err)
	}
	if len(sessions) == 0 {
		t.Log("rex is available but has 0 active sessions")
		return
	}
	t.Logf("Successfully connected to live Rex! Found %d active sessions:", len(sessions))
	for _, s := range sessions {
		t.Logf("  - %s (ID: %s)", s.Name, s.SessionID)
	}
}

func TestRexBackend_GetProgramStatus(t *testing.T) {
	mockJSON := `{"records":[{"state":"working","app":"opencode","title":"Implementing feature"},{"id":"child","state":"blocked","kind":"question","msg":"Which port?"}]}`
	runner := func(ctx context.Context, name string, args ...string) (string, error) {
		cmdStr := strings.Join(args, " ")
		if strings.Contains(cmdStr, "block call -b blk_1 -s sess_1 program_status {}") {
			return mockJSON, nil
		}
		t.Fatalf("unexpected call: %v", args)
		return "", nil
	}

	b := NewRexBackend(WithRunner(runner))
	recs, err := b.GetProgramStatus(context.Background(), "sess_1", "blk_1")
	if err != nil {
		t.Fatalf("GetProgramStatus failed: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].State != "working" || recs[0].App != "opencode" {
		t.Errorf("unexpected first record: %+v", recs[0])
	}
	if recs[1].State != "blocked" || recs[1].Kind != "question" || recs[1].Msg != "Which port?" {
		t.Errorf("unexpected second record: %+v", recs[1])
	}
}

func TestWrapBracketedPaste(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty string unchanged",
			in:   "",
			want: "",
		},
		{
			name: "wraps text containing exclamation mark",
			in:   "Please upgrade the notes to match this look!",
			want: "\x1b[200~Please upgrade the notes to match this look!\x1b[201~",
		},
		{
			name: "wraps multiline text",
			in:   "line 1!\nline 2",
			want: "\x1b[200~line 1!\nline 2\x1b[201~",
		},
		{
			name: "does not double wrap",
			in:   "\x1b[200~already wrapped!\x1b[201~",
			want: "\x1b[200~already wrapped!\x1b[201~",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrapBracketedPaste(tc.in); got != tc.want {
				t.Errorf("wrapBracketedPaste(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRexBackend_Send(t *testing.T) {
	tests := []struct {
		name      string
		input     backend.Input
		wantCalls [][]string
	}{
		{
			name: "wraps text with exclamation mark in bracketed paste and sends enter",
			input: backend.Input{
				Text:  "Please upgrade the notes to match this look!",
				Enter: true,
			},
			wantCalls: [][]string{
				{"send", "-s", "sess_1", "-b", "blk_1", "\x1b[200~Please upgrade the notes to match this look!\x1b[201~"},
				{"send-key", "-s", "sess_1", "-b", "blk_1", "enter"},
			},
		},
		{
			name: "avoids double wrapping already bracketed paste text",
			input: backend.Input{
				Text:  "\x1b[200~!echo hi\x1b[201~",
				Enter: true,
			},
			wantCalls: [][]string{
				{"send", "-s", "sess_1", "-b", "blk_1", "\x1b[200~!echo hi\x1b[201~"},
				{"send-key", "-s", "sess_1", "-b", "blk_1", "enter"},
			},
		},
		{
			name: "no-enter skips enter key",
			input: backend.Input{
				Text:  "multiline!\nprompt",
				Enter: false,
			},
			wantCalls: [][]string{
				{"send", "-s", "sess_1", "-b", "blk_1", "\x1b[200~multiline!\nprompt\x1b[201~"},
			},
		},
		{
			name: "key-only skips rex send and bracketed paste",
			input: backend.Input{
				Key: "ctrl+c",
			},
			wantCalls: [][]string{
				{"send-key", "-s", "sess_1", "-b", "blk_1", "ctrl+c"},
			},
		},
		{
			name:      "empty input makes no calls",
			input:     backend.Input{},
			wantCalls: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			runner := func(ctx context.Context, name string, args ...string) (string, error) {
				calls = append(calls, append([]string(nil), args...))
				return "", nil
			}

			b := NewRexBackend(WithRunner(runner))
			if err := b.Send(context.Background(), "sess_1", "blk_1", tc.input); err != nil {
				t.Fatalf("Send failed: %v", err)
			}

			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("calls = %#v, want %#v", calls, tc.wantCalls)
			}
		})
	}
}
