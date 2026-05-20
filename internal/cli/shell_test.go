package cli

import (
	"strings"
	"testing"
)

func TestShellDryRun(t *testing.T) {
	dir := buildSyntheticCapsule(t)
	out, _, err := runCLI("shell", dir, "--dry-run", "--runtime", "echo")
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	// Shell command emits no stdout when dry-run + runtime is `echo`. We
	// just assert it didn't error.
	if strings.Contains(out, "FATAL") {
		t.Fatalf("unexpected error output: %s", out)
	}
}
