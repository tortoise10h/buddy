package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPClient_CoreEndpointsAndBasicAuth(t *testing.T) {
	const expectedUser = "opencode"
	const expectedPass = "test-secret"

	var gotPromptText string
	var gotInterrupted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != expectedUser || p != expectedPass {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"_tag":"UnauthorizedError","message":"Authentication required"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/info":
			_, _ = w.Write([]byte(`{"version":"2.3.0","pid":90770,"capabilities":{"persistentPty":true}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/project":
			_, _ = w.Write([]byte(`[{"id":"proj_1","canonical":"/tmp/myproj","vcs":"git"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			var req CreateSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			_, _ = fmt.Fprintf(w, `{"data":{"id":"ses_new123","projectID":"proj_1","title":%q,"agent":%q,"location":{"directory":%q}}}`, req.Title, req.Agent, req.Location.Directory)
		case r.Method == http.MethodGet && r.URL.Path == "/api/session":
			_, _ = w.Write([]byte(`{"data":[{"id":"ses_new123","projectID":"proj_1","title":"feat-auth"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/active":
			_, _ = w.Write([]byte(`{"data":{"ses_new123":{"type":"running"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_new123":
			_, _ = w.Write([]byte(`{"data":{"id":"ses_new123","projectID":"proj_1","title":"feat-auth","agent":"build","outcome":"succeeded"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"_tag":"NotFoundError","message":"session not found"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_new123/prompt":
			var req PromptRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotPromptText = req.Text
			_, _ = w.Write([]byte(`{"data":{"id":"inbox_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/session/ses_new123/interrupt":
			gotInterrupted = true
			_, _ = w.Write([]byte(`{"interrupted":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_new123/message":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","type":"user","text":"hello"},{"id":"m2","type":"assistant","content":[{"type":"reasoning","text":"thinking"},{"type":"text","text":"done!"}]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_new123/permission":
			_, _ = w.Write([]byte(`{"data":[{"id":"perm_1","action":"bash","resource":"rm -rf"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/session/ses_new123/form":
			_, _ = w.Write([]byte(`{"data":[{"id":"form_1","title":"Pick target branch"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(WithEndpoint(EndpointConfig{
		URL:      srv.URL,
		Username: expectedUser,
		Password: expectedPass,
	}))

	ctx := context.Background()

	info, err := client.Info(ctx)
	if err != nil || info.Version != "2.3.0" || !info.Capabilities.PersistentPTY {
		t.Fatalf("Info failed: info=%+v err=%v", info, err)
	}

	projects, err := client.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].ID != "proj_1" {
		t.Fatalf("ListProjects failed: projects=%+v err=%v", projects, err)
	}

	sess, err := client.CreateSession(ctx, CreateSessionRequest{
		Title:    "feat-auth",
		Agent:    "build",
		Location: &SessionLocation{Directory: "/tmp/myproj"},
	})
	if err != nil || sess.ID != "ses_new123" || sess.ProjectID != "proj_1" {
		t.Fatalf("CreateSession failed: sess=%+v err=%v", sess, err)
	}

	list, err := client.ListSessions(ctx, ListSessionsOptions{Directory: "/tmp/myproj", Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSessions failed: list=%+v err=%v", list, err)
	}

	active, err := client.GetActiveSessions(ctx)
	if err != nil || active["ses_new123"].Type != "running" {
		t.Fatalf("GetActiveSessions failed: active=%+v err=%v", active, err)
	}

	if err := client.Prompt(ctx, "ses_new123", PromptRequest{Text: "implement auth"}); err != nil || gotPromptText != "implement auth" {
		t.Fatalf("Prompt failed: got=%q err=%v", gotPromptText, err)
	}

	if err := client.Interrupt(ctx, "ses_new123"); err != nil || !gotInterrupted {
		t.Fatalf("Interrupt failed: interrupted=%v err=%v", gotInterrupted, err)
	}

	msgs, err := client.ListMessages(ctx, "ses_new123", 10, "desc")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("ListMessages failed: msgs=%+v err=%v", msgs, err)
	}
	if msgs[0].ExtractText() != "hello" || msgs[1].ExtractText() != "done!" {
		t.Fatalf("unexpected message text extraction: %q / %q", msgs[0].ExtractText(), msgs[1].ExtractText())
	}

	perms, err := client.ListPermissions(ctx, "ses_new123")
	if err != nil || len(perms) != 1 || perms[0].Summary() == "" {
		t.Fatalf("ListPermissions failed: perms=%+v err=%v", perms, err)
	}

	forms, err := client.ListForms(ctx, "ses_new123")
	if err != nil || len(forms) != 1 || forms[0].Summary() != "Pick target branch" {
		t.Fatalf("ListForms failed: forms=%+v err=%v", forms, err)
	}

	_, err = client.GetSession(ctx, "ses_missing")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}

	// Verify 401 maps to ErrUnauthorized
	badClient := NewHTTPClient(WithEndpoint(EndpointConfig{
		URL:      srv.URL,
		Password: "wrong",
	}))
	_, err = badClient.Info(ctx)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestHTTPClient_RediscoversOnPasswordRotation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, p, ok := r.BasicAuth()
		if !ok || p != "fresh-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"_tag":"UnauthorizedError","message":"Authentication required"}`))
			return
		}
		_, _ = w.Write([]byte(`{"version":"2.3.0"}`))
	}))
	defer srv.Close()

	svcPath := filepath.Join(t.TempDir(), "service.json")
	writeState := func(pw string) {
		b, _ := json.Marshal(ServiceState{URL: srv.URL, PID: os.Getpid(), Password: pw})
		if err := os.WriteFile(svcPath, b, 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Initially stale password in service.json
	writeState("stale-pass")
	client := NewHTTPClient(WithDiscoverOptions(DiscoverOptions{
		ServiceJSONPath: svcPath,
		GetEnv:          func(string) string { return "" },
		IsPIDAlive:      func(int) bool { return true },
	}))

	// Prime cache with stale password
	if _, err := client.Info(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected initial 401, got %v", err)
	}

	// Rotate password in service.json; next call should auto-rediscover and succeed.
	writeState("fresh-pass")
	info, err := client.Info(context.Background())
	if err != nil || info.Version != "2.3.0" {
		t.Fatalf("expected rediscovery success, got info=%+v err=%v", info, err)
	}
}

func TestHTTPClient_SubscribeEventsSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/event" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, ": heartbeat\n\nevent: session.idle\ndata: {\"sessionID\":\"ses_1\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(WithEndpoint(EndpointConfig{URL: srv.URL}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := client.SubscribeEvents(ctx, []string{"session.idle"})
	if err != nil {
		t.Fatalf("SubscribeEvents failed: %v", err)
	}
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed before receiving event")
		}
		if ev.Event != "session.idle" || string(ev.Data) != `{"sessionID":"ses_1"}` {
			t.Fatalf("unexpected SSE event: %+v", ev)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for SSE event")
	}
}

func TestHTTPClient_UpdateSessionArchive(t *testing.T) {
	var bodies []string
	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		paths = append(paths, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/session/ses_missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"_tag":"NotFoundError","message":"session not found"}`))
			return
		}
		var req UpdateSessionRequest
		_ = json.Unmarshal(raw, &req)
		if req.Archived != nil && *req.Archived {
			_, _ = w.Write([]byte(`{"data":{"id":"ses_1","time":{"created":100,"updated":200,"archived":300}}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":{"id":"ses_1","time":{"created":100,"updated":250}}}`))
		}
	}))
	defer srv.Close()

	client := NewHTTPClient(WithEndpoint(EndpointConfig{URL: srv.URL}))
	ctx := context.Background()

	archived := true
	sess, err := client.UpdateSession(ctx, "ses_1", UpdateSessionRequest{Archived: &archived})
	if err != nil {
		t.Fatalf("UpdateSession(archived=true) failed: %v", err)
	}
	if sess.ID != "ses_1" || sess.Time.Archived != 300 {
		t.Fatalf("unexpected archived session response: %+v", sess)
	}

	unarchived := false
	sess2, err := client.UpdateSession(ctx, "ses_1", UpdateSessionRequest{Archived: &unarchived})
	if err != nil {
		t.Fatalf("UpdateSession(archived=false) failed: %v", err)
	}
	if sess2.ID != "ses_1" || sess2.Time.Archived != 0 {
		t.Fatalf("unexpected unarchived session response: %+v", sess2)
	}

	if len(bodies) != 2 || bodies[0] != `{"archived":true}` || bodies[1] != `{"archived":false}` {
		t.Fatalf("unexpected PATCH bodies: %+v", bodies)
	}
	if len(paths) != 2 || paths[0] != "/api/session/ses_1" || paths[1] != "/api/session/ses_1" {
		t.Fatalf("unexpected PATCH paths: %+v", paths)
	}

	_, err = client.UpdateSession(ctx, "ses_missing", UpdateSessionRequest{Archived: &archived})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
