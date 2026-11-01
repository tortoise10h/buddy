package formatters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tortoise10h/buddy/internal/state"
)

// FormatJSON serializes any data structure with 2-space indentation and no HTML escaping.
func FormatJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// FormatStatusBrief formats projects and tasks as a concise summary list.
func FormatStatusBrief(projects map[string]state.ProjectNode) string {
	var lines []string

	var pNames []string
	for p := range projects {
		pNames = append(pNames, p)
	}
	sort.Strings(pNames)

	for _, pName := range pNames {
		pData := projects[pName]
		if len(pData.Tasks) == 0 {
			sessID := pData.RexSessionID
			if sessID == "" {
				sessID = pData.SessionID
			}
			if sessID == "" {
				sessID = "none"
			}
			lines = append(lines, fmt.Sprintf("%s: No active tasks (session: %s)", pName, sessID))
			continue
		}

		var tNames []string
		for t := range pData.Tasks {
			tNames = append(tNames, t)
		}
		sort.Strings(tNames)

		for _, tName := range tNames {
			tData := pData.Tasks[tName]
			agent := tData.Agent
			if agent == "" {
				agent = "agy"
			}
			status := tData.Status
			if status == "" {
				status = "unknown"
			}
			summary := tData.LastSummary
			if summary == "" {
				summary = tData.Summary
			}
			lines = append(lines, fmt.Sprintf("• %s/%s [%s | %s]: %s", pName, tName, agent, status, summary))
		}
	}

	if len(lines) == 0 {
		return "No active projects or tasks."
	}
	return strings.Join(lines, "\n")
}

// FormatStatusTable formats projects and tasks as an aligned ASCII table.
func FormatStatusTable(projects map[string]state.ProjectNode) string {
	var out []string
	out = append(out, fmt.Sprintf("%-16s %-20s %-8s %-16s %s", "PROJECT", "TASK", "AGENT", "STATUS", "SUMMARY"))
	out = append(out, strings.Repeat("-", 80))

	var pNames []string
	for p := range projects {
		pNames = append(pNames, p)
	}
	sort.Strings(pNames)

	for _, pName := range pNames {
		pData := projects[pName]
		if len(pData.Tasks) == 0 {
			sessID := pData.RexSessionID
			if sessID == "" {
				sessID = pData.SessionID
			}
			if sessID == "" {
				sessID = "none"
			}
			out = append(out, fmt.Sprintf("%-16s %-20s %-8s %-16s Rex session: %s",
				pName, "(none)", "-", "idle", sessID))
			continue
		}

		var tNames []string
		for t := range pData.Tasks {
			tNames = append(tNames, t)
		}
		sort.Strings(tNames)

		for _, tName := range tNames {
			tData := pData.Tasks[tName]
			agent := tData.Agent
			if agent == "" {
				agent = "agy"
			}
			status := tData.Status
			if status == "" {
				status = "unknown"
			}
			summary := tData.LastSummary
			if summary == "" {
				summary = tData.Summary
			}
			out = append(out, fmt.Sprintf("%-16s %-20s %-8s %-16s %s",
				pName, tName, agent, status, summary))
		}
	}

	return strings.Join(out, "\n")
}
