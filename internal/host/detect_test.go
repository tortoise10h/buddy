package host

import (
	"reflect"
	"testing"
)

func mockEnv(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestDetectHostFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantKind    HostKind
		wantSignals map[string]string
		wantDetails string
	}{
		{
			name:        "clean unknown environment",
			env:         map[string]string{},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "excluded signal: non-rex TERM_PROGRAM",
			env: map[string]string{
				"TERM_PROGRAM": "iTerm.app",
			},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "excluded signals: HOME and arbitrary paths do not trigger host detection",
			env: map[string]string{
				"HOME":       "/Users/fake/.t3",
				"BUDDY_DIR":  "/Users/fake/.buddy",
				"SOME_TOKEN": "secret",
			},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "rex individual signal: REX_SESSION",
			env: map[string]string{
				"REX_SESSION": "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
			},
			wantKind: HostRex,
			wantSignals: map[string]string{
				"REX_SESSION": "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
			},
			wantDetails: "detected Rex host environment",
		},
		{
			name: "rex individual signal: REX_BLOCK",
			env: map[string]string{
				"REX_BLOCK": "block:01a1cae3-28ab-4123-8765-abcdef123456",
			},
			wantKind: HostRex,
			wantSignals: map[string]string{
				"REX_BLOCK": "block:01a1cae3-28ab-4123-8765-abcdef123456",
			},
			wantDetails: "detected Rex host environment",
		},
		{
			name: "rex individual signal: TERM_PROGRAM=rex",
			env: map[string]string{
				"TERM_PROGRAM": "rex",
			},
			wantKind: HostRex,
			wantSignals: map[string]string{
				"TERM_PROGRAM": "rex",
			},
			wantDetails: "detected Rex host environment",
		},
		{
			name: "rex individual signal: REX_SERVER",
			env: map[string]string{
				"REX_SERVER": "unix:///tmp/rex.sock",
			},
			wantKind: HostRex,
			wantSignals: map[string]string{
				"REX_SERVER": "unix:///tmp/rex.sock",
			},
			wantDetails: "detected Rex host environment",
		},
		{
			name: "rex full signals",
			env: map[string]string{
				"REX_SESSION":  "session:123",
				"REX_BLOCK":    "block:456",
				"TERM_PROGRAM": "rex",
				"REX_SERVER":   "unix:///tmp/rex.sock",
			},
			wantKind: HostRex,
			wantSignals: map[string]string{
				"REX_SESSION":  "session:123",
				"REX_BLOCK":    "block:456",
				"TERM_PROGRAM": "rex",
				"REX_SERVER":   "unix:///tmp/rex.sock",
			},
			wantDetails: "detected Rex host environment",
		},
		{
			name: "t3 individual signal: T3CODE_THREAD_ID",
			env: map[string]string{
				"T3CODE_THREAD_ID": "th_t3code123",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3CODE_THREAD_ID": "th_t3code123",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3_ACP_MCP_ENDPOINT",
			env: map[string]string{
				"T3_ACP_MCP_ENDPOINT": "http://127.0.0.1:3773/acp",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3_ACP_MCP_ENDPOINT": "http://127.0.0.1:3773/acp",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3_ACP_MCP_AUTHORIZATION",
			env: map[string]string{
				"T3_ACP_MCP_AUTHORIZATION": "Bearer token123",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3_ACP_MCP_AUTHORIZATION": "Bearer token123",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3_MCP_URL",
			env: map[string]string{
				"T3_MCP_URL": "http://127.0.0.1:3773/sse",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3_MCP_URL": "http://127.0.0.1:3773/sse",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3_MCP_BEARER_TOKEN",
			env: map[string]string{
				"T3_MCP_BEARER_TOKEN": "token_xyz",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3_MCP_BEARER_TOKEN": "token_xyz",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3_THREAD_ID override",
			env: map[string]string{
				"T3_THREAD_ID": "th_manual_override",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3_THREAD_ID": "th_manual_override",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: TERM_PROGRAM=t3code",
			env: map[string]string{
				"TERM_PROGRAM": "t3code",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"TERM_PROGRAM": "t3code",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3CODE_CLI_PATH",
			env: map[string]string{
				"T3CODE_CLI_PATH": "/Users/sunday/.t3/bin/t3",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3CODE_CLI_PATH": "/Users/sunday/.t3/bin/t3",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: T3CODE_HOME",
			env: map[string]string{
				"T3CODE_HOME": "/Users/sunday/.t3",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3CODE_HOME": "/Users/sunday/.t3",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: __CFBundleIdentifier",
			env: map[string]string{
				"__CFBundleIdentifier": "com.t3tools.t3code",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"__CFBundleIdentifier": "com.t3tools.t3code",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "t3 individual signal: __CFBundleIdentifier nightly prefix",
			env: map[string]string{
				"__CFBundleIdentifier": "com.t3tools.t3code.nightly",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"__CFBundleIdentifier": "com.t3tools.t3code.nightly",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "excluded signal: non-t3 __CFBundleIdentifier",
			env: map[string]string{
				"__CFBundleIdentifier": "com.googlecode.iterm2",
			},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "t3 individual signal: ANTIGRAVITY_HARNESS_PATH in .t3",
			env: map[string]string{
				"ANTIGRAVITY_HARNESS_PATH": "/Users/sunday/.t3/tools/antigravity-acp/darwin-arm64/versions/123/localharness_external",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"ANTIGRAVITY_HARNESS_PATH": "/Users/sunday/.t3/tools/antigravity-acp/darwin-arm64/versions/123/localharness_external",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "excluded signal: ANTIGRAVITY_HARNESS_PATH outside .t3",
			env: map[string]string{
				"ANTIGRAVITY_HARNESS_PATH": "/opt/antigravity/localharness_external",
			},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "t3 individual signal: GEMINI_HOME in .t3",
			env: map[string]string{
				"GEMINI_HOME": "/Users/sunday/.t3/userdata/providers/antigravity/ac0a3dfd6ddd",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"GEMINI_HOME": "/Users/sunday/.t3/userdata/providers/antigravity/ac0a3dfd6ddd",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "excluded signal: GEMINI_HOME outside .t3",
			env: map[string]string{
				"GEMINI_HOME": "/Users/sunday/.gemini",
			},
			wantKind:    HostUnknown,
			wantSignals: map[string]string{},
			wantDetails: "no known host signals detected",
		},
		{
			name: "t3 live antigravity environment",
			env: map[string]string{
				"T3CODE_CLI_PATH":          "/Users/sunday/.t3/bin/t3",
				"__CFBundleIdentifier":     "com.t3tools.t3code",
				"ANTIGRAVITY_HARNESS_PATH": "/Users/sunday/.t3/tools/antigravity-acp/darwin-arm64/versions/123/localharness_external",
				"GEMINI_HOME":              "/Users/sunday/.t3/userdata/providers/antigravity/123",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"T3CODE_CLI_PATH":          "/Users/sunday/.t3/bin/t3",
				"__CFBundleIdentifier":     "com.t3tools.t3code",
				"ANTIGRAVITY_HARNESS_PATH": "/Users/sunday/.t3/tools/antigravity-acp/darwin-arm64/versions/123/localharness_external",
				"GEMINI_HOME":              "/Users/sunday/.t3/userdata/providers/antigravity/123",
			},
			wantDetails: "detected T3 host environment",
		},
		{
			name: "conflict: both Rex and T3 present -> T3 wins as innermost context",
			env: map[string]string{
				"REX_SESSION":      "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
				"TERM_PROGRAM":     "rex",
				"T3CODE_THREAD_ID": "th_nested123",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"REX_SESSION":      "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
				"TERM_PROGRAM":     "rex",
				"T3CODE_THREAD_ID": "th_nested123",
			},
			wantDetails: "conflicting Rex and T3 signals detected; T3 selected as innermost context",
		},
		{
			name: "conflict: Rex session with T3 integrated terminal TERM_PROGRAM=t3code -> T3 wins",
			env: map[string]string{
				"REX_SESSION":  "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
				"TERM_PROGRAM": "t3code",
			},
			wantKind: HostT3,
			wantSignals: map[string]string{
				"REX_SESSION":  "session:01a18feb-18dd-795a-9fb9-4c959fd018f3",
				"TERM_PROGRAM": "t3code",
			},
			wantDetails: "conflicting Rex and T3 signals detected; T3 selected as innermost context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectHostFromEnv(mockEnv(tt.env))
			if got.Kind != tt.wantKind {
				t.Errorf("DetectHostFromEnv() Kind = %v, want %v", got.Kind, tt.wantKind)
			}
			if !reflect.DeepEqual(got.Signals, tt.wantSignals) {
				t.Errorf("DetectHostFromEnv() Signals = %v, want %v", got.Signals, tt.wantSignals)
			}
			if got.Details != tt.wantDetails {
				t.Errorf("DetectHostFromEnv() Details = %q, want %q", got.Details, tt.wantDetails)
			}
		})
	}
}
