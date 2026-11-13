package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuddyLauncher_RunsInCurrentTabRegardlessOfSessionName(t *testing.T) {
	buddyScript, err := filepath.Abs(filepath.Join("..", "..", "bin", "buddy"))
	if err != nil {
		t.Fatalf("failed to resolve bin/buddy path: %v", err)
	}

	tmpHome := t.TempDir()
	tmpBuddyDir := filepath.Join(tmpHome, ".buddy")
	tmpBin := t.TempDir()

	// Mock buddy-ctl to handle init and config get without needing a built binary
	mockBuddyCtl := filepath.Join(tmpBin, "buddy-ctl")
	buddyCtlScript := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  init)
    mkdir -p "$BUDDY_DIR"
    echo '{"status":"ok"}'
    ;;
  config)
    if [ "${2:-}" = "get" ] && [ "${3:-}" = "engines.rex.orchestrator_agent" ]; then
      echo '{"value":"opencode"}'
    elif [ "${2:-}" = "get" ] && [ "${3:-}" = "agents.opencode.command" ]; then
      echo '{"value":["opencode","--flag"]}'
    else
      echo '{"value":null}'
    fi
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(mockBuddyCtl, []byte(buddyCtlScript), 0755); err != nil {
		t.Fatalf("failed to write mock buddy-ctl: %v", err)
	}

	// Mock rex that fails if invoked, proving bin/buddy does not query or switch Rex sessions
	mockRex := filepath.Join(tmpBin, "rex")
	if err := os.WriteFile(mockRex, []byte("#!/usr/bin/env bash\necho 'rex should not be called' >&2\nexit 99\n"), 0755); err != nil {
		t.Fatalf("failed to write mock rex: %v", err)
	}

	// Mock opencode agent that records its cwd and arguments
	mockAgent := filepath.Join(tmpBin, "opencode")
	agentScript := `#!/usr/bin/env bash
echo "CWD=$(pwd -P)"
echo "ARGS=$*"
`
	if err := os.WriteFile(mockAgent, []byte(agentScript), 0755); err != nil {
		t.Fatalf("failed to write mock agent: %v", err)
	}

	cmd := exec.Command(buddyScript, "--", "--model", "pro")
	cmd.Env = []string{
		"HOME=" + tmpHome,
		"BUDDY_DIR=" + tmpBuddyDir,
		"REX_SESSION=session:arbitrary-custom-name",
		"PATH=" + tmpBin + ":" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/buddy failed: %v\noutput: %s", err, string(out))
	}

	output := string(out)
	resolvedBuddyDir, err := filepath.EvalSymlinks(tmpBuddyDir)
	if err != nil {
		resolvedBuddyDir = tmpBuddyDir
	}
	if !strings.Contains(output, "CWD="+resolvedBuddyDir) {
		t.Errorf("expected agent cwd to be %q, got output:\n%s", resolvedBuddyDir, output)
	}
	if !strings.Contains(output, "ARGS=--flag --model pro") {
		t.Errorf("expected agent args '--flag --model pro', got output:\n%s", output)
	}
}

func TestBuddyLauncher_HelpText(t *testing.T) {
	buddyScript, err := filepath.Abs(filepath.Join("..", "..", "bin", "buddy"))
	if err != nil {
		t.Fatalf("failed to resolve bin/buddy path: %v", err)
	}

	cmd := exec.Command(buddyScript, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/buddy --help failed: %v\noutput: %s", err, string(out))
	}

	output := string(out)
	if !strings.Contains(output, "current tab") {
		t.Errorf("expected help text to mention launching in the current tab, got:\n%s", output)
	}
	if strings.Contains(output, "Launch or switch to") {
		t.Errorf("expected old 'Launch or switch to' text to be removed from help, got:\n%s", output)
	}
}
