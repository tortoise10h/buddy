package host

import "testing"

func TestDetectHostFromEnv_OpenCodeSignals(t *testing.T) {
	t.Run("OPENCODE_SESSION_ID", func(t *testing.T) {
		info := DetectHostFromEnv(func(k string) string {
			if k == "OPENCODE_SESSION_ID" {
				return "ses_123"
			}
			return ""
		})
		if info.Kind != HostOpenCode {
			t.Fatalf("expected HostOpenCode, got %q", info.Kind)
		}
		if info.Signals["OPENCODE_SESSION_ID"] != "ses_123" {
			t.Fatalf("expected OPENCODE_SESSION_ID signal, got %+v", info.Signals)
		}
	})

	t.Run("macOS bundle identifier ai.opencode.desktop", func(t *testing.T) {
		info := DetectHostFromEnv(func(k string) string {
			if k == "__CFBundleIdentifier" {
				return "ai.opencode.desktop"
			}
			return ""
		})
		if info.Kind != HostOpenCode {
			t.Fatalf("expected HostOpenCode, got %q", info.Kind)
		}
	})

	t.Run("unrelated bundle identifier stays unknown", func(t *testing.T) {
		info := DetectHostFromEnv(func(k string) string {
			if k == "__CFBundleIdentifier" {
				return "com.apple.Terminal"
			}
			return ""
		})
		if info.Kind != HostUnknown {
			t.Fatalf("expected HostUnknown, got %q", info.Kind)
		}
	})

	t.Run("OpenCode wins over Rex as innermost context", func(t *testing.T) {
		env := map[string]string{
			"REX_SESSION":         "session:abc",
			"OPENCODE_SESSION_ID": "ses_inner",
		}
		info := DetectHostFromEnv(func(k string) string { return env[k] })
		if info.Kind != HostOpenCode {
			t.Fatalf("expected HostOpenCode to win, got %q", info.Kind)
		}
	})
}
