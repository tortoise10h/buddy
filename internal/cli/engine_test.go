package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/host"
)

func clearAllHostSignals(t *testing.T) {
	t.Helper()
	for _, v := range host.RexSignalVars {
		t.Setenv(v, "")
	}
	for _, v := range host.T3SignalVars {
		t.Setenv(v, "")
	}
	t.Setenv("BUDDY_ENGINE", "")
	t.Setenv("BUDDY_WORKER_BACKEND", "")
}

func captureOutput(t *testing.T, run func() error) (string, string, error) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}

	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	runErr := run()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	outStr := <-outC
	errStr := <-errC

	return outStr, errStr, runErr
}

func TestEngineCmd_RexDetection(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	t.Setenv("REX_SESSION", "session:01a18feb-18dd-795a-9fb9-4c959fd018f3")
	t.Setenv("TERM_PROGRAM", "rex")

	cmd := newEngineCmd()
	stdout, _, err := captureOutput(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("engine cmd failed: %v", err)
	}

	if !strings.Contains(stdout, "Detected Host: rex") {
		t.Errorf("expected 'Detected Host: rex' in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "Resolved Engine: rex (source: host_detection)") {
		t.Errorf("expected 'Resolved Engine: rex (source: host_detection)' in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "REX_SESSION=session:01a18feb-18dd-795a-9fb9-4c959fd018f3") {
		t.Errorf("expected REX_SESSION signal in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "TERM_PROGRAM=rex") {
		t.Errorf("expected TERM_PROGRAM=rex signal in stdout, got %q", stdout)
	}
}

func TestEngineCmd_T3Detection_JSON(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	t.Setenv("T3CODE_THREAD_ID", "th_test123")

	cmd := newEngineCmd()
	cmd.SetArgs([]string{"--json"})

	stdout, _, err := captureOutput(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("engine cmd failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("failed to parse json output: %v, output was %q", err, stdout)
	}

	if payload["detected_host"] != "t3" {
		t.Errorf("expected detected_host = t3, got %v", payload["detected_host"])
	}
	if payload["engine"] != "t3" {
		t.Errorf("expected engine = t3, got %v", payload["engine"])
	}
	if payload["source"] != "host_detection" {
		t.Errorf("expected source = host_detection, got %v", payload["source"])
	}
	signals, ok := payload["signals"].(map[string]any)
	if !ok || signals["T3CODE_THREAD_ID"] != "th_test123" {
		t.Errorf("expected signals to contain T3CODE_THREAD_ID: %v", payload["signals"])
	}
}

func TestEngineCmd_FlagOverride(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	t.Setenv("T3CODE_THREAD_ID", "th_test123")

	cmd := newEngineCmd()
	cmd.SetArgs([]string{"--engine", "rex", "--json"})

	stdout, _, err := captureOutput(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("engine cmd failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("failed to parse json output: %v", err)
	}

	if payload["detected_host"] != "t3" {
		t.Errorf("expected detected_host = t3, got %v", payload["detected_host"])
	}
	if payload["engine"] != "rex" {
		t.Errorf("expected engine = rex (overridden), got %v", payload["engine"])
	}
	if payload["source"] != "flag" {
		t.Errorf("expected source = flag, got %v", payload["source"])
	}
}

func TestEngineCmd_HostAlias(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	cmd := newEngineCmd()
	aliases := cmd.Aliases
	found := false
	for _, a := range aliases {
		if a == "host" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'host' to be an alias of 'engine' command, got %v", aliases)
	}
}

func TestEngineCmd_UnknownHostWarning(t *testing.T) {
	clearAllHostSignals(t)
	tmpDir := t.TempDir()
	t.Setenv("BUDDY_DIR", tmpDir)

	cfg := config.DefaultConfig()
	cfg.DefaultEngine = "rex"
	_ = config.SaveConfig(cfg)

	cmd := newEngineCmd()
	stdout, stderr, err := captureOutput(t, func() error {
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("engine cmd failed: %v", err)
	}

	if !strings.Contains(stdout, "Detected Host: unknown") {
		t.Errorf("expected 'Detected Host: unknown', got %q", stdout)
	}
	wantWarning := "[buddy] host environment unknown (neither Rex nor T3 detected); defaulting to engine 'rex'. Override with --engine <name> or BUDDY_ENGINE."
	if !strings.Contains(stderr, wantWarning) {
		t.Errorf("expected stderr to contain warning %q, got %q", wantWarning, stderr)
	}
}
