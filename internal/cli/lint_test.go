package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintCmd_File(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(wf, []byte("on: pull_request_target\njobs:\n  x:\n    runs-on: self-hosted\n    steps:\n      - uses: actions/checkout@v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCLI("lint", wf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "permissions/missing") {
		t.Fatalf("expected permissions/missing finding:\n%s", out)
	}
}

func TestLintCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	wf := filepath.Join(dir, "ci.yml")
	_ = os.WriteFile(wf, []byte("on: push\njobs: {}\n"), 0o644)
	out, _, err := runCLI("lint", wf, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("expected JSON array, got: %s", out)
	}
}

func TestLintCmd_CapsuleDir(t *testing.T) {
	dir := buildSyntheticCapsule(t)
	wfDir := filepath.Join(dir, "workflow")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "ci.yml"), []byte("on: push\njobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCLI("lint", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "permissions/missing") {
		t.Fatalf("expected lint to find capsule workflow:\n%s", out)
	}
}
