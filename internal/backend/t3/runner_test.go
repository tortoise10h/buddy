package t3

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// mockRunner records invocations and returns canned responses.
type mockRunner struct {
	recordedName string
	recordedArgs []string
	output       string
	err          error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	m.recordedName = name
	m.recordedArgs = args
	return m.output, m.err
}

func TestBuildThreadStartArgs_Minimal(t *testing.T) {
	opts := ThreadStartOptions{
		ProjectPath: "/path/to/project",
		Prompt:      "Refactor auth logic",
	}
	args := BuildThreadStartArgs(opts)
	expected := []string{"thread", "start", "/path/to/project", "Refactor auth logic", "--json"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %v, got %v", expected, args)
	}
}

func TestBuildThreadStartArgs_Full(t *testing.T) {
	opts := ThreadStartOptions{
		ProjectPath: "/path/to/project",
		Prompt:      "Implement OAuth PKCE",
		Provider:    "antigravity",
		Model:       "gemini-3.8-flash-medium",
		Worktree:    "task-auth-pkce",
	}
	args := BuildThreadStartArgs(opts)
	expected := []string{
		"thread", "start", "/path/to/project", "Implement OAuth PKCE",
		"--provider", "antigravity",
		"--model", "gemini-3.8-flash-medium",
		"--worktree", "task-auth-pkce",
		"--json",
	}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %v, got %v", expected, args)
	}
}

func TestBuildThreadShowArgs(t *testing.T) {
	args := BuildThreadShowArgs("mcp:4a4575e1-00f1-476f-910a-7918528fc75d")
	expected := []string{"thread", "show", "mcp:4a4575e1-00f1-476f-910a-7918528fc75d", "--json"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %v, got %v", expected, args)
	}
}

func TestBuildThreadSendArgs(t *testing.T) {
	args := BuildThreadSendArgs("mcp:4a4575e1-00f1-476f-910a-7918528fc75d", "Proceed with option A")
	expected := []string{"thread", "send", "mcp:4a4575e1-00f1-476f-910a-7918528fc75d", "Proceed with option A", "--json"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %v, got %v", expected, args)
	}
}

func TestBuildProjectAddArgs(t *testing.T) {
	// Without title
	args1 := BuildProjectAddArgs("/path/to/project", "")
	expected1 := []string{"project", "add", "/path/to/project"}
	if !reflect.DeepEqual(args1, expected1) {
		t.Errorf("expected %v, got %v", expected1, args1)
	}

	// With title
	args2 := BuildProjectAddArgs("/path/to/project", "My Cool App")
	expected2 := []string{"project", "add", "/path/to/project", "--title", "My Cool App"}
	if !reflect.DeepEqual(args2, expected2) {
		t.Errorf("expected %v, got %v", expected2, args2)
	}
}

func TestBuildAppArgs(t *testing.T) {
	args := BuildAppArgs("/path/to/project")
	expected := []string{"app", "/path/to/project"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %v, got %v", expected, args)
	}
}

func TestParseThreadStartOutput_PlanJSON(t *testing.T) {
	planJSON := `{
  "threadId": "mcp:4a4575e1-00f1-476f-910a-7918528fc75d",
  "projectId": "fbef82fa-9298-410e-86e3-4bfb329730b3",
  "runId": "run:thread:mcp%3A4a4575e1-00f1-476f-910a-7918528fc75d:ordinal:1"
}`

	out, err := ParseThreadStartOutput(planJSON)
	if err != nil {
		t.Fatalf("ParseThreadStartOutput failed: %v", err)
	}
	if out.ThreadID != "mcp:4a4575e1-00f1-476f-910a-7918528fc75d" {
		t.Errorf("unexpected threadId: %s", out.ThreadID)
	}
	if out.ProjectID != "fbef82fa-9298-410e-86e3-4bfb329730b3" {
		t.Errorf("unexpected projectId: %s", out.ProjectID)
	}
	if out.RunID != "run:thread:mcp%3A4a4575e1-00f1-476f-910a-7918528fc75d:ordinal:1" {
		t.Errorf("unexpected runId: %s", out.RunID)
	}
}

