package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
	"github.com/tortoise10h/buddy/internal/backend/opencode"
)

type mockOpenCodeClient struct {
	createReq    opencode.CreateSessionRequest
	createdSess  *opencode.SessionInfo
	createErr    error
	promptID     string
	promptReq    opencode.PromptRequest
	promptErr    error
	interrupted  bool
	interruptID  string
	interruptErr error
	updatedID    string
	updateReq    opencode.UpdateSessionRequest
	updatedSess  *opencode.SessionInfo
	updateErr    error

	session     *opencode.SessionInfo
	sessionErr  error
	activeMap   map[string]opencode.SessionActive
	activeErr   error
	messages    []opencode.MessageInfo
	messagesErr error
	permissions []opencode.PermissionRequest
	forms       []opencode.FormRequest
	sessions    []opencode.SessionInfo
}

func (m *mockOpenCodeClient) Info(ctx context.Context) (*opencode.ServerInfo, error) {
	return &opencode.ServerInfo{Version: "2.3.0"}, nil
}
func (m *mockOpenCodeClient) ListProjects(ctx context.Context) ([]opencode.Project, error) {
	return nil, nil
}
func (m *mockOpenCodeClient) ListSessions(ctx context.Context, opts opencode.ListSessionsOptions) ([]opencode.SessionInfo, error) {
	return m.sessions, nil
}
func (m *mockOpenCodeClient) CreateSession(ctx context.Context, req opencode.CreateSessionRequest) (*opencode.SessionInfo, error) {
	m.createReq = req
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.createdSess, nil
}
func (m *mockOpenCodeClient) GetActiveSessions(ctx context.Context) (map[string]opencode.SessionActive, error) {
	if m.activeErr != nil {
		return nil, m.activeErr
	}
	if m.activeMap == nil {
		return map[string]opencode.SessionActive{}, nil
	}
	return m.activeMap, nil
}
func (m *mockOpenCodeClient) GetSession(ctx context.Context, sessionID string) (*opencode.SessionInfo, error) {
	if m.sessionErr != nil {
		return nil, m.sessionErr
	}
	return m.session, nil
}
func (m *mockOpenCodeClient) UpdateSession(ctx context.Context, sessionID string, req opencode.UpdateSessionRequest) (*opencode.SessionInfo, error) {
	m.updatedID = sessionID
	m.updateReq = req
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	if m.updatedSess != nil {
		return m.updatedSess, nil
	}
	return &opencode.SessionInfo{ID: sessionID}, nil
}
func (m *mockOpenCodeClient) Prompt(ctx context.Context, sessionID string, req opencode.PromptRequest) error {
	m.promptID = sessionID
	m.promptReq = req
	return m.promptErr
}
func (m *mockOpenCodeClient) Interrupt(ctx context.Context, sessionID string) error {
	m.interrupted = true
	m.interruptID = sessionID
	return m.interruptErr
}
func (m *mockOpenCodeClient) ListMessages(ctx context.Context, sessionID string, limit int, order string) ([]opencode.MessageInfo, error) {
	if m.messagesErr != nil {
		return nil, m.messagesErr
	}
	return m.messages, nil
}
func (m *mockOpenCodeClient) ListPermissions(ctx context.Context, sessionID string) ([]opencode.PermissionRequest, error) {
	return m.permissions, nil
}
func (m *mockOpenCodeClient) ListForms(ctx context.Context, sessionID string) ([]opencode.FormRequest, error) {
	return m.forms, nil
}
func (m *mockOpenCodeClient) SubscribeEvents(ctx context.Context, types []string) (<-chan opencode.SSEEvent, error) {
	return nil, nil
}

