package host

import (
	"os"
	"strings"
)

type HostKind string

const (
	HostRex     HostKind = "rex"
	HostT3      HostKind = "t3"
	HostUnknown HostKind = "unknown"
)

type HostInfo struct {
	Kind    HostKind          `json:"kind"`
	Signals map[string]string `json:"signals"`
	Details string            `json:"details,omitempty"`
}

// RexSignalVars lists the environment variable names that indicate a Rex host environment.
var RexSignalVars = []string{
	"REX_SESSION",
	"REX_BLOCK",
	"TERM_PROGRAM",
	"REX_SERVER",
}

// T3SignalVars lists the environment variable names that indicate a T3 host environment.
var T3SignalVars = []string{
	"T3CODE_THREAD_ID",
	"T3CODE_CLI_PATH",
	"T3CODE_HOME",
	"T3_ACP_MCP_ENDPOINT",
	"T3_ACP_MCP_AUTHORIZATION",
	"T3_ACP_MCP_NODE",
	"T3_ACP_MCP_ENTRYPOINT",
	"T3_MCP_URL",
	"T3_MCP_BEARER_TOKEN",
	"T3_PI_RUNTIME_MODE",
	"T3_PI_MCP_EXTENSION_PATH",
	"T3_THREAD_ID",
	"TERM_PROGRAM",
	"__CFBundleIdentifier",
	"ANTIGRAVITY_HARNESS_PATH",
	"GEMINI_HOME",
}

// DetectHostFromEnv is a pure function that inspects the environment using getEnv.
// It never touches the filesystem (~/.t3, ~/.buddy) or makes network calls.
// If both Rex and T3 signals are detected, T3 wins deterministically as the innermost context.
func DetectHostFromEnv(getEnv func(string) string) HostInfo {
	if getEnv == nil {
		getEnv = os.Getenv
	}

	signals := make(map[string]string)
	rexMatches := make(map[string]string)
	t3Matches := make(map[string]string)

	for _, v := range RexSignalVars {
		val := getEnv(v)
		if val == "" {
			continue
		}
		if v == "TERM_PROGRAM" {
			if val == "rex" {
				rexMatches[v] = val
				signals[v] = val
			}
			continue
		}
		rexMatches[v] = val
		signals[v] = val
	}

	for _, v := range T3SignalVars {
		val := getEnv(v)
		if val == "" {
			continue
		}
		if v == "TERM_PROGRAM" {
			if val == "t3code" {
				t3Matches[v] = val
				signals[v] = val
			}
			continue
		}
		if v == "__CFBundleIdentifier" {
			if val == "com.t3tools.t3code" || strings.HasPrefix(val, "com.t3tools.t3code.") {
				t3Matches[v] = val
				signals[v] = val
			}
			continue
		}
		if v == "ANTIGRAVITY_HARNESS_PATH" || v == "GEMINI_HOME" {
			if strings.Contains(val, "/.t3/") || strings.Contains(val, "\\.t3\\") {
				t3Matches[v] = val
				signals[v] = val
			}
			continue
		}
		t3Matches[v] = val
		signals[v] = val
	}

	if len(t3Matches) > 0 {
		details := "detected T3 host environment"
		if len(rexMatches) > 0 {
			details = "conflicting Rex and T3 signals detected; T3 selected as innermost context"
		}
		return HostInfo{
			Kind:    HostT3,
			Signals: signals,
			Details: details,
		}
	}

	if len(rexMatches) > 0 {
		return HostInfo{
			Kind:    HostRex,
			Signals: signals,
			Details: "detected Rex host environment",
		}
	}

	return HostInfo{
		Kind:    HostUnknown,
		Signals: signals,
		Details: "no known host signals detected",
	}
}

// DetectCurrentHost returns HostInfo based on the current process environment.
func DetectCurrentHost() HostInfo {
	return DetectHostFromEnv(os.Getenv)
}
