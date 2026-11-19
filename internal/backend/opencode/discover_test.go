package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverServer_EnvPrecedence(t *testing.T) {
	env := map[string]string{
		"BUDDY_OPENCODE_URL":      "http://127.0.0.1:4999/",
		"BUDDY_OPENCODE_PASSWORD": "secret",
	}
	ep, err := DiscoverServer(context.Background(), DiscoverOptions{
		ServerURL: "http://127.0.0.1:4096",
		GetEnv:    func(k string) string { return env[k] },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.URL != "http://127.0.0.1:4999" {
		t.Fatalf("expected env URL, got %q", ep.URL)
	}
	if ep.Username != "opencode" || ep.Password != "secret" {
		t.Fatalf("unexpected credentials: user=%q pass=%q", ep.Username, ep.Password)
	}
}

func TestDiscoverServer_ConfigOverride(t *testing.T) {
	ep, err := DiscoverServer(context.Background(), DiscoverOptions{
		ServerURL: "http://127.0.0.1:4096/",
		Password:  "cfgpass",
		GetEnv:    func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.URL != "http://127.0.0.1:4096" || ep.Password != "cfgpass" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestDiscoverServer_ReadsLiveServiceJSON(t *testing.T) {
	dir := t.TempDir()
	svcPath := filepath.Join(dir, "service.json")
	st := ServiceState{
		ID:       "svc-1",
		Version:  "2.3.0",
		URL:      "http://127.0.0.1:49374",
		PID:      12345,
		Password: "local-token",
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(svcPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	ep, err := DiscoverServer(context.Background(), DiscoverOptions{
		ServiceJSONPath: svcPath,
		GetEnv:          func(string) string { return "" },
		IsPIDAlive:      func(pid int) bool { return pid == 12345 },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.URL != "http://127.0.0.1:49374" || ep.Password != "local-token" {
		t.Fatalf("unexpected endpoint: %+v", ep)
	}
}

func TestDiscoverServer_DeadPIDTriggersAutoStart(t *testing.T) {
	dir := t.TempDir()
	svcPath := filepath.Join(dir, "service.json")

	// Write stale dead service.json first.
	stale, _ := json.Marshal(ServiceState{
		URL:      "http://127.0.0.1:40001",
		PID:      99999,
		Password: "old",
	})
	if err := os.WriteFile(svcPath, stale, 0600); err != nil {
		t.Fatal(err)
	}

	started := false
	ep, err := DiscoverServer(context.Background(), DiscoverOptions{
		ServiceJSONPath:  svcPath,
		AutoStartService: true,
		StartTimeout:     2 * time.Second,
		PollInterval:     10 * time.Millisecond,
		GetEnv:           func(string) string { return "" },
		IsPIDAlive:       func(pid int) bool { return pid == 54321 },
		StartServiceFn: func(ctx context.Context) error {
			started = true
			fresh, _ := json.Marshal(ServiceState{
				URL:      "http://127.0.0.1:49500",
				PID:      54321,
				Password: "new-token",
			})
			return os.WriteFile(svcPath, fresh, 0600)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatalf("expected StartServiceFn to be invoked")
	}
	if ep.URL != "http://127.0.0.1:49500" || ep.Password != "new-token" {
		t.Fatalf("unexpected endpoint after auto-start: %+v", ep)
	}
}

func TestDiscoverServer_NotRunningNoAutoStart(t *testing.T) {
	svcPath := filepath.Join(t.TempDir(), "missing-service.json")
	_, err := DiscoverServer(context.Background(), DiscoverOptions{
		ServiceJSONPath:  svcPath,
		AutoStartService: false,
		GetEnv:           func(string) string { return "" },
	})
	if !errors.Is(err, ErrServiceNotRunning) {
		t.Fatalf("expected ErrServiceNotRunning, got %v", err)
	}
}