func TestOpenCodeEngine_Dispatch(t *testing.T) {
	mock := &mockOpenCodeClient{
		createdSess: &opencode.SessionInfo{
			ID:        "ses_abc123",
			ProjectID: "proj_xyz",
			Title:     "feat-auth",
		},
	}
	eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{DefaultAgent: "build"})

	res, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "buddy",
		ProjectPath: "/tmp/buddy",
		TaskSlug:    "feat-auth",
		Prompt:      "implement oauth",
		AgentCmd:    []string{"opencode-go/deepseek-v4.1-flash#high"},
	})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if res.WindowID != "ses_abc123" || res.BlockID != "ses_abc123" || res.SessionID != "proj_xyz" || !res.PickedUp {
		t.Fatalf("unexpected DispatchResult: %+v", res)
	}
	if mock.createReq.Location == nil || mock.createReq.Location.Directory != "/tmp/buddy" {
		t.Fatalf("expected directory /tmp/buddy, got %+v", mock.createReq.Location)
	}
	if mock.createReq.Agent != "build" {
		t.Fatalf("expected agent build, got %q", mock.createReq.Agent)
	}
	if mock.createReq.Model == nil || mock.createReq.Model.ProviderID != "opencode-go" || mock.createReq.Model.ID != "deepseek-v4.1-flash" || mock.createReq.Model.Variant != "high" {
		t.Fatalf("unexpected ModelRef: %+v", mock.createReq.Model)
	}
	if mock.promptID != "ses_abc123" || mock.promptReq.Text != "implement oauth" {
		t.Fatalf("unexpected prompt call: id=%q req=%+v", mock.promptID, mock.promptReq)
	}
	if mock.promptReq.Model == nil || mock.promptReq.Model.ProviderID != "opencode-go" || mock.promptReq.Model.ID != "deepseek-v4.1-flash" || mock.promptReq.Model.Variant != "high" {
		t.Fatalf("expected PromptRequest.Model to be propagated on Dispatch, got %+v", mock.promptReq.Model)
	}
}

func TestOpenCodeEngine_Dispatch_ExplicitModelPreservesDefaultAgent(t *testing.T) {
	mock := &mockOpenCodeClient{
		createdSess: &opencode.SessionInfo{
			ID:        "ses_model1",
			ProjectID: "proj_xyz",
			Title:     "feat-model",
		},
	}
	eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{
		DefaultAgent: "build",
		DefaultModel: "opencode-go/default-model",
	})

	_, err := eng.Dispatch(context.Background(), DispatchRequest{
		ProjectName: "buddy",
		ProjectPath: "/tmp/buddy",
		TaskSlug:    "feat-model",
		Prompt:      "refactor lock",
		Model:       "opencode-go/deepseek-v4.1-flash#high",
	})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if mock.createReq.Agent != "build" {
		t.Fatalf("expected DefaultAgent 'build' to be preserved, got %q", mock.createReq.Agent)
	}
	if mock.createReq.Model == nil || mock.createReq.Model.ProviderID != "opencode-go" || mock.createReq.Model.ID != "deepseek-v4.1-flash" || mock.createReq.Model.Variant != "high" {
		t.Fatalf("unexpected ModelRef from req.Model: %+v", mock.createReq.Model)
	}
}

func TestOpenCodeEngine_SendTextAndInterrupt(t *testing.T) {
	mock := &mockOpenCodeClient{}
	eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{
		DefaultModel: "opencode-go/mimo-v2.6-flash",
	})

	if err := eng.Send(context.Background(), "", "ses_1", backend.Input{Text: "continue"}); err != nil {
		t.Fatalf("Send text failed: %v", err)
	}
	if mock.promptID != "ses_1" || mock.promptReq.Text != "continue" {
		t.Fatalf("unexpected prompt: %+v", mock.promptReq)
	}
	if mock.promptReq.Model == nil || mock.promptReq.Model.ProviderID != "opencode-go" || mock.promptReq.Model.ID != "mimo-v2.6-flash" {
		t.Fatalf("expected PromptRequest.Model to be propagated on Send, got %+v", mock.promptReq.Model)
	}

	if err := eng.Send(context.Background(), "", "ses_1", backend.Input{Key: "ctrl-c"}); err != nil {
		t.Fatalf("Send ctrl-c failed: %v", err)
	}
	if !mock.interrupted || mock.interruptID != "ses_1" {
		t.Fatalf("expected interrupt on ses_1")
	}

	if err := eng.Send(context.Background(), "", "ses_1", backend.Input{Key: "tab"}); err == nil {
		t.Fatalf("expected error for unsupported key")
	}
}

