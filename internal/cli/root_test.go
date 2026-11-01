package cli

import (
	"bytes"
	"testing"
)

func TestRootCommand(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected no error executing version, got: %v", err)
	}

	output := buf.String()
	if len(output) == 0 {
		t.Fatalf("expected version output, got empty string")
	}
}
