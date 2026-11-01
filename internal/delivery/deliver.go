package delivery

import (
	"context"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/tortoise10h/buddy/internal/backends"
)

var (
	activityIndicators = []string{
		"thinking",
		"thought",
		"explor",
		"analyz",
		"read",
		"edit",
		"ran ",
		"●",
		"○",
		"▸",
		"task complete",
	}
	activityRe = regexp.MustCompile(`(?i)(?:` + strings.Join(activityIndicators, "|") + `)`)
)

func VerifyBlockPickup(ctx context.Context, backend backends.TerminalBackend, sessionID, blockID, promptSnippet string, maxTimeout time.Duration) (bool, string) {
	start := time.Now()
	cleanSnippet := strings.ToLower(strings.TrimSpace(promptSnippet))
	if len(cleanSnippet) > 30 {
		cleanSnippet = cleanSnippet[:30]
	}

	lastPreview := ""
	for time.Since(start) < maxTimeout {
		select {
		case <-ctx.Done():
			return false, lastPreview
		default:
		}

		capRes, err := backend.Capture(ctx, sessionID, blockID, 0)
		if err == nil && capRes.Text != "" {
			var lines []string
			for _, ln := range strings.Split(capRes.Text, "\n") {
				trimmed := strings.TrimSpace(ln)
				if trimmed != "" {
					lines = append(lines, trimmed)
				}
			}
			if len(lines) == 1 {
				lastPreview = lines[0]
			} else if len(lines) > 1 {
				lastPreview = lines[len(lines)-2] + " | " + lines[len(lines)-1]
			}

			outLower := strings.ToLower(capRes.Text)
			if (cleanSnippet != "" && strings.Contains(outLower, cleanSnippet)) || activityRe.MatchString(capRes.Text) {
				if len(lastPreview) > 120 {
					lastPreview = lastPreview[:120]
				}
				return true, lastPreview
			}
		}

		time.Sleep(150 * time.Millisecond)
	}

	if lastPreview == "" {
		lastPreview = "Prompt dispatched (verification pending)"
	} else if len(lastPreview) > 120 {
		lastPreview = lastPreview[:120]
	}
	return false, lastPreview
}

func SendPromptSmart(
	ctx context.Context,
	backend backends.TerminalBackend,
	sessionID, blockID, promptText, agentName, submitKey string,
	submitDelayMS int,
) (bool, string) {
	timeoutSeconds := math.Max(3.0, float64(submitDelayMS)/1000.0+1.0)
	ready, _ := backend.WaitReady(ctx, sessionID, blockID, time.Duration(timeoutSeconds*float64(time.Second)))
	if !ready {
		sleepSec := math.Min(1.0, float64(submitDelayMS)/1000.0)
		time.Sleep(time.Duration(sleepSec * float64(time.Second)))
	}

	_ = backend.SendText(ctx, sessionID, blockID, promptText)
	_ = backend.SendKey(ctx, sessionID, blockID, submitKey)

	promptStart := ""
	lines := strings.Split(promptText, "\n")
	if len(lines) > 0 {
		promptStart = lines[0]
	}

	return VerifyBlockPickup(ctx, backend, sessionID, blockID, promptStart, 1800*time.Millisecond)
}
