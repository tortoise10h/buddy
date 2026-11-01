package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type ParityResult struct {
	CaseName string
	Command  string
	PyCode   int
	GoCode   int
	Status   string
	Details  string
}

var parityResults []ParityResult

func TestMain(m *testing.M) {
	// Build the Go binary before running tests
	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error locating repo root: %v\n", err)
		os.Exit(1)
	}

	binPath := filepath.Join(repoRoot, "bin", "buddy-ctl")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/buddy-ctl")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Error building Go binary: %v\nOutput: %s\n", err, string(out))
		os.Exit(1)
	}

	exitCode := m.Run()

	// Print summary table
	printSummaryTable()

	os.Exit(exitCode)
}

func printSummaryTable() {
	fmt.Println()
	fmt.Println("==========================================================================================")
	fmt.Println("                           BUDDY-CTL PARITY VERIFICATION SUMMARY                          ")
	fmt.Println("==========================================================================================")
	fmt.Printf("%-34s %-22s %-12s %s\n", "TEST CASE", "COMMAND", "EXIT (PY/GO)", "PARITY RESULT")
	fmt.Println("------------------------------------------------------------------------------------------")
	for _, r := range parityResults {
		fmt.Printf("%-34s %-22s %-12s %s\n",
			r.CaseName,
			r.Command,
			fmt.Sprintf("%d / %d", r.PyCode, r.GoCode),
			r.Status,
		)
	}
	fmt.Println("==========================================================================================")
	fmt.Println()
}

func recordResult(name, command string, pyCode, goCode int, pass bool, details string) {
	status := "MATCH"
	if !pass {
		status = "FAIL"
	}
	parityResults = append(parityResults, ParityResult{
		CaseName: name,
		Command:  command,
		PyCode:   pyCode,
		GoCode:   goCode,
		Status:   status,
		Details:  details,
	})
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills", "buddy", "scripts", "buddy-ctl")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate buddy repository root")
}

func getPaths(t *testing.T) (repoRoot, pyScript, goBin string) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	py := filepath.Join(root, "skills", "buddy", "scripts", "buddy-ctl")
	goB := filepath.Join(root, "bin", "buddy-ctl")
	return root, py, goB
}

func setupTempBuddyEnv(t *testing.T) (tempHome, buddyDir string) {
	tempHome, err := os.MkdirTemp("", "buddy-parity-home-*")
	if err != nil {
		t.Fatalf("failed to create temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempHome) })

	buddyDir = filepath.Join(tempHome, ".buddy")
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		t.Fatalf("failed to create .buddy: %v", err)
	}

	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "parity_state.json")
	stateData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buddyDir, "state.json"), stateData, 0644); err != nil {
		t.Fatalf("failed to write state.json: %v", err)
	}

	configJSON := `{"version": 1, "orchestrator_agent": "agy", "worker_agent": "agy", "projects_roots": ["/tmp"]}`
	if err := os.WriteFile(filepath.Join(buddyDir, "config.json"), []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	return tempHome, buddyDir
}

