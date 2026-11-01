package rex

// RexSessionItem represents a session in rex ls output.
type RexSessionItem struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd,omitempty"`
}

// RexLsOutput represents output of `rex ls --json`.
type RexLsOutput struct {
	Sessions []RexSessionItem `json:"sessions"`
}

// RexSessionInspectOutput represents output of `rex session inspect <sessionID> --json` or `rex new ... --json`.
type RexSessionInspectOutput struct {
	SessionID string `json:"session_id"`
	Label     string `json:"label"`
	Cwd       string `json:"cwd,omitempty"`
}

// RexWindowItem represents a window in rex window ls output.
type RexWindowItem struct {
	WindowID       string   `json:"window_id"`
	Label          string   `json:"label"`
	FocusedBlockID string   `json:"focused_block_id,omitempty"`
	BlockIDs       []string `json:"block_ids,omitempty"`
}

// RexWindowLsOutput represents output of `rex window ls -s <sessionID> --json`.
type RexWindowLsOutput struct {
	Windows []RexWindowItem `json:"windows"`
}

// RexWindowNewOutput represents output of `rex window new ... --json` or `rex window inspect ... --json`.
type RexWindowNewOutput struct {
	WindowID string   `json:"window_id"`
	BlockIDs []string `json:"block_ids"`
	Label    string   `json:"label,omitempty"`
}

// RexBlockInspectOutput represents output of `rex block inspect <blockID> --json` or `rex block split ... --json`.
type RexBlockInspectOutput struct {
	BlockID string `json:"block_id"`
	IsAlive bool   `json:"is_alive"`
}
