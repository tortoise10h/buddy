package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// HuhPrompter implements Prompter using charm.land/huh/v2.
type HuhPrompter struct {
	in         io.Reader
	out        io.Writer
	accessible bool
	theme      huh.Theme
}

// NewHuhPrompter creates an interactive HuhPrompter with BuddyTheme.
func NewHuhPrompter(in io.Reader, out io.Writer, accessible bool) *HuhPrompter {
	return &HuhPrompter{
		in:         in,
		out:        out,
		accessible: accessible,
		theme:      BuddyTheme(),
	}
}

func (p *HuhPrompter) IsInteractive() bool { return true }

func mapHuhErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled) {
		return ErrAborted
	}
	return err
}

func (p *HuhPrompter) runForm(ctx context.Context, groups ...*huh.Group) error {
	f := huh.NewForm(groups...).
		WithTheme(p.theme).
		WithAccessible(p.accessible).
		WithShowHelp(true).
		WithShowErrors(true)
	if p.in != nil {
		f = f.WithInput(p.in)
	}
	if p.out != nil {
		f = f.WithOutput(p.out)
	}
	return mapHuhErr(f.RunWithContext(ctx))
}

// Spin runs fn with a lightweight, non-raw-mode Braille spinner when fn takes longer than 120ms.
// Avoids launching a short-lived Bubble Tea v2 program, which would emit DECRPM/OSC 11 terminal
// capability queries whose async replies leak to stdout/shell stdin after raw mode exits.
func (p *HuhPrompter) Spin(ctx context.Context, title string, fn func(context.Context) error) error {
	if p.accessible || p.out == nil {
		if p.out != nil {
			fmt.Fprintf(p.out, "• %s\n", title)
		}
		return mapHuhErr(fn(ctx))
	}

	done := make(chan error, 1)
	go func() {
		done <- fn(ctx)
	}()

	// Grace period: fast local operations (<120ms) finish cleanly with zero terminal flicker.
	grace := time.NewTimer(120 * time.Millisecond)
	defer grace.Stop()

	select {
	case err := <-done:
		return mapHuhErr(err)
	case <-grace.C:
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	idx := 0
	fmt.Fprintf(p.out, "\r%s %s", frames[idx], title)
	for {
		select {
		case err := <-done:
			fmt.Fprint(p.out, "\r\033[2K")
			return mapHuhErr(err)
		case <-ticker.C:
			idx = (idx + 1) % len(frames)
			fmt.Fprintf(p.out, "\r%s %s", frames[idx], title)
		}
	}
}

func (p *HuhPrompter) Note(ctx context.Context, req NoteReq) error {
	n := huh.NewNote().Title(req.Title).Description(req.Body)
	return p.runForm(ctx, huh.NewGroup(n))
}

func (p *HuhPrompter) Select(ctx context.Context, req SelectReq) (string, error) {
	val := req.Default
	opts := make([]huh.Option[string], 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, huh.NewOption(o.Label, o.Value))
	}
	sel := huh.NewSelect[string]().
		Title(req.Title).
		Description(req.Description).
		Options(opts...).
		Value(&val)
	if err := p.runForm(ctx, huh.NewGroup(sel)); err != nil {
		return "", err
	}
	return val, nil
}

// multiSelectHeight computes an explicit .Height(...) for huh.NewMultiSelect to work around
// a huh/v2@v2.0.3 bug where MultiSelect.updateViewportSize() subtracts title+description lines
// (yoffset) from lipgloss.Height(options) even when height is unset, clipping small option lists
// down to 1 line.
func multiSelectHeight(optionCount int, hasDescription bool) int {
	const maxVisible = 12
	visible := optionCount
	if visible < 1 {
		visible = 1
	}
	if visible > maxVisible {
		visible = maxVisible
	}
	yoffset := 1 // title line
	if hasDescription {
		yoffset++ // 1-line description
	}
	return visible + yoffset
}

func (p *HuhPrompter) MultiSelect(ctx context.Context, req MultiSelectReq) ([]string, error) {
	var vals []string
	opts := make([]huh.Option[string], 0, len(req.Options))
	for _, o := range req.Options {
		opt := huh.NewOption(o.Label, o.Value)
		if o.Selected {
			opt = opt.Selected(true)
		}
		opts = append(opts, opt)
	}
	ms := huh.NewMultiSelect[string]().
		Title(req.Title).
		Description(req.Description).
		Options(opts...).
		Height(multiSelectHeight(len(opts), strings.TrimSpace(req.Description) != "")).
		Value(&vals)
	if req.MinSelected > 0 {
		ms = ms.Validate(func(selected []string) error {
			if len(selected) < req.MinSelected {
				return fmt.Errorf("select at least %d option(s)", req.MinSelected)
			}
			return nil
		})
	}
	if err := p.runForm(ctx, huh.NewGroup(ms)); err != nil {
		return nil, err
	}
	return vals, nil
}

