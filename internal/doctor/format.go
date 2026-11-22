package doctor

import (
	"fmt"
	"io"
)

// FormatHuman writes a grouped, scannable diagnostic report to w.
func FormatHuman(w io.Writer, r *Report) {
	if r == nil {
		return
	}
	lastCat := ""
	for _, c := range r.Checks {
		if c.Category != lastCat {
			if lastCat != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n", c.Category)
			lastCat = c.Category
		}
		icon := "ℹ"
		switch c.Status {
		case StatusPass:
			icon = "✔"
		case StatusWarn:
			icon = "⚠"
		case StatusFail:
			icon = "✖"
		}
		fixedTag := ""
		if c.Fixed {
			fixedTag = " [fixed]"
		}
		fmt.Fprintf(w, "  %s %-26s %s%s\n", icon, c.Name, c.Message, fixedTag)
		if c.FixHint != "" {
			fmt.Fprintf(w, "      → %s\n", c.FixHint)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d passed, %d warnings, %d failed\n",
		r.Summary.Pass, r.Summary.Warn, r.Summary.Fail)
}
