package notify

import (
	"context"
	"strings"
	"testing"
)

func TestSendDesktopNotification_CustomRunner(t *testing.T) {
	var invokedCmd string
	var invokedArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		invokedCmd = name
		invokedArgs = args
		return nil
	}

	sent := SendDesktopNotification("Test Title", "Test Subtitle", "Test Message", "Glass", true, mockRunner)
	if !sent {
		t.Fatalf("expected notification to be sent")
	}

	if invokedCmd != "osascript" {
		t.Errorf("expected osascript, got %s", invokedCmd)
	}

	script := strings.Join(invokedArgs, " ")
	if !strings.Contains(script, "Test Title") || !strings.Contains(script, "Test Message") {
		t.Errorf("script missing expected content: %s", script)
	}
}