type dirPickerModel struct {
	form         *huh.Form
	ms           *huh.MultiSelect[string]
	addRequested bool
}

func (m *dirPickerModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m *dirPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		if !m.ms.GetFiltering() {
			switch kp.String() {
			case "+", "a":
				m.addRequested = true
				return m, tea.Quit
			}
		}
	}
	model, cmd := m.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		m.form = f
	}
	if m.form.State == huh.StateCompleted || m.form.State == huh.StateAborted {
		return m, tea.Quit
	}
	return m, cmd
}

func (m *dirPickerModel) View() tea.View {
	if m.addRequested || m.form.State != huh.StateNormal {
		return tea.NewView("")
	}
	return tea.NewView(m.form.View())
}

// PickDirectories renders the Step 1 directory list with immediate '+' / 'a' keybinding
// to jump straight into adding a new directory without an extra checkbox option.
func (p *HuhPrompter) PickDirectories(ctx context.Context, req DirPickerReq) (DirPickerResp, error) {
	if p.accessible {
		selected, err := p.MultiSelect(ctx, MultiSelectReq{
			Title:       req.Title,
			Description: req.Description,
			Options:     req.Options,
		})
		if err != nil {
			return DirPickerResp{}, err
		}
		addMore, err := p.Confirm(ctx, ConfirmReq{
			Title:       "Add another project directory?",
			Affirmative: "Add directory",
			Negative:    "Continue",
			Default:     false,
		})
		if err != nil {
			return DirPickerResp{}, err
		}
		return DirPickerResp{Selected: selected, AddRequested: addMore}, nil
	}

	var vals []string
	opts := make([]huh.Option[string], 0, len(req.Options))
	for _, o := range req.Options {
		opt := huh.NewOption(o.Label, o.Value)
		if o.Selected {
			opt = opt.Selected(true)
			vals = append(vals, o.Value)
		}
		opts = append(opts, opt)
	}

	ms := huh.NewMultiSelect[string]().
		Title(req.Title).
		Description(req.Description).
		Options(opts...).
		Filterable(false).
		Height(multiSelectHeight(len(opts), strings.TrimSpace(req.Description) != "")).
		Value(&vals)

	form := huh.NewForm(huh.NewGroup(ms)).
		WithTheme(p.theme).
		WithShowHelp(true).
		WithShowErrors(true)

	m := &dirPickerModel{form: form, ms: ms}
	teaOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if p.in != nil {
		teaOpts = append(teaOpts, tea.WithInput(p.in))
	}
	if p.out != nil {
		teaOpts = append(teaOpts, tea.WithOutput(p.out))
	}

	finalModel, err := tea.NewProgram(m, teaOpts...).Run()
	if err != nil {
		return DirPickerResp{}, mapHuhErr(err)
	}
	dm, _ := finalModel.(*dirPickerModel)
	if dm != nil && dm.form.State == huh.StateAborted {
		return DirPickerResp{}, ErrAborted
	}
	addReq := dm != nil && dm.addRequested
	return DirPickerResp{
		Selected:     vals,
		AddRequested: addReq,
	}, nil
}

func (p *HuhPrompter) Input(ctx context.Context, req InputReq) (string, error) {
	val := req.Default
	inp := huh.NewInput().
		Title(req.Title).
		Description(req.Description).
		Placeholder(req.Placeholder).
		Value(&val)
	if req.Validate != nil {
		inp = inp.Validate(req.Validate)
	}
	if err := p.runForm(ctx, huh.NewGroup(inp)); err != nil {
		return "", err
	}
	return strings.TrimSpace(val), nil
}

func (p *HuhPrompter) Confirm(ctx context.Context, req ConfirmReq) (bool, error) {
	val := req.Default
	aff := req.Affirmative
	if aff == "" {
		aff = "Yes"
	}
	neg := req.Negative
	if neg == "" {
		neg = "No"
	}
	c := huh.NewConfirm().
		Title(req.Title).
		Description(req.Description).
		Affirmative(aff).
		Negative(neg).
		Value(&val)
	if err := p.runForm(ctx, huh.NewGroup(c)); err != nil {
		return false, err
	}
	return val, nil
}

