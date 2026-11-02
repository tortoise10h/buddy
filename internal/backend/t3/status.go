package t3

import (
	"fmt"
	"strings"

	"github.com/tortoise10h/buddy/internal/backend"
)

// MapThreadStatus maps T3 Code thread/run state onto Buddy task lifecycle state
// according to the deterministic mapping table in Section 5 of the design plan.
func MapThreadStatus(thread *ThreadShell) *backend.AgentStatusReport {
	if thread == nil {
		return &backend.AgentStatusReport{
			Status:  "missing",
			Summary: "Thread not found in T3 Code",
			Settled: true,
		}
	}

	if thread.DeletedAt != nil {
		return &backend.AgentStatusReport{
			Status:  "missing",
			Summary: "Thread was deleted in T3 Code",
			Settled: true,
		}
	}

	if thread.ArchivedAt != nil {
		return &backend.AgentStatusReport{
			Status:  "stopped",
			Summary: "Thread was archived in T3 Code",
			Settled: true,
		}
	}

	pendingCount := thread.EffectivePendingRequestCount()
	t3Status := strings.ToLower(strings.TrimSpace(thread.Status))

	var buddyStatus string
	var summary string
	var errorDetails string

	activeRunID := ""
	if thread.ActiveRunID != nil {
		activeRunID = *thread.ActiveRunID
	}
	latestRunID := ""
	if thread.LatestRunID != nil {
		latestRunID = *thread.LatestRunID
	}

	switch {
	case pendingCount > 0 || t3Status == "waiting":
		buddyStatus = "waiting_for_input"
		if thread.PendingRuntimeRequest != nil {
			summary = fmt.Sprintf("Thread waiting for input: request %s (%s)", thread.PendingRuntimeRequest.ID, thread.PendingRuntimeRequest.Kind)
		} else {
			summary = fmt.Sprintf("Thread waiting for input (%d pending request(s))", pendingCount)
		}
		if thread.LatestVisibleMessage != nil && thread.LatestVisibleMessage.Text != "" {
			summary = fmt.Sprintf("%s: %s", summary, truncateSummary(thread.LatestVisibleMessage.Text, 120))
		}

	case t3Status == "preparing" || t3Status == "queued" || t3Status == "starting":
		buddyStatus = "starting"
		summary = fmt.Sprintf("Thread %s (run %s)", t3Status, latestRunID)

	case t3Status == "running":
		buddyStatus = "running"
		if thread.LatestVisibleMessage != nil && thread.LatestVisibleMessage.Text != "" {
			summary = truncateSummary(thread.LatestVisibleMessage.Text, 140)
		} else {
			summary = fmt.Sprintf("Run %s running on %s", activeRunID, thread.ProviderInstanceID)
		}

	case t3Status == "completed":
		buddyStatus = "ready_for_demo"
		if thread.LatestVisibleMessage != nil && thread.LatestVisibleMessage.Text != "" {
			summary = truncateSummary(thread.LatestVisibleMessage.Text, 140)
		} else {
			summary = "Task completed and ready for review"
		}

	case t3Status == "failed" || t3Status == "cancelled" || t3Status == "rolled_back":
		buddyStatus = "error"
		if thread.LastError != nil && *thread.LastError != "" {
			errorDetails = *thread.LastError
			summary = fmt.Sprintf("Run %s: %s", t3Status, *thread.LastError)
		} else {
			summary = fmt.Sprintf("Run ended with status '%s'", t3Status)
		}

	case t3Status == "interrupted":
		buddyStatus = "stopped"
		summary = "Thread run was interrupted"

	case t3Status == "idle":
		if thread.IsSettled() {
			buddyStatus = "ready_for_demo"
			summary = "Thread is settled"
		} else {
			buddyStatus = "stopped"
			summary = "Thread is idle"
		}

	default:
		buddyStatus = t3Status
		summary = fmt.Sprintf("Thread status: %s", t3Status)
	}

	settled := thread.IsSettled() || buddyStatus == "ready_for_demo" || buddyStatus == "error" || t3Status == "interrupted"

	return &backend.AgentStatusReport{
		Status:              buddyStatus,
		Summary:             summary,
		PendingRequestCount: pendingCount,
		Settled:             settled,
		ActiveRunID:         activeRunID,
		LatestRunID:         latestRunID,
		ErrorDetails:        errorDetails,
	}
}

func truncateSummary(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	lines := strings.Split(trimmed, "\n")
	firstLine := strings.TrimSpace(lines[0])
	if len(firstLine) > maxLen {
		return firstLine[:maxLen] + "..."
	}
	return firstLine
}
