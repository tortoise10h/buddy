package t3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ThreadStartOutput represents the JSON response from `t3 thread start --json`.
type ThreadStartOutput struct {
	ThreadID  string `json:"threadId"`
	ProjectID string `json:"projectId"`
	RunID     string `json:"runId"`
}

// ThreadInfo contains metadata about a thread from `t3 thread show --json`.
type ThreadInfo struct {
	ThreadID            string `json:"threadId"`
	Title               string `json:"title,omitempty"`
	Status              string `json:"status"`
	Model               string `json:"model"`
	PendingRequestCount int    `json:"pendingRequestCount,omitempty"`
}

// MessageInfo represents an individual message from `t3 thread show --json`.
type MessageInfo struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// ThreadShowOutput represents the JSON response from `t3 thread show --json`.
type ThreadShowOutput struct {
	Thread   ThreadInfo    `json:"thread"`
	Messages []MessageInfo `json:"messages"`
}

// ProjectAddOutput represents potential JSON / structured output from `t3 project add`.
type ProjectAddOutput struct {
	ProjectID string `json:"projectId,omitempty"`
	Path      string `json:"path,omitempty"`
	Title     string `json:"title,omitempty"`
}

// ThreadStartOptions specifies parameters for launching a new thread via `t3 thread start`.
type ThreadStartOptions struct {
	ProjectPath string
	Prompt      string
	Provider    string
	Model       string
	Worktree    string
}

// CommandRunner executes commands and returns stdout or an error.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// CommandRunnerFunc adapts a function signature to the CommandRunner interface.
type CommandRunnerFunc func(ctx context.Context, name string, args ...string) (string, error)

// Run implements CommandRunner.
func (f CommandRunnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

// Option configures DefaultCommandRunner.
type Option func(*DefaultCommandRunner)

// WithBinPath sets an explicit path to the t3 binary.
func WithBinPath(p string) Option {
	return func(r *DefaultCommandRunner) {
		r.binPath = p
	}
}

// WithRunner injects a custom CommandRunner implementation (useful for tests).
func WithRunner(cr CommandRunner) Option {
	return func(r *DefaultCommandRunner) {
		r.runner = cr
	}
}

// DefaultCommandRunner executes t3 CLI commands using exec.CommandContext or an injected runner.
type DefaultCommandRunner struct {
	binPath string
	runner  CommandRunner
}

// NewDefaultCommandRunner constructs a DefaultCommandRunner with the given options.
func NewDefaultCommandRunner(opts ...Option) *DefaultCommandRunner {
	r := &DefaultCommandRunner{}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes a command with the provided context, binary name, and arguments.
func (r *DefaultCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if r.runner != nil {
		return r.runner.Run(ctx, name, args...)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), fmt.Errorf("t3 command failed (exit %d): %s %v: %s", exitCode, name, args, errMsg)
	}
	return stdout.String(), nil
}

func (r *DefaultCommandRunner) getBin() (string, error) {
	if r.binPath != "" {
		return r.binPath, nil
	}
	return FindT3Binary()
}

// App focuses a project in the T3 Code desktop window.
func (r *DefaultCommandRunner) App(ctx context.Context, projectPath string) error {
	bin, err := r.getBin()
	if err != nil {
		return err
	}
	return RunApp(ctx, r, bin, projectPath)
}

// ProjectAdd registers a project workspace with T3.
func (r *DefaultCommandRunner) ProjectAdd(ctx context.Context, projectPath string, title string) error {
	bin, err := r.getBin()
	if err != nil {
		return err
	}
	return RunProjectAdd(ctx, r, bin, projectPath, title)
}

// ThreadStart starts an autonomous agent thread.
func (r *DefaultCommandRunner) ThreadStart(ctx context.Context, opts ThreadStartOptions) (*ThreadStartOutput, error) {
	bin, err := r.getBin()
	if err != nil {
		return nil, err
	}
	return RunThreadStart(ctx, r, bin, opts)
}

// ThreadShow inspects thread status and messages.
func (r *DefaultCommandRunner) ThreadShow(ctx context.Context, threadID string) (*ThreadShowOutput, error) {
	bin, err := r.getBin()
	if err != nil {
		return nil, err
	}
	return RunThreadShow(ctx, r, bin, threadID)
}

// ThreadSend delivers follow-up input to an active thread.
func (r *DefaultCommandRunner) ThreadSend(ctx context.Context, threadID string, message string) error {
	bin, err := r.getBin()
	if err != nil {
		return err
	}
	return RunThreadSend(ctx, r, bin, threadID, message)
}

// BuildAppArgs builds argument list for `t3 app <project-path>`.
func BuildAppArgs(projectPath string) []string {
	return []string{"app", projectPath}
}

// BuildProjectAddArgs builds argument list for `t3 project add <path> [--title <title>]`.
func BuildProjectAddArgs(projectPath string, title string) []string {
	args := []string{"project", "add", projectPath}
	if title != "" {
		args = append(args, "--title", title)
	}
	return args
}

// BuildThreadStartArgs builds argument list for `t3 thread start`.
func BuildThreadStartArgs(opts ThreadStartOptions) []string {
	args := []string{"thread", "start", opts.ProjectPath, opts.Prompt}
	if opts.Provider != "" {
		args = append(args, "--provider", opts.Provider)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Worktree != "" {
		args = append(args, "--worktree", opts.Worktree)
	}
	args = append(args, "--json")
	return args
}

// BuildThreadShowArgs builds argument list for `t3 thread show <threadId> --json`.
func BuildThreadShowArgs(threadID string) []string {
	return []string{"thread", "show", threadID, "--json"}
}

// BuildThreadSendArgs builds argument list for `t3 thread send <threadId> <message> --json`.
func BuildThreadSendArgs(threadID string, message string) []string {
	return []string{"thread", "send", threadID, message, "--json"}
}

// ParseThreadStartOutput unmarshals JSON from `t3 thread start --json`.
func ParseThreadStartOutput(raw string) (*ThreadStartOutput, error) {
	var out ThreadStartOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("failed to parse t3 thread start JSON: %w", err)
	}
	return &out, nil
}

// ParseThreadShowOutput unmarshals JSON from `t3 thread show --json`.
func ParseThreadShowOutput(raw string) (*ThreadShowOutput, error) {
	var out ThreadShowOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("failed to parse t3 thread show JSON: %w", err)
	}
	return &out, nil
}