func (p *HuhPrompter) RunConfigForm(ctx context.Context, st *ConfigFormState) error {
	showRex := func() bool {
		return (st.ShowAgents || st.ShowEngines) && st.HasSelectedEngine("rex")
	}
	showOpenCode := func() bool {
		return st.ShowEngines && st.HasSelectedEngine("opencode")
	}
	showT3 := func() bool {
		return st.ShowEngines && st.HasSelectedEngine("t3")
	}

	hasVisibleGroup := st.ShowEngines || showRex() || st.ShowAdvanced || (st.ShowSkills && !st.ShowAdvanced)
	if !hasVisibleGroup {
		return nil
	}

	// Step 2: Platforms / Engines multi-select (ONLY rex, opencode, t3)
	platformOpts := buildPlatformOptions(st.Probe, st.SelectedEngines)
	groupPlatforms := huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Step 2/5 · Platforms").
			Description("Space to toggle platforms, Enter to confirm.").
			Options(platformOpts...).
			Height(multiSelectHeight(len(platformOpts), true)).
			Value(&st.SelectedEngines).
			Validate(func(selected []string) error {
				if len(selected) == 0 {
					return fmt.Errorf("select at least one platform")
				}
				return nil
			}),
	).WithHideFunc(func() bool { return !st.ShowEngines })

	// Step 3a: Rex config (ONLY if rex was selected in Step 2)
	groupRex := huh.NewGroup(
		huh.NewSelect[string]().
			Title("Step 3/5 · Rex orchestrator agent").
			Description("Runs the Buddy Control Tower when you invoke `buddy`.").
			Options(buildHarnessOptions(st.Probe, st.OrchestratorAgent)...).
			Value(&st.OrchestratorAgent),
		huh.NewSelect[string]().
			Title("Step 3/5 · Rex worker agent").
			Description("Default agent for new Rex task tabs (`buddy-ctl dispatch`).").
			Options(buildHarnessOptions(st.Probe, st.WorkerAgent)...).
			Value(&st.WorkerAgent),
	).WithHideFunc(func() bool { return !showRex() })

	groupCustomOrch := huh.NewGroup(
		huh.NewInput().
			Title("Custom orchestrator harness name").
			Placeholder("e.g. my-agent").
			Value(&st.OrchestratorCustom).
			Validate(validateNonEmptyToken),
	).WithHideFunc(func() bool {
		return !showRex() || st.OrchestratorAgent != CustomSentinel
	})

	groupCustomWorker := huh.NewGroup(
		huh.NewInput().
			Title("Custom worker harness name").
			Placeholder("e.g. my-agent").
			Value(&st.WorkerCustom).
			Validate(validateNonEmptyToken),
	).WithHideFunc(func() bool {
		return !showRex() || st.WorkerAgent != CustomSentinel
	})

	// Step 3b: OpenCode config (ONLY if opencode was selected in Step 2)
	groupOpenCode := huh.NewGroup(
		huh.NewInput().
			Title("Step 3/5 · OpenCode default worker model (optional)").
			Description(openCodeGroupDescription(st.Probe)).
			Placeholder("Leave empty to use OpenCode server default").
			Value(&st.OpenCodeModel).
			Validate(validateOptionalProviderModel),
	).WithHideFunc(func() bool { return !showOpenCode() })

	// Step 3c: T3 config (ONLY if t3 was selected in Step 2)
	groupT3 := huh.NewGroup(
		huh.NewInput().
			Title("Step 3/5 · T3 default worker model (optional)").
			Description("Format: <provider>/<model>. Press Enter to skip and configure later.").
			Placeholder("antigravity/gemini-3.8-flash-medium").
			Value(&st.T3ModelSpec).
			Validate(validateOptionalProviderModel),
	).WithHideFunc(func() bool { return !showT3() })

	// Step 4: Optional / Advanced Config confirm gate (default No)
	groupAdvancedGate := huh.NewGroup(
		huh.NewConfirm().
			Title("Step 4/5 · Configure optional / advanced settings?").
			Description("Desktop notifications (default: off) and agent skill targets (default: auto-detected).").
			Affirmative("Yes, configure").
			Negative("No, use defaults").
			Value(&st.ConfigureAdvanced),
	).WithHideFunc(func() bool { return !st.ShowAdvanced })

	// Step 4b: Advanced fields (ONLY shown if user answered Yes to Step 4)
	skillOpts := buildSkillTargetOptions(st.Probe, st.SkillTargets)
	advFields := []huh.Field{
		huh.NewConfirm().
			Title("Enable desktop notifications?").
			Description("Send macOS desktop alerts when workers finish or need input.").
			Affirmative("Enable").
			Negative("Disable").
			Value(&st.NotificationsEnabled),
	}
	if st.ShowSkills && len(skillOpts) > 0 {
		advFields = append(advFields,
			huh.NewMultiSelect[string]().
				Title("Install Buddy agent skill into").
				Description("Space to toggle skill directories, Enter to confirm.").
				Options(skillOpts...).
				Height(multiSelectHeight(len(skillOpts), true)).
				Value(&st.SkillTargets),
		)
	}
	groupAdvancedFields := huh.NewGroup(advFields...).WithHideFunc(func() bool {
		return !st.ShowAdvanced || !st.ConfigureAdvanced
	})

	// Standalone skills group only when `--only skills` is explicitly requested (ShowSkills && !ShowAdvanced)
	groupSkillsOnly := huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Install Buddy agent skill into").
			Description("Space to toggle skill directories, Enter to confirm.").
			Options(skillOpts...).
			Height(multiSelectHeight(len(skillOpts), true)).
			Value(&st.SkillTargets),
	).WithHideFunc(func() bool {
		return !st.ShowSkills || st.ShowAdvanced || len(skillOpts) == 0
	})

	return p.runForm(ctx,
		groupPlatforms,
		groupRex,
		groupCustomOrch,
		groupCustomWorker,
		groupOpenCode,
		groupT3,
		groupAdvancedGate,
		groupAdvancedFields,
		groupSkillsOnly,
	)
}

