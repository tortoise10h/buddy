package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
)

// Runner executes a command and returns output or error.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

type RexBackend struct {
	binPath string
	runner  Runner
}

func NewRexBackend(opts ...func(*RexBackend)) *RexBackend {
	b := &RexBackend{}
	for _, opt := range opts {
		opt(b)
	}
	if b.runner == nil {
		b.runner = defaultRunner
	}
	return b
}

func WithRunner(r Runner) func(*RexBackend) {
	return func(b *RexBackend) {
		b.runner = r
	}
}

func WithBinPath(p string) func(*RexBackend) {
	return func(b *RexBackend) {
		b.binPath = p
	}
}

func defaultRunner(ctx context.Context, name string, args ...string) (string, error) {
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
		return stdout.String(), fmt.Errorf("rex command failed (exit %d): %s %v: %s", exitCode, name, args, errMsg)
	}
	return stdout.String(), nil
}

func (r *RexBackend) getRexBin() string {
	if r.binPath != "" {
		return r.binPath
	}
	if envBin := os.Getenv("BUDDY_REX_BIN"); envBin != "" {
		return envBin
	}
	if path, err := exec.LookPath("rex"); err == nil {
		return path
	}
	standardPath := "/Users/sunday/Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex"
	if fi, err := os.Stat(standardPath); err == nil && fi.Mode().Perm()&0111 != 0 {
		return standardPath
	}
	return "rex"
}

func (r *RexBackend) runRex(ctx context.Context, args ...string) (string, error) {
	bin := r.getRexBin()
	return r.runner(ctx, bin, args...)
}

func (r *RexBackend) Name() string {
	return "rex"
}

func (r *RexBackend) IsAvailable(ctx context.Context) bool {
	_, err := r.runRex(ctx, "ls", "--json")
	return err == nil
}

type rexSessionItem struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd,omitempty"`
}

type rexLsOutput struct {
	Sessions []rexSessionItem `json:"sessions"`
}

type rexSessionInspectOutput struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd,omitempty"`
}

type rexWindowItem struct {
	WindowID       string   `json:"window_id"`
	Label          string   `json:"label"`
	FocusedBlockID string   `json:"focused_block_id,omitempty"`
	BlockIDs       []string `json:"block_ids,omitempty"`
}

type rexWindowLsOutput struct {
	Windows []rexWindowItem `json:"windows"`
}

type rexWindowNewOutput struct {
	WindowID string   `json:"window_id"`
	BlockIDs []string `json:"block_ids"`
	Label    string   `json:"label,omitempty"`
}

type rexBlockInspectOutput struct {
	BlockID string `json:"block_id"`
	IsAlive bool   `json:"is_alive"`
}

func (r *RexBackend) ListSessions(ctx context.Context) ([]backend.SessionInfo, error) {
	out, err := r.runRex(ctx, "ls", "--json")
	if err != nil {
		return nil, err
	}

	var data rexLsOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("failed to parse rex ls JSON: %w", err)
	}

	res := make([]backend.SessionInfo, 0, len(data.Sessions))
	for _, s := range data.Sessions {
		res = append(res, backend.SessionInfo{
			SessionID: s.SessionID,
			Name:      s.Label,
			Path:      s.Cwd,
		})
	}
	return res, nil
}

func (r *RexBackend) EnsureSession(ctx context.Context, name string, cwd string) (backend.SessionInfo, error) {
	sessions, err := r.ListSessions(ctx)
	if err == nil {
		for _, s := range sessions {
			if strings.EqualFold(s.Name, name) {
				return s, nil
			}
		}
	}

	out, err := r.runRex(ctx, "new", name, "-c", cwd, "--window", "shell", "--json")
	if err != nil {
		return backend.SessionInfo{}, err
	}

	var data rexSessionInspectOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backend.SessionInfo{}, fmt.Errorf("failed to parse rex new session JSON: %w", err)
	}

	return backend.SessionInfo{
		SessionID: data.SessionID,
		Name:      name,
		Path:      cwd,
	}, nil
}

