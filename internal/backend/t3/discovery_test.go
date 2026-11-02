package t3

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestServerDiscovery_DiscoverRuntime(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, home string) string // returns explicit statePath or empty
		expectErr     bool
		errCheck      func(error) bool
		expectedPort  int
		expectedProto string
	}{
		{
			name: "primary path ~/.t3/server-runtime.json",
			setup: func(t *testing.T, home string) string {
				dir := filepath.Join(home, ".t3")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				content := `{"version":1,"pid":1234,"host":"127.0.0.1","port":3773,"origin":"http://127.0.0.1:3773","startedAt":"2026-11-01T00:00:00Z","serverVersion":"0.1.6"}`
				if err := os.WriteFile(filepath.Join(dir, "server-runtime.json"), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return ""
			},
			expectErr:    false,
			expectedPort: 3773,
		},
		{
			name: "state-dir equivalent ~/.t3/userdata/server-runtime.json",
			setup: func(t *testing.T, home string) string {
				dir := filepath.Join(home, ".t3", "userdata")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				content := `{"version":1,"pid":5678,"host":"127.0.0.1","port":7265,"origin":"http://127.0.0.1:7265","startedAt":"2026-11-01T00:00:00Z","serverVersion":"0.1.6"}`
				if err := os.WriteFile(filepath.Join(dir, "server-runtime.json"), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return ""
			},
			expectErr:    false,
			expectedPort: 7265,
		},
		{
			name: "custom state path override",
			setup: func(t *testing.T, home string) string {
				dir := filepath.Join(home, "custom")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				customPath := filepath.Join(dir, "my-runtime.json")
				content := `{"version":1,"pid":9999,"host":"127.0.0.1","port":9000,"origin":"http://127.0.0.1:9000"}`
				if err := os.WriteFile(customPath, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return customPath
			},
			expectErr:    false,
			expectedPort: 9000,
		},
		{
			name: "server not running returns ErrServerNotRunning",
			setup: func(t *testing.T, home string) string {
				// Empty home, no files
				return ""
			},
			expectErr: true,
			errCheck: func(err error) bool {
				return errors.Is(err, ErrServerNotRunning)
			},
		},
		{
			name: "corrupt json in state file",
			setup: func(t *testing.T, home string) string {
				dir := filepath.Join(home, ".t3")
				_ = os.MkdirAll(dir, 0755)
				_ = os.WriteFile(filepath.Join(dir, "server-runtime.json"), []byte("{not-valid-json"), 0644)
				return ""
			},
			expectErr: true,
			errCheck: func(err error) bool {
				return err != nil && !errors.Is(err, ErrServerNotRunning)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempHome := t.TempDir()
			explicitPath := tt.setup(t, tempHome)

			opts := []DiscoveryOption{WithHomeDir(tempHome)}
			if explicitPath != "" {
				opts = append(opts, WithStatePath(explicitPath))
			}

			d := NewServerDiscovery(opts...)
			runtime, err := d.DiscoverRuntime()

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errCheck != nil && !tt.errCheck(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runtime.Port != tt.expectedPort {
				t.Errorf("expected port %d, got %d", tt.expectedPort, runtime.Port)
			}
			if runtime.Origin == "" {
				t.Errorf("expected origin to be populated")
			}
		})
	}
}

func TestFindT3Binary(t *testing.T) {
	tempDir := t.TempDir()

	// Mock lookpath that does not find t3
	lookPathNotFound := func(name string) (string, error) {
		return "", os.ErrNotExist
	}

	// Create fake ~/.local/bin/t3
	localBinDir := filepath.Join(tempDir, ".local", "bin")
	if err := os.MkdirAll(localBinDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeT3 := filepath.Join(localBinDir, "t3")
	if err := os.WriteFile(fakeT3, []byte("#!/bin/sh\necho t3\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Found in custom home ~/.local/bin/t3 when not in PATH
	path, err := FindT3BinaryWithLookup("", lookPathNotFound, tempDir)
	if err != nil {
		t.Fatalf("expected to find fake t3 in ~/.local/bin, got: %v", err)
	}
	if path != fakeT3 {
		t.Errorf("expected %s, got %s", fakeT3, path)
	}

	// 2. Found in PATH when lookPath succeeds
	lookPathFound := func(name string) (string, error) {
		return "/usr/bin/t3", nil
	}
	path, err = FindT3BinaryWithLookup("", lookPathFound, tempDir)
	if err != nil {
		t.Fatalf("expected to find t3 in PATH, got: %v", err)
	}
	if path != "/usr/bin/t3" {
		t.Errorf("expected /usr/bin/t3, got %s", path)
	}

	// 3. Explicit configured bin path takes precedence
	customBin := filepath.Join(tempDir, "custom-t3")
	if err := os.WriteFile(customBin, []byte("#!/bin/sh\necho custom\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path, err = FindT3BinaryWithLookup(customBin, lookPathFound, tempDir)
	if err != nil {
		t.Fatalf("expected to find custom bin, got: %v", err)
	}
	if path != customBin {
		t.Errorf("expected %s, got %s", customBin, path)
	}

	// 4. Not found in PATH or ~/.local/bin returns error mentioning ~/.local/bin/t3
	emptyHome := t.TempDir()
	_, err = FindT3BinaryWithLookup("", lookPathNotFound, emptyHome)
	if err == nil {
		t.Fatalf("expected error when t3 is missing, got nil")
	}
}
