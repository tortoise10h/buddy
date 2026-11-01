package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/backends"
)

var (
	readinessIndicators = []string{
		"? for shortcuts",
		"auto ·",
		"Ask anything",
		"Type a message",
		"Gemini",
		"OpenCode",
		"Anthropic",
		"Claude",
	}
	promptLineRe = regexp.MustCompile(`(?m)^[>❯]\s*$`)
)

type RexBackend struct {
	binPath string
}

func NewRexBackend() *RexBackend {
	return &RexBackend{}
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

func (r *RexBackend) SetRexBin(bin string) {
	r.binPath = bin
}

func (r *RexBackend) runRex(ctx context.Context, args ...string) (string, error) {
	bin := r.getRexBin()
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return stdout.String(), &backends.BackendExecutionError{
			Backend:    "rex",
			Command:    bin,
			Args:       args,
			ExitCode:   exitCode,
			Stderr:     strings.TrimSpace(stderr.String()),
			Stdout:     stdout.String(),
			Underlying: err,
		}
	}

	return stdout.String(), nil
}

func (r *RexBackend) Name() string {
	return "rex"
}

func (r *RexBackend) Capabilities() backends.BackendCapabilities {
	return backends.BackendCapabilities{
		SupportsWorktree:     true,
		SupportsSplit:        true,
		CaptureHasScrollback: true,
		NeedsTUIProbe:        true,
		DefaultSubmitDelayMS: 1500,
		SupportsNativeWait:   false,
		SupportsFocus:        true,
	}
}

func (r *RexBackend) IsAvailable(ctx context.Context) bool {
	_, err := r.runRex(ctx, "ls", "--json")
	return err == nil
}

func (r *RexBackend) ListSessions(ctx context.Context) ([]backends.SessionRef, error) {
	out, err := r.runRex(ctx, "ls", "--json")
	if err != nil {
		return nil, err
	}

	var data RexLsOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("failed to parse rex ls JSON: %w", err)
	}

	refs := make([]backends.SessionRef, 0, len(data.Sessions))
	for _, s := range data.Sessions {
		refs = append(refs, backends.SessionRef{
			SessionID: s.SessionID,
			Name:      s.Label,
			Path:      s.Cwd,
		})
	}
	return refs, nil
}

func (r *RexBackend) InspectSession(ctx context.Context, sessionID string) (backends.SessionRef, error) {
	out, err := r.runRex(ctx, "session", "inspect", sessionID, "--json")
	if err != nil {
		return backends.SessionRef{}, err
	}

	var data RexSessionInspectOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backends.SessionRef{}, fmt.Errorf("failed to parse rex session inspect JSON: %w", err)
	}

	return backends.SessionRef{
		SessionID: data.SessionID,
		Name:      data.Label,
		Path:      data.Cwd,
	}, nil
}

func (r *RexBackend) EnsureSession(ctx context.Context, name string, cwd string) (backends.SessionRef, error) {
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
		return backends.SessionRef{}, err
	}

	var data RexSessionInspectOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backends.SessionRef{}, fmt.Errorf("failed to parse rex new session JSON: %w", err)
	}

	return backends.SessionRef{
		SessionID: data.SessionID,
		Name:      name,
		Path:      cwd,
	}, nil
}

func (r *RexBackend) ListWindows(ctx context.Context, sessionID string) ([]backends.WindowRef, error) {
	out, err := r.runRex(ctx, "window", "ls", "-s", sessionID, "--json")
	if err != nil {
		return nil, err
	}

	var data RexWindowLsOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("failed to parse rex window ls JSON: %w", err)
	}

	refs := make([]backends.WindowRef, 0, len(data.Windows))
	for _, w := range data.Windows {
		extra := map[string]any{
			"focused_block_id": w.FocusedBlockID,
		}
		if len(w.BlockIDs) > 0 {
			extra["block_ids"] = w.BlockIDs
		}
		refs = append(refs, backends.WindowRef{
			WindowID:  w.WindowID,
			Label:     w.Label,
			SessionID: sessionID,
			Extra:     extra,
		})
	}
	return refs, nil
}

func (r *RexBackend) InspectWindow(ctx context.Context, sessionID, windowID string) (backends.WindowRef, error) {
	out, err := r.runRex(ctx, "window", "inspect", "-s", sessionID, windowID, "--json")
	if err != nil {
		return backends.WindowRef{}, err
	}

	var data RexWindowNewOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backends.WindowRef{}, fmt.Errorf("failed to parse rex window inspect JSON: %w", err)
	}

	return backends.WindowRef{
		WindowID:  data.WindowID,
		Label:     data.Label,
		SessionID: sessionID,
		Extra: map[string]any{
			"block_ids": data.BlockIDs,
		},
	}, nil
}

