package notify

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Runner func(ctx context.Context, name string, args ...string) error

var defaultRunner = func(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// SendDesktopNotification triggers a native macOS notification via osascript.
func SendDesktopNotification(title, subtitle, message, sound string, enabled bool, customRunner ...Runner) bool {
	if !enabled {
		return false
	}
	if runtime.GOOS != "darwin" {
		return false
	}

	r := defaultRunner
	if len(customRunner) > 0 && customRunner[0] != nil {
		r = customRunner[0]
	}

	script := fmt.Sprintf(`display notification "%s" with title "%s"`, escapeAppleScript(message), escapeAppleScript(title))
	if subtitle != "" {
		script += fmt.Sprintf(` subtitle "%s"`, escapeAppleScript(subtitle))
	}
	if sound != "" {
		script += fmt.Sprintf(` sound name "%s"`, escapeAppleScript(sound))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := r(ctx, "osascript", "-e", script)
	return err == nil
}
