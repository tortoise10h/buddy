package task

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var SlugStopWords = map[string]bool{
	"a": true, "about": true, "above": true, "after": true, "again": true, "against": true, "all": true, "am": true,
	"an": true, "and": true, "any": true, "are": true, "aren't": true, "as": true, "at": true, "be": true,
	"because": true, "been": true, "before": true, "being": true, "below": true, "between": true, "both": true,
	"but": true, "by": true, "can't": true, "cannot": true, "could": true, "couldn't": true, "did": true,
	"didn't": true, "do": true, "does": true, "doesn't": true, "doing": true, "don't": true, "down": true,
	"during": true, "each": true, "few": true, "for": true, "from": true, "further": true, "had": true,
	"hadn't": true, "has": true, "hasn't": true, "have": true, "haven't": true, "having": true, "he": true,
	"he'd": true, "he'll": true, "he's": true, "her": true, "here": true, "here's": true, "hers": true,
	"herself": true, "him": true, "himself": true, "his": true, "how": true, "how's": true, "i": true,
	"i'd": true, "i'll": true, "i'm": true, "i've": true, "if": true, "in": true, "into": true, "is": true,
	"isn't": true, "it": true, "it's": true, "its": true, "itself": true, "let's": true, "me": true,
	"more": true, "most": true, "mustn't": true, "my": true, "myself": true, "no": true, "nor": true,
	"not": true, "of": true, "off": true, "on": true, "once": true, "only": true, "or": true, "other": true,
	"ought": true, "our": true, "ours": true, "ourselves": true, "out": true, "over": true, "own": true,
	"same": true, "shan't": true, "she": true, "she'd": true, "she'll": true, "she's": true, "should": true,
	"shouldn't": true, "so": true, "some": true, "such": true, "than": true, "that": true, "that's": true,
	"the": true, "their": true, "theirs": true, "them": true, "themselves": true, "then": true, "there": true,
	"there's": true, "these": true, "they": true, "they'd": true, "they'll": true, "they're": true,
	"they've": true, "this": true, "those": true, "through": true, "to": true, "too": true, "under": true,
	"until": true, "up": true, "very": true, "was": true, "wasn't": true, "we": true, "we'd": true,
	"we'll": true, "we're": true, "we've": true, "were": true, "weren't": true, "what": true, "what's": true,
	"when": true, "when's": true, "where": true, "where's": true, "which": true, "while": true, "who": true,
	"who's": true, "whom": true, "why": true, "why's": true, "with": true, "won't": true, "would": true,
	"wouldn't": true, "you": true, "you'd": true, "you'll": true, "you're": true, "you've": true, "your": true,
	"yours": true, "yourself": true, "yourselves": true, "please": true, "can": true,
	"look": true, "take": true, "hey": true, "buddy": true, "make": true, "sure": true,
	"want": true, "need": true, "go": true, "ahead": true, "help": true,
}

var conversationalPrefixRe = regexp.MustCompile(
	`(?i)^(?:in\s+[\w-]+\s*,?\s*)?(?:(?:can|could|would)\s+you\s+(?:please\s+)?|please\s+|let'?s\s+|help\s+me\s+|i\s+(?:want|need)\s+to\s+|we\s+need\s+to\s+|go\s+ahead\s+and\s+|take\s+a\s+look\s+at\s+(?:the\s+)?(?:and\s+)?|check\s+(?:out\s+)?(?:the\s+)?|hey\s+buddy\s*,?\s*)+`,
)

var wordRe = regexp.MustCompile(`[a-zA-Z0-9]+`)
var nonAlphanumericRe = regexp.MustCompile(`[^a-z0-9-]+`)
var sanitizeRawRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// SanitizeSlug sanitizes a user-provided slug matching Python: re.sub(r"[^a-zA-Z0-9_-]+", "-", raw_slug).strip("-").lower()
func SanitizeSlug(raw string) string {
	cleaned := sanitizeRawRe.ReplaceAllString(raw, "-")
	return strings.ToLower(strings.Trim(cleaned, "-"))
}

// DeriveTaskSlug derives a slug from prompt and project name.
func DeriveTaskSlug(prompt string, projectName string) string {
	h := sha256.Sum256([]byte(prompt))
	promptHash := hex.EncodeToString(h[:])[:4]

	cleaned := strings.TrimSpace(prompt)
	for {
		loc := conversationalPrefixRe.FindStringIndex(cleaned)
		if loc != nil && loc[0] == 0 {
			cleaned = strings.TrimSpace(cleaned[loc[1]:])
		} else {
			break
		}
	}

	allWords := wordRe.FindAllString(strings.ToLower(cleaned), -1)
	var words []string
	for _, w := range allWords {
		if !SlugStopWords[w] && len(w) > 1 {
			words = append(words, w)
		}
	}

	base := ""
	if len(words) > 0 {
		end := 3
		if len(words) < 3 {
			end = len(words)
		}
		base = strings.Join(words[:end], "-")
	}
	base = strings.Trim(nonAlphanumericRe.ReplaceAllString(base, "-"), "-")
	if base != "" {
		if len(base) > 24 {
			base = strings.TrimRight(base[:24], "-")
		}
		return base + "-" + promptHash
	}

	proj := projectName
	if proj == "" {
		proj = "proj"
	}
	projSlug := strings.Trim(nonAlphanumericRe.ReplaceAllString(strings.ToLower(proj), "-"), "-")
	return projSlug + "-task-" + promptHash
}