func (r *RexBackend) ListWindows(ctx context.Context, sessionID string) ([]backend.WindowInfo, error) {
	out, err := r.runRex(ctx, "window", "ls", "-s", sessionID, "--json")
	if err != nil {
		return nil, err
	}

	var data rexWindowLsOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("failed to parse rex window ls JSON: %w", err)
	}

	res := make([]backend.WindowInfo, 0, len(data.Windows))
	for _, w := range data.Windows {
		res = append(res, backend.WindowInfo{
			WindowID:  w.WindowID,
			Label:     w.Label,
			SessionID: sessionID,
			BlockIDs:  w.BlockIDs,
		})
	}
	return res, nil
}

func (r *RexBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backend.WindowInfo, error) {
	out, err := r.runRex(ctx, "window", "inspect", windowID, "-s", sessionID, "--json")
	if err != nil {
		return backend.WindowInfo{}, err
	}

	var data rexWindowNewOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backend.WindowInfo{}, fmt.Errorf("failed to parse rex window inspect JSON: %w", err)
	}

	return backend.WindowInfo{
		WindowID:  data.WindowID,
		Label:     data.Label,
		SessionID: sessionID,
		BlockIDs:  data.BlockIDs,
	}, nil
}

func (r *RexBackend) CreateTaskWindow(ctx context.Context, sessionID, taskSlug, cwd string, agentCmd []string) (string, string, error) {
	args := []string{
		"window", "new",
		"-s", sessionID,
		taskSlug,
		"-c", cwd,
		"--keep-open",
		"--focus=false",
		"--json",
		"--",
	}
	args = append(args, agentCmd...)

	out, err := r.runRex(ctx, args...)
	if err != nil {
		return "", "", fmt.Errorf("create task window failed: %w", err)
	}

	var winData rexWindowNewOutput
	if err := json.Unmarshal([]byte(out), &winData); err != nil {
		return "", "", fmt.Errorf("failed to parse rex window new JSON: %w", err)
	}

	blockID := ""
	if len(winData.BlockIDs) > 0 {
		blockID = winData.BlockIDs[0]
	}

	return winData.WindowID, blockID, nil
}

func (r *RexBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (string, error) {
	out, err := r.runRex(ctx, "capture", "-s", sessionID, "-b", blockID, "--trim", "--unwrap")
	if err != nil {
		return "", err
	}

	if lines <= 0 {
		return out, nil
	}

	trimmed := strings.TrimRight(out, "\r\n")
	allLines := strings.Split(trimmed, "\n")
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return strings.Join(allLines, "\n"), nil
}

func (r *RexBackend) Send(ctx context.Context, sessionID, blockID string, input backend.Input) error {
	if input.Text != "" {
		if _, err := r.runRex(ctx, "send", "-s", sessionID, "-b", blockID, input.Text); err != nil {
			return err
		}
	}
	if input.Key != "" {
		if _, err := r.runRex(ctx, "send-key", "-s", sessionID, "-b", blockID, input.Key); err != nil {
			return err
		}
	}
	if input.Enter {
		if _, err := r.runRex(ctx, "send-key", "-s", sessionID, "-b", blockID, "enter"); err != nil {
			return err
		}
	}
	return nil
}

func (r *RexBackend) Close(ctx context.Context, sessionID, windowID string) error {
	_, err := r.runRex(ctx, "window", "close", "-s", sessionID, windowID)
	return err
}

func (r *RexBackend) Focus(ctx context.Context, sessionID, windowID string) error {
	_, _ = r.runRex(ctx, "window", "focus", "-s", sessionID, windowID)
	_, err := r.runRex(ctx, "focus", windowID)
	return err
}

func (r *RexBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (string, error) {
	args := []string{
		"split",
		"-s", sessionID,
		"-b", blockID,
		"--split=right",
		"--ratio", "40",
		"--label", "Demo",
		"-c", cwd,
		"--keep-open",
		"--json",
		"--",
	}
	args = append(args, strings.Fields(cmd)...)

	out, err := r.runRex(ctx, args...)
	if err != nil {
		return "", err
	}

	var data rexBlockInspectOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return "", fmt.Errorf("failed to parse rex split JSON: %w", err)
	}
	return data.BlockID, nil
}

func (r *RexBackend) IsBlockAlive(ctx context.Context, sessionID, blockID string) (bool, error) {
	args := []string{"block", "inspect", blockID, "--json"}
	if sessionID != "" {
		args = append(args, "-s", sessionID)
	}
	_, err := r.runRex(ctx, args...)
	if err != nil {
		return false, nil
	}
	return true, nil
}