func (r *RexBackend) CreateTaskWindow(ctx context.Context, session backends.SessionRef, taskSlug string, cwd string, agentCmd []string) (backends.WindowRef, backends.BlockRef, error) {
	args := []string{
		"window", "new",
		"-s", session.SessionID,
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
		return backends.WindowRef{}, backends.BlockRef{}, err
	}

	var data RexWindowNewOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backends.WindowRef{}, backends.BlockRef{}, fmt.Errorf("failed to parse rex window new JSON: %w", err)
	}

	winRef := backends.WindowRef{
		WindowID:  data.WindowID,
		Label:     taskSlug,
		SessionID: session.SessionID,
		Extra: map[string]any{
			"block_ids": data.BlockIDs,
		},
	}

	blockID := ""
	if len(data.BlockIDs) > 0 {
		blockID = data.BlockIDs[0]
	}

	blockRef := backends.BlockRef{
		BlockID:   blockID,
		WindowID:  data.WindowID,
		SessionID: session.SessionID,
	}

	return winRef, blockRef, nil
}

func (r *RexBackend) SendText(ctx context.Context, sessionID, blockID, text string) error {
	_, err := r.runRex(ctx, "send", "-s", sessionID, "-b", blockID, text)
	return err
}

func (r *RexBackend) SendKey(ctx context.Context, sessionID, blockID, key string) error {
	_, err := r.runRex(ctx, "send-key", "-s", sessionID, "-b", blockID, key)
	return err
}

func (r *RexBackend) Capture(ctx context.Context, sessionID, blockID string, lines int) (backends.CaptureResult, error) {
	out, err := r.runRex(ctx, "capture", "-s", sessionID, "-b", blockID, "--trim", "--unwrap")
	if err != nil {
		return backends.CaptureResult{}, err
	}

	if lines <= 0 {
		return backends.CaptureResult{
			Text:    out,
			IsAlive: true,
		}, nil
	}

	trimmed := out
	if strings.HasSuffix(trimmed, "\r\n") {
		trimmed = strings.TrimSuffix(trimmed, "\r\n")
	} else if strings.HasSuffix(trimmed, "\n") {
		trimmed = strings.TrimSuffix(trimmed, "\n")
	}
	allLines := strings.Split(trimmed, "\n")

	resultLines := allLines
	if len(allLines) > lines {
		resultLines = allLines[len(allLines)-lines:]
	}

	return backends.CaptureResult{
		Text:    strings.Join(resultLines, "\n"),
		Lines:   resultLines,
		IsAlive: true,
	}, nil
}

func (r *RexBackend) Focus(ctx context.Context, sessionID string, windowID string) error {
	_, _ = r.runRex(ctx, "window", "focus", "-s", sessionID, windowID)
	_, err := r.runRex(ctx, "focus", windowID)
	return err
}

func (r *RexBackend) CloseWindow(ctx context.Context, sessionID, windowID string) error {
	_, err := r.runRex(ctx, "window", "close", "-s", sessionID, windowID)
	return err
}

func (r *RexBackend) OpenDevSplit(ctx context.Context, sessionID, blockID, cwd, cmd string) (backends.BlockRef, error) {
	args := []string{
		"block", "split", blockID,
		"-d", "right",
		"-s", "40%",
		"-c", cwd,
		"--keep-open",
		"--json",
		"--",
	}
	parts := strings.Fields(cmd)
	args = append(args, parts...)

	out, err := r.runRex(ctx, args...)
	if err != nil {
		return backends.BlockRef{}, err
	}

	var data RexBlockInspectOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return backends.BlockRef{}, fmt.Errorf("failed to parse rex block split JSON: %w", err)
	}

	return backends.BlockRef{
		BlockID:   data.BlockID,
		SessionID: sessionID,
	}, nil
}

func (r *RexBackend) WaitReady(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error) {
	start := time.Now()
	for time.Since(start) < timeout {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		capRes, err := r.Capture(ctx, sessionID, blockID, 0)
		if err == nil && capRes.Text != "" {
			for _, ind := range readinessIndicators {
				if strings.Contains(capRes.Text, ind) {
					return true, nil
				}
			}
			if promptLineRe.MatchString(capRes.Text) {
				return true, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, nil
}

func (r *RexBackend) DetectPrompt(ctx context.Context, sessionID, blockID string, timeout time.Duration) (bool, error) {
	return r.WaitReady(ctx, sessionID, blockID, timeout)
}
