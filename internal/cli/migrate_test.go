package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCmd_All(t *testing.T) {
	cap := buildSyntheticCapsule(t)
	out := t.TempDir()
	if _, _, err := runCLI("migrate", cap, "--out", out, "--format", "all"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"migrate.sh", "Earthfile"} {
		body, err := os.ReadFile(filepath.Join(out, p))
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if !strings.Contains(string(body), "octocat/hello") {
			t.Fatalf("%s missing repo: %s", p, body)
		}
	}
}

func TestMigrateCmd_BadFormat(t *testing.T) {
	cap := buildSyntheticCapsule(t)
	if _, _, err := runCLI("migrate", cap, "--out", t.TempDir(), "--format", "weird"); err == nil {
		t.Fatal("expected error")
	}
}
