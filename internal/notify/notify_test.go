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

func TestSendDesktopNotification_DisabledDoesNotInvokeRunner(t *testing.T) {
	invoked := false
	mockRunner := func(ctx context.Context, name string, args ...string) error {
		invoked = true
		return nil
	}

	if sent := SendDesktopNotification("Title", "Sub", "Msg", "Glass", false, mockRunner); sent {
		t.Fatalf("expected sent=false when enabled=false")
	}
	if invoked {
		t.Fatalf("expected runner not to be invoked when enabled=false")
	}
}