func buildPlatformOptions(probe *ProbeResult, selected []string) []huh.Option[string] {
	selSet := make(map[string]bool, len(selected))
	for _, s := range selected {
		selSet[s] = true
	}

	mark := func(ok bool, note string) string {
		if ok {
			if note != "" {
				return "✔ (" + note + ")"
			}
			return "✔ (installed)"
		}
		return "✖ (not installed)"
	}

	ocNote := "installed"
	if probe != nil && probe.OpenCodeDesktopApp {
		ocNote = "OpenCode Desktop"
	}

	hasRex := probe != nil && probe.HasRex
	hasOC := probe != nil && probe.HasOpenCode
	hasT3 := probe != nil && probe.HasT3

	opts := []huh.Option[string]{
		huh.NewOption("rex       "+mark(hasRex, "installed"), "rex").Selected(selSet["rex"]),
		huh.NewOption("opencode  "+mark(hasOC, ocNote), "opencode").Selected(selSet["opencode"]),
		huh.NewOption("t3        "+mark(hasT3, "installed"), "t3").Selected(selSet["t3"]),
	}
	return opts
}

func openCodeGroupDescription(probe *ProbeResult) string {
	if probe != nil && probe.OpenCodeDesktopApp {
		return "OpenCode Desktop detected. Format: <provider>/<model> (press Enter to use server default)."
	}
	return "Format: <provider>/<model> (press Enter to use OpenCode server default)."
}

func buildHarnessOptions(probe *ProbeResult, current string) []huh.Option[string] {
	var opts []huh.Option[string]
	seen := make(map[string]bool)

	if probe != nil && len(probe.InstalledHarnesses) > 0 {
		for _, h := range probe.InstalledHarnesses {
			label := h
			if p, ok := probe.HarnessPaths[h]; ok && p != "" {
				label = fmt.Sprintf("%-10s (%s)", h, FormatHomePath(p, probe.HomeDir))
			}
			opts = append(opts, huh.NewOption(label, h))
			seen[h] = true
		}
	} else {
		for _, h := range HarnessCandidates {
			opts = append(opts, huh.NewOption(h+" (not on PATH)", h))
			seen[h] = true
		}
	}

	if current != "" && current != CustomSentinel && !seen[current] {
		opts = append(opts, huh.NewOption(current+" (configured)", current))
	}

	opts = append(opts, huh.NewOption("Custom harness...", CustomSentinel))
	return opts
}

func buildSkillTargetOptions(probe *ProbeResult, selected []string) []huh.Option[string] {
	selSet := make(map[string]bool, len(selected))
	for _, s := range selected {
		selSet[s] = true
	}
	opts := make([]huh.Option[string], 0, len(probe.SkillTargets))
	for _, t := range probe.SkillTargets {
		label := fmt.Sprintf("%-22s (%s)", t.Name, t.DisplayDir)
		opt := huh.NewOption(label, t.Dir)
		if selSet[t.Dir] {
			opt = opt.Selected(true)
		}
		opts = append(opts, opt)
	}
	return opts
}
