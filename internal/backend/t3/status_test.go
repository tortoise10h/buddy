package t3

import (
	"testing"
)

func strPtr(s string) *string {
	return &s
}

func TestMapThreadStatus(t *testing.T) {
	tests := []struct {
		name                string
		thread              *ThreadShell
		expectedStatus      string
		expectedSettled     bool
		expectedPendingReqs int
	}{
		{
			name:            "nil thread returns missing",
			thread:          nil,
			expectedStatus:  "missing",
			expectedSettled: true,
		},
		{
			name: "deleted thread returns missing",
			thread: &ThreadShell{
				ID:        "th_1",
				Status:    "running",
				DeletedAt: strPtr("2026-11-01T00:00:00Z"),
			},
			expectedStatus:  "missing",
			expectedSettled: true,
		},
		{
			name: "archived thread returns stopped",
			thread: &ThreadShell{
				ID:         "th_1",
				Status:     "running",
				ArchivedAt: strPtr("2026-11-01T00:00:00Z"),
			},
			expectedStatus:  "stopped",
			expectedSettled: true,
		},
		{
			name: "preparing maps to starting",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "preparing",
			},
			expectedStatus:  "starting",
			expectedSettled: false,
		},
		{
			name: "queued maps to starting",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "queued",
			},
			expectedStatus:  "starting",
			expectedSettled: false,
		},
		{
			name: "starting maps to starting",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "starting",
			},
			expectedStatus:  "starting",
			expectedSettled: false,
		},
		{
			name: "running with 0 pending requests maps to running",
			thread: &ThreadShell{
				ID:                  "th_1",
				Status:              "running",
				PendingRequestCount: 0,
			},
			expectedStatus:  "running",
			expectedSettled: false,
		},
		{
			name: "running with pending requests maps to waiting_for_input",
			thread: &ThreadShell{
				ID:                  "th_1",
				Status:              "running",
				PendingRequestCount: 1,
			},
			expectedStatus:      "waiting_for_input",
			expectedSettled:     false,
			expectedPendingReqs: 1,
		},
		{
			name: "running with PendingRuntimeRequest object maps to waiting_for_input",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "running",
				PendingRuntimeRequest: &PendingRuntimeRequestSummary{
					ID:   "req_123",
					Kind: "user_input",
				},
			},
			expectedStatus:      "waiting_for_input",
			expectedSettled:     false,
			expectedPendingReqs: 1,
		},
		{
			name: "waiting status maps to waiting_for_input even if pendingRequestCount is 0",
			thread: &ThreadShell{
				ID:                  "th_1",
				Status:              "waiting",
				PendingRequestCount: 0,
			},
			expectedStatus:  "waiting_for_input",
			expectedSettled: false,
		},
		{
			name: "completed status maps to ready_for_demo and settled",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "completed",
			},
			expectedStatus:  "ready_for_demo",
			expectedSettled: true,
		},
		{
			name: "failed status maps to error",
			thread: &ThreadShell{
				ID:        "th_1",
				Status:    "failed",
				LastError: strPtr("OOM killed"),
			},
			expectedStatus:  "error",
			expectedSettled: true,
		},
		{
			name: "cancelled status maps to error",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "cancelled",
			},
			expectedStatus:  "error",
			expectedSettled: true,
		},
		{
			name: "rolled_back status maps to error",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "rolled_back",
			},
			expectedStatus:  "error",
			expectedSettled: true,
		},
		{
			name: "interrupted status maps to stopped",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "interrupted",
			},
			expectedStatus:  "stopped",
			expectedSettled: true,
		},
		{
			name: "idle status with settledAt non-nil maps to ready_for_demo",
			thread: &ThreadShell{
				ID:        "th_1",
				Status:    "idle",
				SettledAt: strPtr("2026-11-01T00:00:00Z"),
			},
			expectedStatus:  "ready_for_demo",
			expectedSettled: true,
		},
		{
			name: "idle status without settledAt maps to stopped",
			thread: &ThreadShell{
				ID:     "th_1",
				Status: "idle",
			},
			expectedStatus:  "stopped",
			expectedSettled: false,
		},
		{
			name: "settledOverride == 'settled' marks thread as settled",
			thread: &ThreadShell{
				ID:              "th_1",
				Status:          "running",
				SettledOverride: strPtr("settled"),
			},
			expectedStatus:  "running",
			expectedSettled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := MapThreadStatus(tt.thread)
			if report.Status != tt.expectedStatus {
				t.Errorf("expected status '%s', got '%s'", tt.expectedStatus, report.Status)
			}
			if report.Settled != tt.expectedSettled {
				t.Errorf("expected settled %v, got %v", tt.expectedSettled, report.Settled)
			}
			if tt.expectedPendingReqs > 0 && report.PendingRequestCount != tt.expectedPendingReqs {
				t.Errorf("expected pending request count %d, got %d", tt.expectedPendingReqs, report.PendingRequestCount)
			}
		})
	}
}
