package setup

import "context"

// NonInteractivePrompter implements Prompter for --non-interactive, --yes, --upgrade, and CI runs.
// It never touches a TTY and returns pre-resolved defaults.
type NonInteractivePrompter struct{}

func NewNonInteractivePrompter() *NonInteractivePrompter {
	return &NonInteractivePrompter{}
}

func (p *NonInteractivePrompter) IsInteractive() bool { return false }

func (p *NonInteractivePrompter) Note(_ context.Context, _ NoteReq) error { return nil }

func (p *NonInteractivePrompter) Select(_ context.Context, req SelectReq) (string, error) {
	if req.Default != "" {
		return req.Default, nil
	}
	if len(req.Options) > 0 {
		return req.Options[0].Value, nil
	}
	return "", nil
}

func (p *NonInteractivePrompter) MultiSelect(_ context.Context, req MultiSelectReq) ([]string, error) {
	var out []string
	for _, o := range req.Options {
		if o.Selected {
			out = append(out, o.Value)
		}
	}
	return out, nil
}

func (p *NonInteractivePrompter) PickDirectories(_ context.Context, req DirPickerReq) (DirPickerResp, error) {
	var out []string
	for _, o := range req.Options {
		if o.Selected {
			out = append(out, o.Value)
		}
	}
	return DirPickerResp{Selected: out}, nil
}

func (p *NonInteractivePrompter) Input(_ context.Context, req InputReq) (string, error) {
	return req.Default, nil
}

func (p *NonInteractivePrompter) Confirm(_ context.Context, req ConfirmReq) (bool, error) {
	return req.Default, nil
}

func (p *NonInteractivePrompter) Spin(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

func (p *NonInteractivePrompter) RunConfigForm(_ context.Context, _ *ConfigFormState) error {
	return nil
}