func TestParseThreadShowOutput_PlanJSON(t *testing.T) {
	planJSON := `{
  "thread": {
    "threadId": "mcp:4a4575e1-00f1-476f-910a-7918528fc75d",
    "status": "running",
    "model": "gemini-3.8-flash-medium"
  },
  "messages": [
    {"role": "user", "text": "Hello from Buddy!..."},
    {"role": "assistant", "text": "Hello! How can I help you today?"}
  ]
}`

	out, err := ParseThreadShowOutput(planJSON)
	if err != nil {
		t.Fatalf("ParseThreadShowOutput failed: %v", err)
	}
	if out.Thread.ThreadID != "mcp:4a4575e1-00f1-476f-910a-7918528fc75d" {
		t.Errorf("unexpected threadId: %s", out.Thread.ThreadID)
	}
	if out.Thread.Status != "running" {
		t.Errorf("unexpected status: %s", out.Thread.Status)
	}
	if out.Thread.Model != "gemini-3.8-flash-medium" {
		t.Errorf("unexpected model: %s", out.Thread.Model)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "user" || out.Messages[0].Text != "Hello from Buddy!..." {
		t.Errorf("unexpected message 0: %+v", out.Messages[0])
	}
	if out.Messages[1].Role != "assistant" || out.Messages[1].Text != "Hello! How can I help you today?" {
		t.Errorf("unexpected message 1: %+v", out.Messages[1])
	}
}

func TestParseOutput_InvalidJSON(t *testing.T) {
	_, err := ParseThreadStartOutput("not json")
	if err == nil {
		t.Errorf("expected error for invalid start JSON")
	}

	_, err = ParseThreadShowOutput("not json")
	if err == nil {
		t.Errorf("expected error for invalid show JSON")
	}
}

func TestDefaultCommandRunner_WithMockRunner_ThreadStart(t *testing.T) {
	mockResponse := `{
  "threadId": "mcp:thread-123",
  "projectId": "proj-456",
  "runId": "run-789"
}`
	mock := &mockRunner{output: mockResponse}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	opts := ThreadStartOptions{
		ProjectPath: "/home/user/project",
		Prompt:      "Implement feature",
		Provider:    "antigravity",
		Model:       "gemini-3.8-flash-medium",
		Worktree:    "task-slug",
	}

	out, err := runner.ThreadStart(context.Background(), opts)
	if err != nil {
		t.Fatalf("ThreadStart failed: %v", err)
	}

	// Verify CLI argument generation
	if mock.recordedName != "/usr/local/bin/t3" {
		t.Errorf("expected binary /usr/local/bin/t3, got %s", mock.recordedName)
	}
	expectedArgs := []string{
		"thread", "start", "/home/user/project", "Implement feature",
		"--provider", "antigravity",
		"--model", "gemini-3.8-flash-medium",
		"--worktree", "task-slug",
		"--json",
	}
	if !reflect.DeepEqual(mock.recordedArgs, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, mock.recordedArgs)
	}

	// Verify JSON deserialization
	if out.ThreadID != "mcp:thread-123" || out.ProjectID != "proj-456" || out.RunID != "run-789" {
		t.Errorf("unexpected ThreadStartOutput: %+v", out)
	}
}

func TestDefaultCommandRunner_WithMockRunner_ThreadShow(t *testing.T) {
	mockResponse := `{
  "thread": {
    "threadId": "mcp:thread-123",
    "status": "completed",
    "model": "gemini-3.8-flash-medium"
  },
  "messages": [
    {"role": "user", "text": "Run tests"},
    {"role": "assistant", "text": "All tests passed."}
  ]
}`
	mock := &mockRunner{output: mockResponse}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	out, err := runner.ThreadShow(context.Background(), "mcp:thread-123")
	if err != nil {
		t.Fatalf("ThreadShow failed: %v", err)
	}

	expectedArgs := []string{"thread", "show", "mcp:thread-123", "--json"}
	if !reflect.DeepEqual(mock.recordedArgs, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, mock.recordedArgs)
	}

	if out.Thread.Status != "completed" {
		t.Errorf("expected status completed, got %s", out.Thread.Status)
	}
	if len(out.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(out.Messages))
	}
}

