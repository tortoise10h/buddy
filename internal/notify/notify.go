package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/config"
)

// SendDesktopNotification sends a native macOS notification via osascript.
// If sound is nil or empty, notification is sent silently.
func SendDesktopNotification(title, subtitle, message, sound string, cfg *config.Config) bool {
	if cfg == nil {
		c := config.LoadConfig()
		cfg = &c
	}

	if !cfg.Notifications.DesktopEnabled {
		return false
	}

	soundName := sound
	if sound != "" && cfg.Notifications.Sound != "" {
		soundName = cfg.Notifications.Sound
	}

	escape := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}

	cmdStr := fmt.Sprintf(`display notification "%s" with title "%s"`, escape(message), escape(title))
	if subtitle != "" {
		cmdStr += fmt.Sprintf(` subtitle "%s"`, escape(subtitle))
	}
	if soundName != "" {
		cmdStr += fmt.Sprintf(` sound name "%s"`, escape(soundName))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "osascript", "-e", cmdStr)
	err := cmd.Run()
	return err == nil
}
