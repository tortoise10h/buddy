package state

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const HistoryVersion = 1

// HistoryRecord represents one immutable append-only audit entry in tasks.jsonl.
type HistoryRecord struct {
	CleanupVersion int     `json:"cleanup_version"`
	CleanupID      string  `json:"cleanup_id"`
	Ts             string  `json:"ts"`
	Project        string  `json:"project"`
	ProjectPath    string  `json:"project_path"`
	TaskSlug       string  `json:"task_slug"`
	Engine         string  `json:"engine"`
	SessionID      string  `json:"session_id"`
	WindowID       string  `json:"window_id"`
	WindowLabel    string  `json:"window_label"`
	AgentBlockID   string  `json:"agent_block_id"`
	DevBlockID     *string `json:"dev_block_id"`
	Agent          string  `json:"agent"`
	Cwd            string  `json:"cwd"`
	WorktreePath   *string `json:"worktree_path"`
	Branch         *string `json:"branch"`
	FinalStatus    string  `json:"final_status"`
	Origin         string  `json:"origin"`
	InitialPrompt  *string `json:"initial_prompt"`
	LastSummary    string  `json:"last_summary"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	LastVerifiedAt string  `json:"last_verified_at"`
	Reason         string  `json:"reason"`
	Forced         bool    `json:"forced"`
	ThreadSettled  *bool   `json:"thread_settled,omitempty"`
	SettleError    string  `json:"settle_error,omitempty"`
}

// GetHistoryDir returns the directory for append-only task history (~/.buddy/history).
func GetHistoryDir() string {
	return filepath.Join(GetBuddyDir(), "history")
}

// GetHistoryPath returns the path to ~/.buddy/history/tasks.jsonl.
func GetHistoryPath() string {
	return filepath.Join(GetHistoryDir(), "tasks.jsonl")
}

func withHistoryLock(fn func() error) error {
	lockPath := filepath.Join(GetHistoryDir(), ".tasks.lock")
	return withLockFile(lockPath, fn)
}

// NewCleanupID generates a unique cleanup identifier (e.g. cln_20261115T124500_a1b2c3d4).
func NewCleanupID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("cln_%s_%s", time.Now().Format("20060102T150405"), hex.EncodeToString(b[:]))
}

// NewHistoryRecord constructs a HistoryRecord from a ProjectState and TaskState snapshot.
func NewHistoryRecord(proj ProjectState, task TaskState, reason string, forced bool) HistoryRecord {
	eng := task.GetEngine()
	if strings.TrimSpace(reason) == "" {
		reason = "cleaned via CLI"
	}
	return HistoryRecord{
		CleanupVersion: HistoryVersion,
		CleanupID:      NewCleanupID(),
		Ts:             nowISO(),
		Project:        proj.Name,
		ProjectPath:    proj.ProjectPath,
		TaskSlug:       task.TaskSlug,
		Engine:         eng,
		SessionID:      proj.RexSessionID,
		WindowID:       task.WindowID,
		WindowLabel:    task.WindowLabel,
		AgentBlockID:   task.AgentBlockID,
		DevBlockID:     task.DevBlockID,
		Agent:          task.Agent,
		Cwd:            task.Cwd,
		WorktreePath:   task.WorktreePath,
		Branch:         task.Branch,
		FinalStatus:    task.Status,
		Origin:         task.Origin,
		InitialPrompt:  task.InitialPrompt,
		LastSummary:    task.LastSummary,
		CreatedAt:      task.CreatedAt,
		UpdatedAt:      task.UpdatedAt,
		LastVerifiedAt: task.LastVerifiedAt,
		Reason:         reason,
		Forced:         forced,
	}
}

// AppendHistoryRecord atomically appends a single HistoryRecord to tasks.jsonl under flock and fsyncs.
func AppendHistoryRecord(rec HistoryRecord) error {
	return AppendHistoryRecords([]HistoryRecord{rec})
}

// AppendHistoryRecords atomically appends one or more HistoryRecords to tasks.jsonl under flock and fsyncs.
func AppendHistoryRecords(recs []HistoryRecord) error {
	if len(recs) == 0 {
		return nil
	}
	return withHistoryLock(func() error {
		path := GetHistoryPath()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create history directory: %w", err)
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open history file %s: %w", path, err)
		}
		defer f.Close()

		for _, rec := range recs {
			if rec.CleanupVersion == 0 {
				rec.CleanupVersion = HistoryVersion
			}
			if rec.CleanupID == "" {
				rec.CleanupID = NewCleanupID()
			}
			if rec.Ts == "" {
				rec.Ts = nowISO()
			}
			line, err := json.Marshal(rec)
			if err != nil {
				return fmt.Errorf("failed to serialize history record: %w", err)
			}
			line = append(line, '\n')
			if _, err := f.Write(line); err != nil {
				return fmt.Errorf("failed to write history record: %w", err)
			}
		}

		if err := f.Sync(); err != nil {
			return fmt.Errorf("failed to fsync history file: %w", err)
		}
		return nil
	})
}

// FindRecentHistoryRecord returns the most recent HistoryRecord matching project, taskSlug, and engine (if non-empty).
// Returns (nil, nil) when no matching record exists or tasks.jsonl does not exist yet.
func FindRecentHistoryRecord(project, taskSlug, engine string) (*HistoryRecord, error) {
	var match *HistoryRecord
	err := withHistoryLock(func() error {
		path := GetHistoryPath()
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to open history file: %w", err)
		}
		defer f.Close()

		normEng := ""
		if engine != "" {
			normEng = NormalizeEngine(engine)
		}

		scanner := bufio.NewScanner(f)
		// Support large initial_prompt payloads (up to 1MB per line).
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			raw := strings.TrimSpace(scanner.Text())
			if raw == "" {
				continue
			}
			var rec HistoryRecord
			if err := json.Unmarshal([]byte(raw), &rec); err != nil {
				// Skip corrupt/partial trailing lines gracefully.
				continue
			}
			if !strings.EqualFold(rec.Project, project) || !strings.EqualFold(rec.TaskSlug, taskSlug) {
				continue
			}
			if normEng != "" && NormalizeEngine(rec.Engine) != normEng {
				continue
			}
			copyRec := rec
			match = &copyRec
		}
		return scanner.Err()
	})
	return match, err
}