func TestOpenCodeEngine_ProbeStatusTransitions(t *testing.T) {
	t.Run("pending permission -> waiting_for_input", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session:     &opencode.SessionInfo{ID: "ses_1", Agent: "build"},
			activeMap:   map[string]opencode.SessionActive{"ses_1": {Type: "running"}},
			permissions: []opencode.PermissionRequest{{ID: "p1", Action: "bash", Resource: "git push"}},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "waiting_for_input" || probe.Working || probe.Event != "waiting_for_input" {
			t.Fatalf("unexpected probe: %+v", probe)
		}
	})

	t.Run("pending form -> waiting_for_input", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session:   &opencode.SessionInfo{ID: "ses_1", Agent: "build"},
			activeMap: map[string]opencode.SessionActive{"ses_1": {Type: "running"}},
			forms:     []opencode.FormRequest{{ID: "f1", Title: "Choose database"}},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "waiting_for_input" || probe.Summary != "Choose database" {
			t.Fatalf("unexpected probe: %+v", probe)
		}
	})

	t.Run("active plan agent -> planning", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session:   &opencode.SessionInfo{ID: "ses_1", Agent: "plan", Title: "Design API"},
			activeMap: map[string]opencode.SessionActive{"ses_1": {Type: "running"}},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "planning" || !probe.Working {
			t.Fatalf("unexpected probe: %+v", probe)
		}
	})

	t.Run("active build agent -> running", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session:   &opencode.SessionInfo{ID: "ses_1", Agent: "build", Title: "Implement auth"},
			activeMap: map[string]opencode.SessionActive{"ses_1": {Type: "running"}},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "running" || !probe.Working {
			t.Fatalf("unexpected probe: %+v", probe)
		}
	})

	t.Run("idle succeeded -> ready_for_demo", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session: &opencode.SessionInfo{ID: "ses_1", Agent: "build", Outcome: "succeeded"},
			messages: []opencode.MessageInfo{
				{ID: "m1", Type: "user", Text: "fix bug"},
				{ID: "m2", Type: "assistant", Content: []opencode.MessagePart{{Type: "text", Text: "Fixed the bug and all tests pass."}}},
			},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "ready_for_demo" || probe.Working || probe.Event != "ready_for_demo" {
			t.Fatalf("unexpected probe: %+v", probe)
		}
		if !strings.Contains(probe.Summary, "Fixed the bug") {
			t.Fatalf("unexpected summary: %q", probe.Summary)
		}
	})

	t.Run("idle failed -> error with message", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			session: &opencode.SessionInfo{ID: "ses_1", Outcome: "failed"},
			messages: []opencode.MessageInfo{
				{ID: "m2", Type: "assistant", Error: json.RawMessage(`{"message":"rate limit exceeded"}`)},
			},
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if probe.Status != "error" || probe.Event != "error" || probe.Summary != "rate limit exceeded" {
			t.Fatalf("unexpected probe: %+v", probe)
		}
	})

	t.Run("server unreachable -> unknown + error", func(t *testing.T) {
		mock := &mockOpenCodeClient{
			activeErr: opencode.ErrServiceNotRunning,
		}
		eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
		probe, err := eng.ProbeStatus(context.Background(), "", "ses_1")
		if err == nil || probe.Status != "unknown" {
			t.Fatalf("expected unknown+err, got probe=%+v err=%v", probe, err)
		}
	})
}

