package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
)

func TestNotifyCmd_DisabledByDefaultAndOptIn(t *testing.T) {
	t.Run("disabled by default when notifications.desktop_enabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("BUDDY_DIR", tmpDir)

		recorder := &notificationRecorder{}
		notifyFn = recorder.Record
		defer func() { notifyFn = globalNotificationRecorder.Record }()

		buf := new(bytes.Buffer)
		cmd := newNotifyCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"-m", "Worker finished"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("notify execute failed: %v", err)
		}

		var resp map[string]any
		if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
		}
		if resp["status"] != "disabled" {
			t.Errorf("status = %v, want disabled", resp["status"])
		}
		if len(recorder.All()) != 0 {
			t.Errorf("expected 0 desktop notifications when disabled by default, got %d", len(recorder.All()))
		}
	})

	t.Run("force flag sends notification even when disabled in config", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("BUDDY_DIR", tmpDir)

		recorder := &notificationRecorder{}
		notifyFn = recorder.Record
		defer func() { notifyFn = globalNotificationRecorder.Record }()

		buf := new(bytes.Buffer)
		cmd := newNotifyCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"-m", "Urgent alert", "--title", "Custom", "--force"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("notify --force execute failed: %v", err)
		}

		var resp map[string]any
		if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
		}
		if resp["status"] != "sent" {
			t.Errorf("status = %v, want sent", resp["status"])
		}
		notifs := recorder.All()
		if len(notifs) != 1 {
			t.Fatalf("expected 1 notification with --force, got %d", len(notifs))
		}
		if notifs[0].title != "Custom" || notifs[0].message != "Urgent alert" || notifs[0].sound != "Glass" {
			t.Errorf("unexpected notification payload: %+v", notifs[0])
		}
	})

	t.Run("enabled in config sends notification and honors configured sound and overrides", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("BUDDY_DIR", tmpDir)

		cfg := config.DefaultConfig()
		cfg.Notifications["desktop_enabled"] = true
		cfg.Notifications["sound"] = "Ping"
		if err := config.SaveConfig(cfg); err != nil {
			t.Fatalf("SaveConfig failed: %v", err)
		}

		recorder := &notificationRecorder{}
		notifyFn = recorder.Record
		defer func() { notifyFn = globalNotificationRecorder.Record }()

		// Default sound comes from config.
		buf := new(bytes.Buffer)
		cmd := newNotifyCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"-m", "Ready for review"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("notify execute failed: %v", err)
		}

		// --no-sound overrides configured sound to empty string.
		buf2 := new(bytes.Buffer)
		cmd2 := newNotifyCmd()
		cmd2.SetOut(buf2)
		cmd2.SetErr(buf2)
		cmd2.SetArgs([]string{"-m", "Silent alert", "--no-sound"})
		if err := cmd2.Execute(); err != nil {
			t.Fatalf("notify --no-sound execute failed: %v", err)
		}

		notifs := recorder.All()
		if len(notifs) != 2 {
			t.Fatalf("expected 2 notifications, got %d", len(notifs))
		}
		if notifs[0].sound != "Ping" {
			t.Errorf("first notification sound = %q, want Ping", notifs[0].sound)
		}
		if notifs[1].sound != "" {
			t.Errorf("second notification sound = %q, want empty string", notifs[1].sound)
		}
	})
}
