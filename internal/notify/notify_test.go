package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestSendDesktopNotification_Disabled(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationConfig{
			DesktopEnabled: false,
		},
	}

	sent := SendDesktopNotification("Title", "Subtitle", "Message", "Glass", cfg)
	if sent {
		t.Fatalf("expected false when notifications are disabled")
	}
}

func TestSendDesktopNotification_WithStubOsascript(t *testing.T) {
	tempDir := t.TempDir()
	stubScript := filepath.Join(tempDir, "osascript")
	if err := os.WriteFile(stubScript, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", tempDir+":"+os.Getenv("PATH"))

	cfg := &config.Config{
		Notifications: config.NotificationConfig{
			DesktopEnabled: true,
			Sound:          "Glass",
		},
	}

	sent := SendDesktopNotification("Test Title", "Test Sub", "Test Msg", "Glass", cfg)
	if !sent {
		t.Fatalf("expected true with stub osascript")
	}
}