func TestOpenCodeEngine_CloseNonDestructiveAndOpenDevSplitUnsupported(t *testing.T) {
	// Running session is interrupted and archived on Close
	mock := &mockOpenCodeClient{
		activeMap: map[string]opencode.SessionActive{"ses_active": {Type: "running"}},
	}
	eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
	if err := eng.Close(context.Background(), "", "ses_active"); err != nil {
		t.Fatalf("Close running failed: %v", err)
	}
	if !mock.interrupted || mock.interruptID != "ses_active" {
		t.Fatalf("expected active session to be interrupted on Close")
	}
	if mock.updatedID != "ses_active" || mock.updateReq.Archived == nil || !*mock.updateReq.Archived {
		t.Fatalf("expected active session to be archived on Close, got id=%q req=%+v", mock.updatedID, mock.updateReq)
	}

	// Idle session is not interrupted, but is archived
	mockIdle := &mockOpenCodeClient{}
	engIdle := NewOpenCodeEngineWithClient(mockIdle, OpenCodeEngineOptions{})
	if err := engIdle.Close(context.Background(), "", "ses_idle"); err != nil {
		t.Fatalf("Close idle failed: %v", err)
	}
	if mockIdle.interrupted {
		t.Fatalf("did not expect idle session to be interrupted")
	}
	if mockIdle.updatedID != "ses_idle" || mockIdle.updateReq.Archived == nil || !*mockIdle.updateReq.Archived {
		t.Fatalf("expected idle session to be archived on Close, got id=%q req=%+v", mockIdle.updatedID, mockIdle.updateReq)
	}

	// 404 on archive is ignored idempotently
	mockMissing := &mockOpenCodeClient{updateErr: opencode.ErrSessionNotFound}
	engMissing := NewOpenCodeEngineWithClient(mockMissing, OpenCodeEngineOptions{})
	if err := engMissing.Close(context.Background(), "", "ses_missing"); err != nil {
		t.Fatalf("Close 404 should succeed idempotently, got %v", err)
	}

	// Offline server is ignored on Close (both GetActiveSessions and UpdateSession)
	mockOffline := &mockOpenCodeClient{activeErr: opencode.ErrServiceNotRunning}
	engOffline := NewOpenCodeEngineWithClient(mockOffline, OpenCodeEngineOptions{})
	if err := engOffline.Close(context.Background(), "", "ses_1"); err != nil {
		t.Fatalf("Close offline should succeed idempotently, got %v", err)
	}
	mockOfflineUpdate := &mockOpenCodeClient{updateErr: opencode.ErrServiceNotRunning}
	engOfflineUpdate := NewOpenCodeEngineWithClient(mockOfflineUpdate, OpenCodeEngineOptions{})
	if err := engOfflineUpdate.Close(context.Background(), "", "ses_1"); err != nil {
		t.Fatalf("Close offline on UpdateSession should succeed idempotently, got %v", err)
	}

	// OpenDevSplit returns ErrNotImplemented
	_, err := eng.OpenDevSplit(context.Background(), "", "ses_1", "/tmp", "npm run dev")
	var notImpl *ErrNotImplemented
	if !errors.As(err, &notImpl) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

func TestOpenCodeEngine_CaptureFormatting(t *testing.T) {
	mock := &mockOpenCodeClient{
		session: &opencode.SessionInfo{
			ID:      "ses_cap",
			Agent:   "build",
			Title:   "Refactor",
			Outcome: "succeeded",
			Model:   &opencode.ModelRef{ProviderID: "opencode-go", ID: "mimo-v2.6-pro"},
		},
		messages: []opencode.MessageInfo{
			{ID: "m2", Type: "assistant", Content: []opencode.MessagePart{{Type: "text", Text: "Done refactoring."}}, Time: struct {
				Created int64 `json:"created,omitempty"`
			}{Created: 200}},
			{ID: "m1", Type: "user", Text: "Please refactor", Time: struct {
				Created int64 `json:"created,omitempty"`
			}{Created: 100}},
		},
	}
	eng := NewOpenCodeEngineWithClient(mock, OpenCodeEngineOptions{})
	out, err := eng.Capture(context.Background(), "", "ses_cap", 0)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if !strings.Contains(out, "[session: ses_cap | status: succeeded | agent: build | model: opencode-go/mimo-v2.6-pro | title: Refactor]") {
		t.Fatalf("missing expected header in capture:\n%s", out)
	}
	userIdx := strings.Index(out, "[user]\nPlease refactor")
	asstIdx := strings.Index(out, "[assistant]\nDone refactoring.")
	if userIdx == -1 || asstIdx == -1 || userIdx > asstIdx {
		t.Fatalf("expected chronological user -> assistant ordering in capture:\n%s", out)
	}
}
