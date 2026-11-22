package setup

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands leading "~" or "~/" against homeDir and cleans the path.
// If homeDir is empty, os.UserHomeDir() is used.
func ExpandPath(raw, homeDir string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	if p == "~" && homeDir != "" {
		return filepath.Clean(homeDir)
	}
	if strings.HasPrefix(p, "~/") && homeDir != "" {
		p = filepath.Join(homeDir, p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}

// FormatHomePath shortens paths under homeDir to "~/..." for display.
func FormatHomePath(path, homeDir string) string {
	if path == "" {
		return ""
	}
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}
	if homeDir == "" {
		return path
	}
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(homeDir)
	if cleanPath == cleanHome {
		return "~"
	}
	prefix := cleanHome + string(filepath.Separator)
	if strings.HasPrefix(cleanPath, prefix) {
		return "~" + string(filepath.Separator) + strings.TrimPrefix(cleanPath, prefix)
	}
	return cleanPath
}

// ParseRootInputs parses user-entered project root paths separated by commas or spaces,
// preserving quoted paths and backslash-escaped spaces (pure Go replacement for shlex).
func ParseRootInputs(raw string) []string {
	var out []string
	for _, seg := range splitOutsideQuotes(raw, ',') {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if strings.ContainsAny(seg, `"'\\`) {
			tokens := shellSplit(seg)
			for _, tok := range tokens {
				tok = strings.TrimSpace(tok)
				if tok != "" {
					out = append(out, tok)
				}
			}
			continue
		}
		// Unquoted segment: split on whitespace only when followed by a path-like prefix
		// (~, ~/, /, ./, ../) so unquoted paths with spaces like "/tmp/Mobile Documents" survive.
		tokens := splitSpaceBeforePathPrefix(seg)
		for _, tok := range tokens {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				out = append(out, tok)
			}
		}
	}
	return out
}

// SplitEnvRoots splits BUDDY_PROJECTS_ROOTS on ":" or ",".
func SplitEnvRoots(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ':' || r == ','
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// MergeUniqueStrings merges string slices preserving first-seen order and dropping empties.
func MergeUniqueStrings(lists ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range lists {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

func splitOutsideQuotes(s string, sep rune) []string {
	var parts []string
	var cur strings.Builder
	var quote rune
	escaped := false

	for _, ch := range s {
		switch {
		case escaped:
			cur.WriteRune(ch)
			escaped = false
		case ch == '\\' && quote != '\'':
			cur.WriteRune(ch)
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			}
			cur.WriteRune(ch)
		case ch == '"' || ch == '\'':
			quote = ch
			cur.WriteRune(ch)
		case ch == sep:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

func shellSplit(s string) []string {
	var tokens []string
	var cur strings.Builder
	var quote rune
	escaped := false
	inToken := false

	for _, ch := range s {
		switch {
		case escaped:
			cur.WriteRune(ch)
			escaped = false
			inToken = true
		case ch == '\\' && quote != '\'':
			escaped = true
			inToken = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteRune(ch)
			}
			inToken = true
		case ch == '"' || ch == '\'':
			quote = ch
			inToken = true
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if inToken {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(ch)
			inToken = true
		}
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func splitSpaceBeforePathPrefix(s string) []string {
	var parts []string
	start := 0
	i := 0
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' {
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && hasPathPrefix(s[j:]) {
				parts = append(parts, s[start:i])
				start = j
				i = j
				continue
			}
			i = j
			continue
		}
		i++
	}
	parts = append(parts, s[start:])
	return parts
}

func hasPathPrefix(s string) bool {
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	return false
}
