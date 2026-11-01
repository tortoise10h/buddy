package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/spf13/cobra"
	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/state"
)

type HookPayload struct {
	WorkspacePaths []string `json:"workspacePaths"`
}

type SeenEntry struct {
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

func newContextInjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context-inject",
		Short: "PreInvocation hook handler (internal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextInject(os.Stdin, os.Stdout)
		},
	}
}

// FindBuddyRepoPath attempts to determine the buddy repository root directory.
func FindBuddyRepoPath() string {
	if repoEnv := os.Getenv("BUDDY_REPO_DIR"); repoEnv != "" {
		if resolved, err := filepath.EvalSymlinks(repoEnv); err == nil {
			return resolved
		}
		return repoEnv
	}

	exe, err := os.Executable()
	if err == nil {
		resolvedExe, err := filepath.EvalSymlinks(exe)
		if err == nil {
			// exe might be in <repo>/bin/buddy-ctl
			dir := filepath.Dir(resolvedExe)
			if filepath.Base(dir) == "bin" {
				return filepath.Dir(dir)
			}
		}
	}

	// Check cwd and ancestors
	cwd, err := os.Getwd()
	if err == nil {
		curr := cwd
		for {
			if _, err := os.Stat(filepath.Join(curr, "skills", "buddy")); err == nil {
				if resolved, err := filepath.EvalSymlinks(curr); err == nil {
					return resolved
				}
				return curr
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	return ""
}

func runContextInject(in io.Reader, out io.Writer) error {
	var payload HookPayload
	if in != nil {
		// Only read if stdin is not a char device or if in is a custom reader (like in tests)
		shouldRead := true
		if f, ok := in.(*os.File); ok {
			fi, err := f.Stat()
			if err != nil || (fi.Mode()&os.ModeCharDevice != 0) {
				shouldRead = false
			}
		}
		if shouldRead {
			data, err := io.ReadAll(in)
			if err == nil && len(bytes.TrimSpace(data)) > 0 {
				_ = json.Unmarshal(data, &payload)
			}
		}
	}

	buddyDir := config.GetBuddyDir()
	buddyPath, err := filepath.EvalSymlinks(buddyDir)
	if err != nil {
		buddyPath = buddyDir
	}

	repoPath := FindBuddyRepoPath()

	cwd, _ := os.Getwd()
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		resolvedCwd = cwd
	}

	isBuddy := (resolvedCwd == buddyPath || (repoPath != "" && resolvedCwd == repoPath))
	if !isBuddy {
		for _, wp := range payload.WorkspacePaths {
			wpResolved, err := filepath.EvalSymlinks(wp)
			if err != nil {
				wpResolved = wp
			}
			if wpResolved == buddyPath || (repoPath != "" && wpResolved == repoPath) {
				isBuddy = true
				break
			}
		}
	}

	if !isBuddy {
		fmt.Fprintln(out, "{}")
		return nil
	}

	cfg := config.LoadConfig()
	st := state.LoadState()

	seenPath := filepath.Join(buddyDir, ".state_seen.json")
	stateSeen := make(map[string]SeenEntry)
	if data, err := os.ReadFile(seenPath); err == nil {
		_ = json.Unmarshal(data, &stateSeen)
	}

	newSeen := make(map[string]SeenEntry, len(stateSeen))
	for k, v := range stateSeen {
		newSeen[k] = v
	}

	var taskSummaries []string
	var nudges []string

	var pNames []string
	for p := range st.Projects {
		pNames = append(pNames, p)
	}
	sort.Strings(pNames)

	for _, pName := range pNames {
		pData := st.Projects[pName]
		var tNames []string
		for t := range pData.Tasks {
			tNames = append(tNames, t)
		}
		sort.Strings(tNames)

		for _, tName := range tNames {
			tData := pData.Tasks[tName]
			tKey := fmt.Sprintf("%s/%s", pName, tName)
			agent := tData.Agent
			if agent == "" {
				agent = "agy"
			}
			currStatus := tData.Status
			if currStatus == "" {
				currStatus = "unknown"
			}
			currSummary := tData.LastSummary
			if currSummary == "" {
				currSummary = tData.Summary
			}
			currUpdated := tData.UpdatedAt

			taskSummaries = append(taskSummaries, fmt.Sprintf("%s (%s, %s: '%s')", tKey, agent, currStatus, currSummary))

			seenInfo := stateSeen[tKey]
			seenStatus := seenInfo.Status
			seenUpdated := seenInfo.UpdatedAt

			if currStatus == "waiting_for_input" || currStatus == "ready_for_demo" || currStatus == "error" {
				if currStatus != seenStatus || currUpdated != seenUpdated {
					switch currStatus {
					case "waiting_for_input":
						nudges = append(nudges, fmt.Sprintf("• ❓ %s: WAITING FOR INPUT — \"%s\"", tKey, currSummary))
					case "ready_for_demo":
						nudges = append(nudges, fmt.Sprintf("• 🚀 %s: READY FOR DEMO — \"%s\"", tKey, currSummary))
					case "error":
						nudges = append(nudges, fmt.Sprintf("• ⚠️ %s: ERROR — \"%s\"", tKey, currSummary))
					}
				}
			}

			newSeen[tKey] = SeenEntry{
				Status:    currStatus,
				UpdatedAt: currUpdated,
			}
		}
	}

	if seenData, err := json.MarshalIndent(newSeen, "", "  "); err == nil {
		_ = os.WriteFile(seenPath, seenData, 0644)
	}

	tasksStr := "None active"
	if len(taskSummaries) > 0 {
		tasksStr = strings.Join(taskSummaries, "; ")
	}

	orchAgent := cfg.GetEffectiveOrchestratorAgent()
	workerAgent := cfg.GetEffectiveWorkerAgent()

	nudgeBanner := ""
	if len(nudges) > 0 {
		nudgeLines := strings.Join(nudges, "\n")
		nudgeBanner = fmt.Sprintf(
			"\n\n[🚨 BUDDY PROACTIVE NUDGE] State transitions requiring immediate attention:\n%s\nACTION REQUIRED: Prioritize notifying the user about these worker milestones/questions before addressing other requests!",
			nudgeLines,
		)
	}

	ephemeralText := fmt.Sprintf(
		"[Buddy Live State] Orchestrator: %s | Worker: %s | Active projects: %d | Tasks: %s. 🚨 ZERO-TOLERANCE GUARDRAIL (RULE #1): NEVER edit project files, docs, or code in this session. Even for minor edits, typos, or READMEs, ALWAYS dispatch a task tab via 'buddy-ctl dispatch'.%s",
		orchAgent, workerAgent, len(st.Projects), tasksStr, nudgeBanner,
	)

	escaped := jsonEscapeASCII(ephemeralText)
	fmt.Fprintf(out, "{\"injectSteps\": [{\"ephemeralMessage\": %s}]}\n", escaped)
	return nil
}

// jsonEscapeASCII serializes string with ASCII-only escaping matching Python json.dumps(..., ensure_ascii=True).
func jsonEscapeASCII(s string) string {
	var buf strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&buf, `\u%04x`, r)
			} else if r > 0x7e {
				if r <= 0xffff {
					fmt.Fprintf(&buf, `\u%04x`, r)
				} else {
					r1, r2 := utf16.EncodeRune(r)
					fmt.Fprintf(&buf, `\u%04x\u%04x`, r1, r2)
				}
			} else {
				buf.WriteRune(r)
			}
		}
	}
	return `"` + buf.String() + `"`
}