func TestDefaultCommandRunner_WithMockRunner_ThreadSend(t *testing.T) {
	mock := &mockRunner{output: "ok"}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	err := runner.ThreadSend(context.Background(), "mcp:thread-123", "Yes, continue")
	if err != nil {
		t.Fatalf("ThreadSend failed: %v", err)
	}

	expectedArgs := []string{"thread", "send", "mcp:thread-123", "Yes, continue", "--json"}
	if !reflect.DeepEqual(mock.recordedArgs, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, mock.recordedArgs)
	}
}

func TestDefaultCommandRunner_WithMockRunner_ProjectAdd(t *testing.T) {
	mock := &mockRunner{output: "Project added."}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	err := runner.ProjectAdd(context.Background(), "/path/to/project", "MyProject")
	if err != nil {
		t.Fatalf("ProjectAdd failed: %v", err)
	}

	expectedArgs := []string{"project", "add", "/path/to/project", "--title", "MyProject"}
	if !reflect.DeepEqual(mock.recordedArgs, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, mock.recordedArgs)
	}
}

func TestDefaultCommandRunner_WithMockRunner_App(t *testing.T) {
	mock := &mockRunner{output: "Opened in T3 Code."}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	err := runner.App(context.Background(), "/path/to/project")
	if err != nil {
		t.Fatalf("App failed: %v", err)
	}

	expectedArgs := []string{"app", "/path/to/project"}
	if !reflect.DeepEqual(mock.recordedArgs, expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, mock.recordedArgs)
	}
}

func TestDefaultCommandRunner_ValidationErrors(t *testing.T) {
	runner := NewDefaultCommandRunner(WithBinPath("/bin/echo"))

	// Empty project path in ThreadStart
	_, err := runner.ThreadStart(context.Background(), ThreadStartOptions{Prompt: "hello"})
	if err == nil {
		t.Errorf("expected error for empty ProjectPath")
	}

	// Empty prompt in ThreadStart
	_, err = runner.ThreadStart(context.Background(), ThreadStartOptions{ProjectPath: "/path"})
	if err == nil {
		t.Errorf("expected error for empty Prompt")
	}

	// Empty thread ID in ThreadShow
	_, err = runner.ThreadShow(context.Background(), "")
	if err == nil {
		t.Errorf("expected error for empty threadID in ThreadShow")
	}

	// Empty thread ID in ThreadSend
	err = runner.ThreadSend(context.Background(), "", "msg")
	if err == nil {
		t.Errorf("expected error for empty threadID in ThreadSend")
	}

	// Empty message in ThreadSend
	err = runner.ThreadSend(context.Background(), "thread-1", "")
	if err == nil {
		t.Errorf("expected error for empty message in ThreadSend")
	}

	// Empty project path in ProjectAdd
	err = runner.ProjectAdd(context.Background(), "", "title")
	if err == nil {
		t.Errorf("expected error for empty projectPath in ProjectAdd")
	}

	// Empty project path in App
	err = runner.App(context.Background(), "")
	if err == nil {
		t.Errorf("expected error for empty projectPath in App")
	}
}

func TestDefaultCommandRunner_CommandError(t *testing.T) {
	mockErr := errors.New("exit status 1")
	mock := &mockRunner{err: mockErr}
	runner := NewDefaultCommandRunner(
		WithBinPath("/usr/local/bin/t3"),
		WithRunner(mock),
	)

	_, err := runner.ThreadShow(context.Background(), "mcp:123")
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("expected command error to be propagated, got: %v", err)
	}
}

func TestCommandRunnerFunc(t *testing.T) {
	called := false
	var fn CommandRunnerFunc = func(ctx context.Context, name string, args ...string) (string, error) {
		called = true
		return "ok", nil
	}
	out, err := fn.Run(context.Background(), "test")
	if err != nil || out != "ok" || !called {
		t.Errorf("unexpected result from CommandRunnerFunc: out=%s, err=%v", out, err)
	}
}
