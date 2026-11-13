package t3

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func createFakeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	content := []byte("#!/bin/sh\necho 't3 mock'\n")
	if err := os.WriteFile(path, content, 0755); err != nil {
		t.Fatalf("failed to write fake executable: %v", err)
	}
}

func TestFindT3Binary_EnvOverride(t *testing.T) {
	customBin := "/custom/opt/bin/t3"
	t.Setenv("BUDDY_T3_BIN", customBin)

	bin, err := FindT3Binary()
	if err != nil {
		t.Fatalf("expected binary found via BUDDY_T3_BIN, got error: %v", err)
	}
	if bin != customBin {
		t.Errorf("expected %s, got %s", customBin, bin)
	}
}

func TestFindT3Binary_T3CodeCliPath(t *testing.T) {
	t.Setenv("BUDDY_T3_BIN", "")

	fakeBinDir := t.TempDir()
	fakeT3 := filepath.Join(fakeBinDir, "t3")
	createFakeExecutable(t, fakeT3)

	t.Setenv("T3CODE_CLI_PATH", fakeT3)

	bin, err := FindT3Binary()
	if err != nil {
		t.Fatalf("expected binary found via T3CODE_CLI_PATH, got error: %v", err)
	}
	if bin != fakeT3 {
		t.Errorf("expected %s, got %s", fakeT3, bin)
	}
}

func TestFindT3Binary_PathLookup(t *testing.T) {
	t.Setenv("BUDDY_T3_BIN", "")
	t.Setenv("T3CODE_CLI_PATH", "")

	fakeBinDir := t.TempDir()
	fakeT3 := filepath.Join(fakeBinDir, "t3")
	createFakeExecutable(t, fakeT3)

	t.Setenv("PATH", fakeBinDir)

	bin, err := FindT3Binary()
	if err != nil {
		t.Fatalf("expected binary found in PATH, got error: %v", err)
	}
	if bin != fakeT3 {
		t.Errorf("expected %s, got %s", fakeT3, bin)
	}
}

func TestFindT3Binary_LocalBinFallback(t *testing.T) {
	t.Setenv("BUDDY_T3_BIN", "")
	t.Setenv("T3CODE_CLI_PATH", "")
	t.Setenv("PATH", "") // empty PATH so LookPath fails

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	fakeLocalBin := filepath.Join(fakeHome, ".local", "bin", "t3")
	createFakeExecutable(t, fakeLocalBin)

	bin, err := FindT3Binary()
	if err != nil {
		t.Fatalf("expected binary found in ~/.local/bin/t3, got error: %v", err)
	}
	if bin != fakeLocalBin {
		t.Errorf("expected %s, got %s", fakeLocalBin, bin)
	}
}

func TestFindT3Binary_UsrLocalBinFallback(t *testing.T) {
	t.Setenv("BUDDY_T3_BIN", "")
	t.Setenv("T3CODE_CLI_PATH", "")
	t.Setenv("PATH", "")

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	fakeUsrLocal := filepath.Join(t.TempDir(), "usr-local-t3")
	createFakeExecutable(t, fakeUsrLocal)

	oldPath := usrLocalBinT3
	usrLocalBinT3 = fakeUsrLocal
	t.Cleanup(func() {
		usrLocalBinT3 = oldPath
	})

	bin, err := FindT3Binary()
	if err != nil {
		t.Fatalf("expected binary found in /usr/local/bin/t3 candidate, got error: %v", err)
	}
	if bin != fakeUsrLocal {
		t.Errorf("expected %s, got %s", fakeUsrLocal, bin)
	}
}

func TestFindT3Binary_NotFound(t *testing.T) {
	t.Setenv("BUDDY_T3_BIN", "")
	t.Setenv("T3CODE_CLI_PATH", "")
	t.Setenv("PATH", "")

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	oldPath := usrLocalBinT3
	usrLocalBinT3 = filepath.Join(t.TempDir(), "nonexistent-t3")
	t.Cleanup(func() {
		usrLocalBinT3 = oldPath
	})

	bin, err := FindT3Binary()
	if err == nil {
		t.Fatalf("expected error when t3 is not found, got bin: %s", bin)
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("expected ErrBinaryNotFound, got: %v", err)
	}
}