// RunApp executes `t3 app <project-path>`.
func RunApp(ctx context.Context, runner CommandRunner, bin string, projectPath string) error {
	if projectPath == "" {
		return errors.New("project path is required for app focus")
	}
	args := BuildAppArgs(projectPath)
	_, err := runner.Run(ctx, bin, args...)
	return err
}

// RunProjectAdd executes `t3 project add <path> [--title <title>]`.
func RunProjectAdd(ctx context.Context, runner CommandRunner, bin string, projectPath string, title string) error {
	if projectPath == "" {
		return errors.New("project path is required for project add")
	}
	args := BuildProjectAddArgs(projectPath, title)
	_, err := runner.Run(ctx, bin, args...)
	return err
}

// RunThreadStart executes `t3 thread start` and parses the JSON response.
func RunThreadStart(ctx context.Context, runner CommandRunner, bin string, opts ThreadStartOptions) (*ThreadStartOutput, error) {
	if opts.ProjectPath == "" {
		return nil, errors.New("project path is required for thread start")
	}
	if opts.Prompt == "" {
		return nil, errors.New("prompt message is required for thread start")
	}
	args := BuildThreadStartArgs(opts)
	out, err := runner.Run(ctx, bin, args...)
	if err != nil {
		return nil, err
	}
	return ParseThreadStartOutput(out)
}

// RunThreadShow executes `t3 thread show` and parses the JSON response.
func RunThreadShow(ctx context.Context, runner CommandRunner, bin string, threadID string) (*ThreadShowOutput, error) {
	if threadID == "" {
		return nil, errors.New("thread ID is required for thread show")
	}
	args := BuildThreadShowArgs(threadID)
	out, err := runner.Run(ctx, bin, args...)
	if err != nil {
		return nil, err
	}
	return ParseThreadShowOutput(out)
}

// RunThreadSend executes `t3 thread send`.
func RunThreadSend(ctx context.Context, runner CommandRunner, bin string, threadID string, message string) error {
	if threadID == "" {
		return errors.New("thread ID is required for thread send")
	}
	if message == "" {
		return errors.New("message is required for thread send")
	}
	args := BuildThreadSendArgs(threadID, message)
	_, err := runner.Run(ctx, bin, args...)
	return err
}
