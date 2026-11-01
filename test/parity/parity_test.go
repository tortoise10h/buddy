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
	fmt.Printf("%-34s %-24s %-12s %s\n", "TEST CASE", "COMMAND", "EXIT (PY/GO)", "PARITY RESULT")
	fmt.Println("------------------------------------------------------------------------------------------")
	for _, r := range parityResults {
		fmt.Printf("%-34s %-24s %-12s %s\n",
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

	projectsDir := filepath.Join(tempHome, "projects")
	_ = os.MkdirAll(filepath.Join(projectsDir, "docnotate"), 0755)
	_ = os.MkdirAll(filepath.Join(projectsDir, "daily-catch"), 0755)

	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "parity_state.json")
	stateData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buddyDir, "state.json"), stateData, 0644); err != nil {
		t.Fatalf("failed to write state.json: %v", err)
	}

	configJSON := fmt.Sprintf(`{"version": 1, "orchestrator_agent": "agy", "worker_agent": "agy", "projects_roots": [%q]}`, projectsDir)
	if err := os.WriteFile(filepath.Join(buddyDir, "config.json"), []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}

	return tempHome, buddyDir
}

func setupTempBuddyEnvWithGitRepo(t *testing.T) (tempHome, buddyDir, repoPath string) {
	tempHome, buddyDir = setupTempBuddyEnv(t)
	projectsDir := filepath.Join(tempHome, "projects")
	repoPath = filepath.Join(projectsDir, "myrepo")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\nOutput: %s", args, err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
	runGit("commit", "--allow-empty", "-m", "init")

	return tempHome, buddyDir, repoPath
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
    print(json.dumps({"sessions": [
        {"session_id": "session:rex-docnotate", "label": "docnotate", "cwd": "/tmp/test-docnotate"},
        {"session_id": "session:rex-myrepo", "label": "myrepo", "cwd": "/tmp/test-myrepo"}
    ]}))
    sys.exit(0)

if cmd == "session" and len(args) > 1 and args[1] == "inspect":
    sess_id = args[2]
    label = sess_id.split(":")[-1]
    print(json.dumps({"session_id": sess_id, "label": label, "cwd": "/tmp/test"}))
    sys.exit(0)

if cmd == "window":
    sub = args[1]
    if sub == "inspect":
        win_id = args[4] if len(args) > 4 else args[2]
        print(json.dumps({"window_id": win_id, "label": "auth", "block_ids": ["block:auth"]}))
        sys.exit(0)
    if sub == "new":
        task_slug = args[3] if len(args) > 3 else "test-task"
        print(json.dumps({"window_id": "window:dispatched-1", "block_ids": ["block:dispatched-1"], "label": task_slug}))
        sys.exit(0)
    if sub == "ls":
        print(json.dumps({"windows": [{"window_id": "window:auth", "label": "auth", "focused_block_id": "block:auth"}]}))
        sys.exit(0)
    if sub in ("close", "focus"):
        sys.exit(0)
    sys.exit(0)

if cmd == "client" and len(args) > 1 and args[1] == "ls":
    print(json.dumps({"clients": [{"client_id": "client:1", "info": {"kind": "app"}}]}))
    sys.exit(0)

if cmd == "block" and len(args) > 1 and args[1] == "inspect":
    block_id = args[2]
    print(json.dumps({"block_id": block_id, "window_id": "window:auth"}))
    sys.exit(0)

if cmd == "split":
    print(json.dumps({"block_ids": ["block:dev-1"], "block_id": "block:dev-1"}))
    sys.exit(0)

if cmd == "capture":
    print("? for shortcuts\nauto · Gemini 3.8 Flash\nLine 1: agent initialized\nLine 2: thinking...\nLine 3: reading files\n")
    sys.exit(0)

if cmd in ("send", "send-key", "focus", "do"):
    sys.exit(0)

if cmd == "-C":
    sys.exit(0)

if cmd == "new":
    label = args[1] if len(args) > 1 else "default"
    print(json.dumps({"session_id": f"session:rex-{label}", "label": label}))
    sys.exit(0)

sys.exit(0)
`, logFile)

	rexPath := filepath.Join(stubDir, "rex")
	if err := os.WriteFile(rexPath, []byte(stubScript), 0755); err != nil {
		t.Fatalf("failed to write stub rex: %v", err)
	}

	osascriptPath := filepath.Join(stubDir, "osascript")
	_ = os.WriteFile(osascriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755)

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

func normalizeJSON(obj any, tempHome string) any {
	switch val := obj.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			if k == "created_at" || k == "updated_at" {
				res[k] = "MASKED_TIMESTAMP"
			} else {
				res[k] = normalizeJSON(item, tempHome)
			}
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = normalizeJSON(item, tempHome)
		}
		return res
	case string:
		s := val
		if tempHome != "" {
			evalHome, _ := filepath.EvalSymlinks(tempHome)
			if evalHome != "" {
				s = strings.ReplaceAll(s, evalHome, "<TEMP_HOME>")
			}
			s = strings.ReplaceAll(s, tempHome, "<TEMP_HOME>")
			s = strings.ReplaceAll(s, "/private<TEMP_HOME>", "<TEMP_HOME>")
		}
		return s
	default:
		return val
	}
}

func normalizeText(s string, tempHome string) string {
	res := s
	if tempHome != "" {
		evalHome, _ := filepath.EvalSymlinks(tempHome)
		if evalHome != "" {
			res = strings.ReplaceAll(res, evalHome, "<TEMP_HOME>")
		}
		res = strings.ReplaceAll(res, tempHome, "<TEMP_HOME>")
		res = strings.ReplaceAll(res, "/private<TEMP_HOME>", "<TEMP_HOME>")
	}
	return res
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
	pyArgvSeq := normalizeText(strings.TrimSpace(string(pyLogBytes)), tempHomePy)

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
	goArgvSeq := normalizeText(strings.TrimSpace(string(goLogBytes)), tempHomeGo)

	argvMatched := (pyArgvSeq == goArgvSeq)
	codeMatched := (pyCode == 0 && goCode == 0)

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)
	jsonMatched := reflect.DeepEqual(normalizeJSON(pyObj, tempHomePy), normalizeJSON(goObj, tempHomeGo))

	allMatched := argvMatched && codeMatched && jsonMatched
	recordResult("Dispatch_ArgvSequence", "dispatch (argv seq)", pyCode, goCode, allMatched, "")

	if !allMatched {
		t.Fatalf("dispatch mismatch:\nPyCode=%d GoCode=%d\nArgv Match: %v\nPy Argv:\n%s\nGo Argv:\n%s\nPy JSON:\n%s\nGo JSON:\n%s\n",
			pyCode, goCode, argvMatched, pyArgvSeq, goArgvSeq, pyOut, goOut)
	}
}

func TestParity_Config_Show(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo)

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "config", "show")
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "config", "show")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Config_Show", "config show", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("config show mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Config_Get(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "config", "get", "worker_agent")
	goOut, _, goCode := runCLI(goBin, false, "", env, "config", "get", "worker_agent")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Config_Get", "config get worker_agent", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("config get mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Config_Set(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env, "config", "set", "worker_agent", "opencode")
	goOut, _, goCode := runCLI(goBin, false, "", env, "config", "set", "worker_agent", "opencode")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Config_Set", "config set worker_agent", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("config set mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Projects_List(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo)

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "projects")
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "projects")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Projects_List", "projects", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("projects list mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Projects_Query(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo)

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "projects", "--query", "doc")
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "projects", "--query", "doc")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Projects_Query", "projects --query doc", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("projects query mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Projects_Roots(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo)

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "projects", "roots")
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "projects", "roots")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Projects_Roots", "projects roots", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("projects roots mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Projects_AddRemoveRoot(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo)

	newRootPy := filepath.Join(tempHomePy, "new-root")
	_ = os.MkdirAll(newRootPy, 0755)

	newRootGo := filepath.Join(tempHomeGo, "new-root")
	_ = os.MkdirAll(newRootGo, 0755)

	// Add root
	pyOutAdd, _, pyCodeAdd := runCLI(pyScript, true, "", envPy, "projects", "add-root", newRootPy)
	goOutAdd, _, goCodeAdd := runCLI(goBin, false, "", envGo, "projects", "add-root", newRootGo)

	var pyObjAdd, goObjAdd any
	_ = json.Unmarshal([]byte(pyOutAdd), &pyObjAdd)
	_ = json.Unmarshal([]byte(goOutAdd), &goObjAdd)

	pyNormAdd := normalizeJSON(pyObjAdd, tempHomePy)
	goNormAdd := normalizeJSON(goObjAdd, tempHomeGo)

	matchedAdd := (pyCodeAdd == goCodeAdd && reflect.DeepEqual(pyNormAdd, goNormAdd))
	recordResult("Projects_AddRoot", "projects add-root", pyCodeAdd, goCodeAdd, matchedAdd, "")

	// Remove root
	pyOutRm, _, pyCodeRm := runCLI(pyScript, true, "", envPy, "projects", "remove-root", newRootPy)
	goOutRm, _, goCodeRm := runCLI(goBin, false, "", envGo, "projects", "remove-root", newRootGo)

	var pyObjRm, goObjRm any
	_ = json.Unmarshal([]byte(pyOutRm), &pyObjRm)
	_ = json.Unmarshal([]byte(goOutRm), &goObjRm)

	pyNormRm := normalizeJSON(pyObjRm, tempHomePy)
	goNormRm := normalizeJSON(goObjRm, tempHomeGo)

	matchedRm := (pyCodeRm == goCodeRm && reflect.DeepEqual(pyNormRm, goNormRm))
	recordResult("Projects_RemoveRoot", "projects remove-root", pyCodeRm, goCodeRm, matchedRm, "")

	if !matchedAdd || !matchedRm {
		t.Fatalf("add/remove root mismatch: add=%v rm=%v", matchedAdd, matchedRm)
	}
}

func TestParity_EnsureSession(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	stubLogPy := filepath.Join(tempHomePy, "stub.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy, "PATH="+stubDirPy+":"+os.Getenv("PATH"))

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo, "PATH="+stubDirGo+":"+os.Getenv("PATH"))

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "ensure-session", "-p", "docnotate")
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "ensure-session", "-p", "docnotate")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("EnsureSession", "ensure-session -p doc", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("ensure-session mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Task_Start(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	// Run Python task start
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"task", "start", "-p", "docnotate", "-t", "feat-test", "--prompt", "test prompt")

	// Run Go task start
	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)
	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"task", "start", "-p", "docnotate", "-t", "feat-test", "--prompt", "test prompt")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Task_Start", "task start -p doc -t feat", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("task start mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Task_Update(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	stubLogPy := filepath.Join(tempHomePy, "stub.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(), "HOME="+tempHomePy, "BUDDY_DIR="+buddyDirPy, "PATH="+stubDirPy+":"+os.Getenv("PATH"))

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(), "HOME="+tempHomeGo, "BUDDY_DIR="+buddyDirGo, "PATH="+stubDirGo+":"+os.Getenv("PATH"))

	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"task", "update", "-p", "docnotate", "-t", "auth", "-s", "running", "-m", "In progress")
	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"task", "update", "-p", "docnotate", "-t", "auth", "-s", "running", "-m", "In progress")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Task_Update", "task update -p doc -t auth", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("task update mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Task_Close(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"task", "close", "-p", "docnotate", "-t", "auth")
	// For Go, re-add task to state so it can close it too
	fixturePath := filepath.Join("..", "..", "testdata", "fixtures", "parity_state.json")
	stateData, _ := os.ReadFile(fixturePath)
	_ = os.WriteFile(filepath.Join(buddyDir, "state.json"), stateData, 0644)

	goOut, _, goCode := runCLI(goBin, false, "", env,
		"task", "close", "-p", "docnotate", "-t", "auth")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Task_Close", "task close -p doc -t auth", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("task close mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Send_Text(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"send", "-p", "docnotate", "-t", "auth", "--text", "hello world")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"send", "-p", "docnotate", "-t", "auth", "--text", "hello world")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Send_Text", "send --text ...", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("send text mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Send_Key(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"send", "-p", "docnotate", "-t", "auth", "--key", "enter")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"send", "-p", "docnotate", "-t", "auth", "--key", "enter")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Send_Key", "send --key enter", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("send key mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Focus(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"focus", "-w", "window:auth", "-s", "session:rex-docnotate")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"focus", "-w", "window:auth", "-s", "session:rex-docnotate")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Focus", "focus -w win -s sess", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("focus mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Demo(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"demo", "-p", "docnotate", "-t", "auth", "--cmd", "npm run dev")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"demo", "-p", "docnotate", "-t", "auth", "--cmd", "npm run dev")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Demo", "demo -p doc -t auth --cmd", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("demo mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Notify(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"notify", "-m", "Test notification message", "--no-sound")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"notify", "-m", "Test notification message", "--no-sound")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Notify", "notify -m ... --no-sound", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("notify mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Diagnose(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHome, buddyDir := setupTempBuddyEnv(t)
	stubLog := filepath.Join(tempHome, "stub.log")
	stubDir := createStubRex(t, stubLog)

	env := append(os.Environ(),
		"HOME="+tempHome,
		"BUDDY_DIR="+buddyDir,
		"PATH="+stubDir+":"+os.Getenv("PATH"),
	)

	pyOut, _, pyCode := runCLI(pyScript, true, "", env,
		"diagnose", "-p", "docnotate", "-t", "auth", "-n", "50")
	goOut, _, goCode := runCLI(goBin, false, "", env,
		"diagnose", "-p", "docnotate", "-t", "auth", "-n", "50")

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && reflect.DeepEqual(pyObj, goObj))
	recordResult("Diagnose", "diagnose -p doc -t auth", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("diagnose mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Batch(t *testing.T) {
	_, pyScript, goBin := getPaths(t)
	tempHomePy, buddyDirPy := setupTempBuddyEnv(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)

	batchJSON := `[{"project": "docnotate", "task": "batch-1", "prompt": "batch prompt"}]`
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "batch", "--json", batchJSON)

	tempHomeGo, buddyDirGo := setupTempBuddyEnv(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "batch", "--json", batchJSON)

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Batch", "batch --json ...", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("batch mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
	}
}

func TestParity_Init(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	tempHomePy, err := os.MkdirTemp("", "init-py-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempHomePy)
	envPy := append(os.Environ(), "HOME="+tempHomePy)
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy, "init")

	tempHomeGo, err := os.MkdirTemp("", "init-go-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempHomeGo)
	envGo := append(os.Environ(), "HOME="+tempHomeGo)
	goOut, _, goCode := runCLI(goBin, false, "", envGo, "init")

	var pyObj, goObj map[string]any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	matched := (pyCode == goCode && pyObj["status"] == "ok" && goObj["status"] == "ok")
	recordResult("Init", "init", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("init mismatch: pyCode=%d goCode=%d\nPy:\n%s\nGo:\n%s\n", pyCode, goCode, pyOut, goOut)
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

func TestParity_Task_Start_Worktree(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	// Run Python task start --worktree
	tempHomePy, buddyDirPy, repoPy := setupTempBuddyEnvWithGitRepo(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"task", "start", "-p", "myrepo", "-t", "wt-feat", "--worktree")

	// Run Go task start --worktree
	tempHomeGo, buddyDirGo, repoGo := setupTempBuddyEnvWithGitRepo(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)
	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"task", "start", "-p", "myrepo", "-t", "wt-feat", "--worktree")

	var pyObj, goObj map[string]any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyTask, _ := pyObj["task"].(map[string]any)
	goTask, _ := goObj["task"].(map[string]any)

	// Verify Python worktree convention
	expectedWtSuffix := filepath.Join("projects", "myrepo-wt-wt-feat")
	pyWt, _ := pyTask["worktree_path"].(string)
	if !strings.HasSuffix(pyWt, expectedWtSuffix) || pyTask["branch"] != "feat/wt-feat" || pyTask["cwd"] != pyWt {
		t.Fatalf("python worktree conventions unexpected: %v", pyTask)
	}

	// Verify Go worktree convention
	goWt, _ := goTask["worktree_path"].(string)
	if !strings.HasSuffix(goWt, expectedWtSuffix) || goTask["branch"] != "feat/wt-feat" || goTask["cwd"] != goWt {
		t.Fatalf("go worktree conventions unexpected: %v", goTask)
	}

	// Verify real git worktrees were created
	pyListCmd := exec.Command("git", "-C", repoPy, "worktree", "list")
	pyListOut, _ := pyListCmd.CombinedOutput()
	if !strings.Contains(string(pyListOut), "myrepo-wt-wt-feat") {
		t.Fatalf("git worktree list in python repo does not contain worktree:\n%s", string(pyListOut))
	}

	goListCmd := exec.Command("git", "-C", repoGo, "worktree", "list")
	goListOut, _ := goListCmd.CombinedOutput()
	if !strings.Contains(string(goListOut), "myrepo-wt-wt-feat") {
		t.Fatalf("git worktree list in go repo does not contain worktree:\n%s", string(goListOut))
	}

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Task_Start_Worktree", "task start --worktree", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("task start worktree mismatch:\nPy:\n%s\nGo:\n%s\n", pyOut, goOut)
	}
}

func TestParity_Task_Start_Worktree_ReuseExistingBranch(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	// Setup Python repo with pre-existing branch
	tempHomePy, buddyDirPy, repoPy := setupTempBuddyEnvWithGitRepo(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)
	_ = exec.Command("git", "-C", repoPy, "branch", "feat/wt-reuse").Run()
	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"task", "start", "-p", "myrepo", "-t", "wt-reuse", "--worktree")

	// Setup Go repo with pre-existing branch
	tempHomeGo, buddyDirGo, repoGo := setupTempBuddyEnvWithGitRepo(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)
	_ = exec.Command("git", "-C", repoGo, "branch", "feat/wt-reuse").Run()
	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)
	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"task", "start", "-p", "myrepo", "-t", "wt-reuse", "--worktree")

	var pyObj, goObj map[string]any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyTask, _ := pyObj["task"].(map[string]any)
	goTask, _ := goObj["task"].(map[string]any)

	if pyTask["branch"] != "feat/wt-reuse" || goTask["branch"] != "feat/wt-reuse" {
		t.Fatalf("reused branch mismatch: py=%v go=%v", pyTask["branch"], goTask["branch"])
	}

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == 0 && goCode == 0 && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Task_Start_Worktree_Reuse", "task start --worktree (reuse)", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("task start reuse branch mismatch:\nPy:\n%s\nGo:\n%s\n", pyOut, goOut)
	}
}

func TestParity_Dispatch_Worktree(t *testing.T) {
	_, pyScript, goBin := getPaths(t)

	// Run Python dispatch --worktree --branch
	tempHomePy, buddyDirPy, repoPy := setupTempBuddyEnvWithGitRepo(t)
	stubLogPy := filepath.Join(tempHomePy, "stub_rex_py.log")
	stubDirPy := createStubRex(t, stubLogPy)
	envPy := append(os.Environ(),
		"HOME="+tempHomePy,
		"BUDDY_DIR="+buddyDirPy,
		"PATH="+stubDirPy+":"+os.Getenv("PATH"),
	)
	pyOut, _, pyCode := runCLI(pyScript, true, "", envPy,
		"dispatch", "-p", "myrepo", "-m", "worktree prompt", "-t", "wt-disp", "--worktree", "--branch", "custom-branch")

	// Run Go dispatch --worktree --branch
	tempHomeGo, buddyDirGo, repoGo := setupTempBuddyEnvWithGitRepo(t)
	stubLogGo := filepath.Join(tempHomeGo, "stub_rex_go.log")
	stubDirGo := createStubRex(t, stubLogGo)
	envGo := append(os.Environ(),
		"HOME="+tempHomeGo,
		"BUDDY_DIR="+buddyDirGo,
		"PATH="+stubDirGo+":"+os.Getenv("PATH"),
	)
	goOut, _, goCode := runCLI(goBin, false, "", envGo,
		"dispatch", "-p", "myrepo", "-m", "worktree prompt", "-t", "wt-disp", "--worktree", "--branch", "custom-branch")

	// Read Rex argv logs
	pyLog, _ := os.ReadFile(stubLogPy)
	goLog, _ := os.ReadFile(stubLogGo)

	pyNormLog := normalizeText(string(pyLog), tempHomePy)
	goNormLog := normalizeText(string(goLog), tempHomeGo)

	// Rex window new should be called with -c pointing to the worktree path
	expectedSubstr := "myrepo-wt-wt-disp"
	if !strings.Contains(pyNormLog, expectedSubstr) || !strings.Contains(goNormLog, expectedSubstr) {
		t.Fatalf("rex argv does not spawn in worktree cwd:\nPy Log:\n%s\nGo Log:\n%s\n", pyNormLog, goNormLog)
	}

	// Verify worktree list shows custom-branch in both repos
	pyListOut, _ := exec.Command("git", "-C", repoPy, "worktree", "list").CombinedOutput()
	if !strings.Contains(string(pyListOut), "custom-branch") {
		t.Fatalf("custom branch not in python worktree list: %s", string(pyListOut))
	}

	goListOut, _ := exec.Command("git", "-C", repoGo, "worktree", "list").CombinedOutput()
	if !strings.Contains(string(goListOut), "custom-branch") {
		t.Fatalf("custom branch not in go worktree list: %s", string(goListOut))
	}

	var pyObj, goObj any
	_ = json.Unmarshal([]byte(pyOut), &pyObj)
	_ = json.Unmarshal([]byte(goOut), &goObj)

	pyNorm := normalizeJSON(pyObj, tempHomePy)
	goNorm := normalizeJSON(goObj, tempHomeGo)

	matched := (pyCode == goCode && reflect.DeepEqual(pyNorm, goNorm))
	recordResult("Dispatch_Worktree", "dispatch --worktree", pyCode, goCode, matched, "")

	if !matched {
		t.Fatalf("dispatch worktree mismatch:\nPy:\n%s\nGo:\n%s\n", pyOut, goOut)
	}
}
