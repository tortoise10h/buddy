package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// HistoryFilter controls which records QueryHistory returns.
type HistoryFilter struct {
	Project  string
	TaskSlug string
	Engine   string
	Since    time.Time
	Limit    int
}

// ParseSince parses a duration (e.g. "24h", "7d"), RFC3339 timestamp, or YYYY-MM-DD date
// into a lower-bound time.Time.
func ParseSince(spec string) (time.Time, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return time.Time{}, nil
	}

	if strings.HasSuffix(s, "d") && len(s) > 1 {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && days >= 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if strings.HasSuffix(s, "w") && len(s) > 1 {
		weeks, err := strconv.Atoi(strings.TrimSuffix(s, "w"))
		if err == nil && weeks >= 0 {
			return time.Now().Add(-time.Duration(weeks) * 7 * 24 * time.Hour), nil
		}
	}
	if dur, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-dur), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since value %q (use e.g. 24h, 7d, 2006-01-02, or RFC3339)", spec)
}

func parseHistoryTimestamp(ts string) (time.Time, bool) {
	if strings.TrimSpace(ts) == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999-07:00", ts); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// QueryHistory streams tasks.jsonl under lock, retaining at most filter.Limit matching records
// in O(limit) memory, and returns them newest-first.
func QueryHistory(filter HistoryFilter) ([]HistoryRecord, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 20
	}

	var results []HistoryRecord
	err := withHistoryLock(func() error {
		f, err := os.Open(GetHistoryPath())
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to open history file: %w", err)
		}
		defer f.Close()

		normEng := ""
		if filter.Engine != "" {
			normEng = NormalizeEngine(filter.Engine)
		}

		var ring []HistoryRecord
		ringIdx := 0
		ringCount := 0
		if limit > 0 {
			ring = make([]HistoryRecord, limit)
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			raw := strings.TrimSpace(scanner.Text())
			if raw == "" {
				continue
			}
			var rec HistoryRecord
			if err := json.Unmarshal([]byte(raw), &rec); err != nil {
				continue
			}
			if filter.Project != "" && !strings.EqualFold(rec.Project, filter.Project) {
				continue
			}
			if filter.TaskSlug != "" && !strings.EqualFold(rec.TaskSlug, filter.TaskSlug) {
				continue
			}
			if normEng != "" && NormalizeEngine(rec.Engine) != normEng {
				continue
			}
			if !filter.Since.IsZero() {
				ts, ok := parseHistoryTimestamp(rec.Ts)
				if !ok || ts.Before(filter.Since) {
					continue
				}
			}

			if limit > 0 {
				ring[ringIdx] = rec
				ringIdx = (ringIdx + 1) % limit
				if ringCount < limit {
					ringCount++
				}
			} else {
				results = append(results, rec)
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}

		if limit > 0 {
			results = make([]HistoryRecord, 0, ringCount)
			for i := 0; i < ringCount; i++ {
				pos := (ringIdx - 1 - i + limit) % limit
				results = append(results, ring[pos])
			}
		} else {
			for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
				results[i], results[j] = results[j], results[i]
			}
		}
		return nil
	})
	return results, err
}

// CleanedWindowIDs returns a map of window_id -> latest cleaned timestamp for the given engine.
func CleanedWindowIDs(engine string) (map[string]time.Time, error) {
	cleaned := make(map[string]time.Time)
	normEng := NormalizeEngine(engine)

	err := withHistoryLock(func() error {
		f, err := os.Open(GetHistoryPath())
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to open history file: %w", err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			raw := strings.TrimSpace(scanner.Text())
			if raw == "" {
				continue
			}
			var rec HistoryRecord
			if err := json.Unmarshal([]byte(raw), &rec); err != nil {
				continue
			}
			if rec.WindowID == "" || NormalizeEngine(rec.Engine) != normEng {
				continue
			}
			ts, _ := parseHistoryTimestamp(rec.Ts)
			if prev, ok := cleaned[rec.WindowID]; !ok || ts.After(prev) {
				cleaned[rec.WindowID] = ts
			}
		}
		return scanner.Err()
	})
	return cleaned, err
}