func createStubRex(t *testing.T, logFile string) (stubDir string) {
	stubDir, err := os.MkdirTemp("", "buddy-parity-stub-*")
	if err != nil {
		t.Fatalf("failed to create stub dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(stubDir) })

	stubScript := fmt.Sprintf(`#!/usr/bin/env python3
import sys, os, json

log_file = %q
with open(log_file, "a") as f:
    f.write(" ".join(sys.argv[1:]) + "\n")

args = sys.argv[1:]
if not args:
    sys.exit(0)

cmd = args[0]
if cmd == "ls":
    print(json.dumps({"sessions": [{"session_id": "session:rex-docnotate", "label": "docnotate", "cwd": "/tmp/test-docnotate"}]}))
    sys.exit(0)

if cmd == "session" and len(args) > 1 and args[1] == "inspect":
    sess_id = args[2]
    print(json.dumps({"session_id": sess_id, "label": "docnotate", "cwd": "/tmp/test-docnotate"}))
    sys.exit(0)

if cmd == "window":
    sub = args[1]
    if sub == "inspect":
        win_id = args[4] if len(args) > 4 else args[2]
        if win_id == "window:auth":
            print(json.dumps({"window_id": win_id, "label": "auth", "block_ids": ["block:auth"]}))
            sys.exit(0)
        sys.exit(1)
    if sub == "new":
        task_slug = args[3] if len(args) > 3 else "test-task"
        print(json.dumps({"window_id": "window:dispatched-1", "block_ids": ["block:dispatched-1"], "label": task_slug}))
        sys.exit(0)
    if sub == "ls":
        print(json.dumps({"windows": [{"window_id": "window:auth", "label": "auth", "focused_block_id": "block:auth"}]}))
        sys.exit(0)
    sys.exit(0)

if cmd == "capture":
    print("? for shortcuts\nauto · Gemini 3.8 Flash\nLine 1: agent initialized\nLine 2: thinking...\nLine 3: reading files\n")
    sys.exit(0)

if cmd in ("send", "send-key", "focus"):
    sys.exit(0)

if cmd == "new":
    print(json.dumps({"session_id": "session:rex-docnotate", "label": args[1]}))
    sys.exit(0)

sys.exit(0)
`, logFile)

	rexPath := filepath.Join(stubDir, "rex")
	if err := os.WriteFile(rexPath, []byte(stubScript), 0755); err != nil {
		t.Fatalf("failed to write stub rex: %v", err)
	}

	return stubDir
}

func runCLI(bin string, isPython bool, stdinPayload string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	var cmd *exec.Cmd
	if isPython {
		fullArgs := append([]string{bin}, args...)
		cmd = exec.Command("python3", fullArgs...)
	} else {
		cmd = exec.Command(bin, args...)
	}

	cmd.Env = env
	if stdinPayload != "" {
		cmd.Stdin = strings.NewReader(stdinPayload)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestParity_ContextInject_NotBuddy(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	payload := `{"workspacePaths": ["/some/other/workspace"]}`

	pyOut, _, pyCode := runCLI(pyScript, true, payload, env, "context-inject")
	goOut, _, goCode := runCLI(goBin, false, payload, env, "context-inject")

	matched := (pyCode == goCode && strings.TrimSpace(pyOut) == "{}" && strings.TrimSpace(goOut) == "{}")
	recordResult("ContextInject_NotBuddy", "context-inject", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("mismatch: pyCode=%d goCode=%d pyOut=%q goOut=%q", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_ContextInject_ByteIdentical(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	payload := fmt.Sprintf(`{"workspacePaths": ["%s"]}`, buddyDir)

	pyOut, _, pyCode := runCLI(pyScript, true, payload, env, "context-inject")

	// Reset seen state so Go has identical diff baseline
	_ = os.Remove(filepath.Join(buddyDir, ".state_seen.json"))

	goOut, _, goCode := runCLI(goBin, false, payload, env, "context-inject")

	matched := (pyCode == goCode && pyOut == goOut)
	recordResult("ContextInject_ByteIdentical", "context-inject", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("byte-identical mismatch!\nPy (len %d):\n%s\nGo (len %d):\n%s\n", len(pyOut), pyOut, len(goOut), goOut)
	}
}

func TestParity_Status_JSON(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "status", "--format", "json")
	goOut, _, goCode := runCLI(goBin, false, "", env, "status", "--format", "json")

	var pyObj, goObj any
	err1 := json.Unmarshal([]byte(pyOut), &pyObj)
	err2 := json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && err1 == nil && err2 == nil && reflect.DeepEqual(pyObj, goObj))
	recordResult("Status_JSON", "status --format json", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("status JSON mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Status_Brief(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "status", "--format", "brief")
	goOut, _, goCode := runCLI(goBin, false, "", env, "status", "--format", "brief")

	matched := (pyCode == goCode && strings.TrimSpace(pyOut) == strings.TrimSpace(goOut))
	recordResult("Status_Brief", "status --format brief", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("status brief mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Status_ProjectFilter(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "status", "-p", "docnotate", "-f", "json")
	goOut, _, goCode := runCLI(goBin, false, "", env, "status", "-p", "docnotate", "-f", "json")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Status_ProjectFilter", "status -p docnotate", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("status filter mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Capture_Success(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "capture", "--project", "docnotate", "--task", "auth")
	goOut, _, goCode := runCLI(goBin, false, "", env, "capture", "--project", "docnotate", "--task", "auth")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Capture_Success", "capture -p doc -t auth", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("capture success mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Capture_LinesLimit(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "capture", "--project", "docnotate", "--task", "auth", "--lines", "2")
	goOut, _, goCode := runCLI(goBin, false, "", env, "capture", "--project", "docnotate", "--task", "auth", "--lines", "2")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Capture_LinesLimit", "capture -n 2", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("capture lines limit mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Capture_NotFound(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	_, pyErr, pyCode := runCLI(pyScript, true, "", env, "capture", "--project", "docnotate", "--task", "nonexistent")
	_, goErr, goCode := runCLI(goBin, false, "", env, "capture", "--project", "docnotate", "--task", "nonexistent")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyErr), &pyObj)
	_ = json.Unmarshal([]byte(goErr), &goObj)

	matched := (pyCode == 1 && goCode == 1 && reflect.DeepEqual(pyObj, goObj))
	recordResult("Capture_NotFound", "capture non-existent", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("capture not found mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyErr, goErr)
	}
}

func TestParity_Dispatch_ArgvSequence(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	// 1. Run Python dispatch with stub rex
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)

	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"dispatch", "--project", "docnotate", "--prompt", "Implement user session refresh", "--task", "session-refresh")

	// Read Python rex argv log
	pyLogBytes, err := os.ReadFile(stubLogPy)
	if err != nil {
		t.Fatalf("failed to read python stub log: %v", err)
	}
	pyArgvSeq := strings.TrimSpace(string(pyLogBytes))

	// 2. Run Go dispatch with stub rex
	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)

	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)

	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"dispatch", "--project", "docnotate", "--prompt", "Implement user session refresh", "--task", "session-refresh")

	// Read Go rex argv log
	goLogBytes, err := os.ReadFile(stubLogGo)
	if err != nil {
		t.Fatalf("failed to read go stub log: %v", err)
	}
	goArgvSeq := strings.TrimSpace(string(goLogBytes))

	argvMatched := (pyArgvSeq == goArgvSeq)
	codeMatched := (pyCode == 0 && goCode == 0)

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)
	jsonMatched := reflect.DeepEqual(pyObj, goObj)

	allMatched := argvMatched && codeMatched && jsonMatched
	recordResult("Dispatch_ArgvSequence", "dispatch (argv seq)", pyCode, goCode, allMatched, "")

	if !allMatched {
		t.Fatalf("dispatch mismatch:\nPyCode=%d GoCode=%d\nArgv Match: %v\nPy Argv:\n%s\nGo Argv:\n%s\nPy JSON:\n%s\nGo JSON:\n%s\n",
			pyCode, goCode, argvMatched, pyArgvSeq, goArgvSeq, pyOut, goOut)
	}
}

func TestParity_UnknownCommand_ExitCode2(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	_, _, pyCode := runCLI(pyScript, true, "", env, "unknown-command")
	_, _, goCode := runCLI(goBin, false, "", env, "unknown-command")

	matched := (pyCode == 2 && goCode == 2)
	recordResult("UnknownCommand_ExitCode2", "unknown-command", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("unknown command exit code mismatch: py=%d go=%d", pyCode, goCode)
	}
}

func TestParity_UnknownFlag_ExitCode2(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	_, _, pyCode := runCLI(pyScript, true, "", env, "status", "--unknown-flag-123")
	_, _, goCode := runCLI(goBin, false, "", env, "status", "--unknown-flag-123")

	matched := (pyCode == 2 && goCode == 2)
	recordResult("UnknownFlag_ExitCode2", "status --unknown-flag", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("unknown flag exit code mismatch: py=%d go=%d", pyCode, goCode)
	}
}
