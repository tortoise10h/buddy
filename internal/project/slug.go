package project

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	conversationalRe = regexp.MustCompile(`(?i)^(please\s+|can\s+you\s+|could\s+you\s+|i\s+need\s+you\s+to\s+|help\s+me\s+)+`)
	wordRe           = regexp.MustCompile(`[a-zA-Z0-9]+`)
	stopWords        = map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "by": true, "for": true, "from": true, "has": true, "he": true,
		"in": true, "is": true, "it": true, "its": true, "of": true, "on": true,
		"that": true, "the": true, "to": true, "was": true, "were": true, "will": true,
		"with": true,
	}
)

// DeriveSlug produces a clean semantic slug. If explicitSlug is provided, it sanitizes it.
// Otherwise, it extracts 2-3 words from prompt + a 4-char hash for uniqueness.
func DeriveSlug(explicitSlug, prompt, projectName string) string {
	if strings.TrimSpace(explicitSlug) != "" {
		cleaned := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(explicitSlug, "-")
		cleaned = strings.Trim(strings.ToLower(cleaned), "-")
		if cleaned != "" {
			return cleaned
		}
	}

	h := sha256.Sum256([]byte(prompt))
	hashStr := hex.EncodeToString(h[:])[:4]

	cleanedPrompt := conversationalRe.ReplaceAllString(prompt, "")
	words := wordRe.FindAllString(strings.ToLower(cleanedPrompt), -1)

	var meaningful []string
	for _, w := range words {
		if !stopWords[w] && len(w) > 1 {
			meaningful = append(meaningful, w)
			if len(meaningful) == 3 {
				break
			}
		}
	}

	if len(meaningful) > 0 {
		base := strings.Join(meaningful, "-")
		if len(base) > 24 {
			base = base[:24]
		}
		base = strings.TrimRight(base, "-")
		return base + "-" + hashStr
	}

	proj := projectName
	if proj == "" {
		proj = "task"
	}
	proj = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(proj), "-")
	return strings.Trim(proj, "-") + "-task-" + hashStr
}
