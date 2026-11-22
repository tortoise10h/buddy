package setup

import "context"

// MockPrompter is a deterministic, TTY-free Prompter for unit tests.
type MockPrompter struct {
	Interactive      bool
	MultiSelectResp  [][]string
	DirPickerResp    []DirPickerResp
	DirPickerReqs    []DirPickerReq
	InputResp        []string
	ConfirmResp      []bool
	SelectResp       []string
	ConfigFormFn     func(*ConfigFormState) error
	AbortAtStep      string // "multiselect", "dirpicker", "input", "confirm", "configform", "spin"
	SpinTitles       []string
	MultiSelectCalls int
	DirPickerCalls   int
	InputCalls       int
	ConfirmCalls     int
	ConfigFormCalls  int
}

func (m *MockPrompter) IsInteractive() bool { return m.Interactive }

func (m *MockPrompter) Note(_ context.Context, _ NoteReq) error { return nil }

func (m *MockPrompter) Select(_ context.Context, req SelectReq) (string, error) {
	if len(m.SelectResp) > 0 {
		v := m.SelectResp[0]
		m.SelectResp = m.SelectResp[1:]
		return v, nil
	}
	return req.Default, nil
}

func (m *MockPrompter) MultiSelect(_ context.Context, _ MultiSelectReq) ([]string, error) {
	m.MultiSelectCalls++
	if m.AbortAtStep == "multiselect" {
		return nil, ErrAborted
	}
	if len(m.MultiSelectResp) > 0 {
		v := m.MultiSelectResp[0]
		m.MultiSelectResp = m.MultiSelectResp[1:]
		return v, nil
	}
	return nil, nil
}

func (m *MockPrompter) PickDirectories(_ context.Context, req DirPickerReq) (DirPickerResp, error) {
	m.DirPickerCalls++
	m.DirPickerReqs = append(m.DirPickerReqs, req)
	if m.AbortAtStep == "dirpicker" {
		return DirPickerResp{}, ErrAborted
	}
	if len(m.DirPickerResp) > 0 {
		v := m.DirPickerResp[0]
		m.DirPickerResp = m.DirPickerResp[1:]
		return v, nil
	}
	return DirPickerResp{}, nil
}

func (m *MockPrompter) Input(_ context.Context, _ InputReq) (string, error) {
	m.InputCalls++
	if m.AbortAtStep == "input" {
		return "", ErrAborted
	}
	if len(m.InputResp) > 0 {
		v := m.InputResp[0]
		m.InputResp = m.InputResp[1:]
		return v, nil
	}
	return "", nil
}

func (m *MockPrompter) Confirm(_ context.Context, req ConfirmReq) (bool, error) {
	m.ConfirmCalls++
	if m.AbortAtStep == "confirm" {
		return false, ErrAborted
	}
	if len(m.ConfirmResp) > 0 {
		v := m.ConfirmResp[0]
		m.ConfirmResp = m.ConfirmResp[1:]
		return v, nil
	}
	return req.Default, nil
}

func (m *MockPrompter) Spin(ctx context.Context, title string, fn func(context.Context) error) error {
	m.SpinTitles = append(m.SpinTitles, title)
	if m.AbortAtStep == "spin" {
		return ErrAborted
	}
	return fn(ctx)
}

func (m *MockPrompter) RunConfigForm(_ context.Context, st *ConfigFormState) error {
	m.ConfigFormCalls++
	if m.AbortAtStep == "configform" {
		return ErrAborted
	}
	if m.ConfigFormFn != nil {
		return m.ConfigFormFn(st)
	}
	return nil
}
