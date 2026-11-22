package setup

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// BuddyTheme returns an adaptive light/dark huh theme derived from ThemeCharm
// with Buddy's accent palette.
func BuddyTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeCharm(isDark)
		lightDark := lipgloss.LightDark(isDark)

		accent := lightDark(lipgloss.Color("#0284C7"), lipgloss.Color("#38BDF8"))
		muted := lightDark(lipgloss.Color("#64748B"), lipgloss.Color("#94A3B8"))
		success := lightDark(lipgloss.Color("#16A34A"), lipgloss.Color("#4ADE80"))

		t.Focused.Base = t.Focused.Base.BorderForeground(accent)
		t.Focused.Title = t.Focused.Title.Foreground(accent).Bold(true)
		t.Focused.Description = t.Focused.Description.Foreground(muted)
		t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(success)
		t.Focused.FocusedButton = t.Focused.FocusedButton.Background(accent)
		return t
	})
}

// FormatProbeBanner renders a compact capability overview for Step 0.
func FormatProbeBanner(version string, probe *ProbeResult) string {
	var b strings.Builder
	mode := "First-time setup"
	if probe.ConfigExists {
		mode = "Reconfiguring existing setup"
	}
	if version == "" {
		version = "dev"
	}
	fmt.Fprintf(&b, "Buddy Setup (%s) — %s\n", version, mode)
	b.WriteString("───────────────────────────────────────────────────────\n")

	mark := func(ok bool) string {
		if ok {
			return "✔"
		}
		return "✖"
	}
	fmt.Fprintf(&b, "Detected platforms: rex %s   opencode %s   t3 %s\n",
		mark(probe.HasRex), mark(probe.HasOpenCode), mark(probe.HasT3))

	if len(probe.InstalledHarnesses) > 0 {
		var parts []string
		for _, h := range probe.InstalledHarnesses {
			parts = append(parts, h+" ✔")
		}
		fmt.Fprintf(&b, "Detected harnesses: %s\n", strings.Join(parts, "   "))
	} else {
		b.WriteString("Detected harnesses: none found on PATH\n")
	}

	if probe.DetectedHost != "" && probe.DetectedHost != "unknown" {
		fmt.Fprintf(&b, "Current host:       %s\n", probe.DetectedHost)
	}
	return strings.TrimRight(b.String(), "\n")
}
