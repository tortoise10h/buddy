package t3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClient_AuthAndProtocolHeaders(t *testing.T) {
	expectedToken := "test-session-bearer-token"
	expectedProto := "2"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer "+expectedToken {
			t.Errorf("expected Authorization 'Bearer %s', got '%s'", expectedToken, authHeader)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Check orchestration protocol header
		switch r.URL.Path {
		case "/api/orchestration/shell", "/api/orchestration/threads/th_123", "/api/orchestration/threads/th_123/transcript":
			protoHeader := r.Header.Get(OrchestrationProtocolHeader)
			if protoHeader != expectedProto {
				t.Errorf("expected %s header '%s', got '%s' on %s",
					OrchestrationProtocolHeader, expectedProto, protoHeader, r.URL.Path)
			}
		case "/api/projects":
			// Projects endpoint does not require orchestration protocol header
			if r.Header.Get(OrchestrationProtocolHeader) != "" {
				t.Errorf("did not expect %s header on /api/projects", OrchestrationProtocolHeader)
			}
		}

		switch r.URL.Path {
		case "/api/projects":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"projects":[{"id":"proj_1","title":"my-app","workspaceRoot":"/path/to/app"}],"updatedAt":"2026-11-01T00:00:00Z"}`))

		case "/api/orchestration/shell":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"schemaVersion":1,"snapshotSequence":42,"threads":[{"id":"th_123","projectId":"proj_1","title":"task-a","providerInstanceId":"antigravity","status":"running","pendingRequestCount":0}]}`))

		case "/api/orchestration/threads/th_123":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"snapshotSequence":42,"projection":{"thread":{"id":"th_123","projectId":"proj_1","title":"task-a","providerInstanceId":"antigravity","status":"running"},"updatedAt":"2026-11-01T00:00:00Z"}}`))

		case "/api/orchestration/threads/th_123/transcript":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"threadId":"th_123","title":"task-a","updatedAt":"2026-11-01T00:00:00Z","items":[{"position":1,"visibility":"local","sourceThreadId":"th_123","sourceItemId":"item_1","item":{"type":"assistant_message","text":"Working on it."}}]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenProvider := NewSessionTokenProvider(WithStaticSessionToken(expectedToken))
	client := NewHTTPClient(
		WithBaseURL(server.URL),
		WithHTTPTokenProvider(tokenProvider),
		WithProtocolVersion(2),
	)

	ctx := context.Background()

	// 1. GetProjects
	projSnap, err := client.GetProjects(ctx)
	if err != nil {
		t.Fatalf("GetProjects failed: %v", err)
	}
	if len(projSnap.Projects) != 1 || projSnap.Projects[0].ID != "proj_1" {
		t.Errorf("unexpected projects response: %+v", projSnap)
	}

	// 2. GetShellSnapshot
	shellSnap, err := client.GetShellSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetShellSnapshot failed: %v", err)
	}
	if len(shellSnap.Threads) != 1 || shellSnap.Threads[0].ID != "th_123" {
		t.Errorf("unexpected shell snapshot: %+v", shellSnap)
	}

	// 3. GetThreadSnapshot
	threadSnap, err := client.GetThreadSnapshot(ctx, "th_123")
	if err != nil {
		t.Fatalf("GetThreadSnapshot failed: %v", err)
	}
	if threadSnap.Projection.Thread.ID != "th_123" {
		t.Errorf("unexpected thread detail: %+v", threadSnap)
	}

	// 4. GetThreadTranscript
	transcript, err := client.GetThreadTranscript(ctx, "th_123")
	if err != nil {
		t.Fatalf("GetThreadTranscript failed: %v", err)
	}
	if len(transcript.Items) != 1 || transcript.Items[0].Item.Text != "Working on it." {
		t.Errorf("unexpected transcript: %+v", transcript)
	}
}

func TestHTTPClient_DiscoverProtocolVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/t3/environment" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"environmentId":"env_1","label":"Dev","serverVersion":"0.1.6","orchestrationProtocolVersion":3}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHTTPClient(WithBaseURL(server.URL))
	desc, err := client.DiscoverProtocolVersion(context.Background())
	if err != nil {
		t.Fatalf("DiscoverProtocolVersion failed: %v", err)
	}
	if desc.OrchestrationProtocolVersion != 3 {
		t.Errorf("expected protocol version 3, got %d", desc.OrchestrationProtocolVersion)
	}
	if client.ProtocolVersion() != 3 {
		t.Errorf("expected client protocol version to update to 3, got %d", client.ProtocolVersion())
	}
}

func TestHTTPClient_ErrorPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/unauth":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"auth_invalid","reason":"missing_credential"}`))
		case "/api/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/api/servererror":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("crash"))
		case "/api/badjson":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid-json"))
		}
	}))
	defer server.Close()

	tokenProvider := NewSessionTokenProvider(WithStaticSessionToken("token"))
	client := NewHTTPClient(
		WithBaseURL(server.URL),
		WithHTTPTokenProvider(tokenProvider),
	)

	ctx := context.Background()

	// 1. 401 returns ErrAuthInvalid and invalidates token
	var target map[string]any
	err := client.doGet(ctx, "/api/unauth", false, &target)
	if !errors.Is(err, ErrAuthInvalid) {
		t.Fatalf("expected ErrAuthInvalid on 401, got: %v", err)
	}

	// 2. 404 returns ErrThreadNotFound
	err = client.doGet(ctx, "/api/notfound", false, &target)
	if !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected ErrThreadNotFound on 404, got: %v", err)
	}

	// 3. 500 returns error
	err = client.doGet(ctx, "/api/servererror", false, &target)
	if err == nil {
		t.Fatalf("expected error on 500, got nil")
	}

	// 4. Bad JSON
	err = client.doGet(ctx, "/api/badjson", false, &target)
	if err == nil {
		t.Fatalf("expected decode error on bad JSON, got nil")
	}
}
