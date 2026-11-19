package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindOpenCodeBinary_EnvOverride(t *testing.T) {
	t.Setenv("BUDDY_OPENCODE_BIN", "/custom/bin/opencode")
	got, err := FindOpenCodeBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/custom/bin/opencode" {
		t.Fatalf("expected /custom/bin/opencode, got %q", got)
	}
}

func TestFindOpenCodeBinary_DesktopBundledFallback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BUDDY_OPENCODE_BIN", "")

	origBrew, origUsr := homebrewBinOpenCode, usrLocalBinOpenCode
	homebrewBinOpenCode = filepath.Join(tmpHome, "nonexistent-brew")
	usrLocalBinOpenCode = filepath.Join(tmpHome, "nonexistent-usr")
	t.Cleanup(func() {
		homebrewBinOpenCode = origBrew
		usrLocalBinOpenCode = origUsr
	})

	cliDir1 := filepath.Join(tmpHome, "Library", "Application Support", "ai.opencode.desktop", "cli", "2.2.0-arm64")
	cliDir2 := filepath.Join(tmpHome, "Library", "Application Support", "ai.opencode.desktop", "cli", "2.3.0-arm64")
	if err := os.MkdirAll(cliDir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cliDir2, 0755); err != nil {
		t.Fatal(err)
	}
	bin1 := filepath.Join(cliDir1, "opencode-cli")
	bin2 := filepath.Join(cliDir2, "opencode-cli")
	if err := os.WriteFile(bin1, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin2, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := FindOpenCodeBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bin2 {
		t.Fatalf("expected newest bundled CLI %q, got %q", bin2, got)
	}
}

func TestFindOpenCodeBinary_NotFound(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("BUDDY_OPENCODE_BIN", "")

	origBrew, origUsr := homebrewBinOpenCode, usrLocalBinOpenCode
	homebrewBinOpenCode = filepath.Join(tmpHome, "nonexistent-brew")
	usrLocalBinOpenCode = filepath.Join(tmpHome, "nonexistent-usr")
	t.Cleanup(func() {
		homebrewBinOpenCode = origBrew
		usrLocalBinOpenCode = origUsr
	})

	_, err := FindOpenCodeBinary()
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("expected ErrBinaryNotFound, got %v", err)
	}
}
