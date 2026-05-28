package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(args ...string) (string, string, error) {
	cmd := NewRoot()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestRoot_Help(t *testing.T) {
	out, _, err := runCLI("--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reproforge") {
		t.Fatalf("missing usage: %s", out)
	}
}

func TestSchema(t *testing.T) {
	out, _, err := runCLI("schema")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reproforge.capsule/v1") {
		t.Fatalf("expected schema string, got %q", out)
	}
}

func TestInit_WritesConfig(t *testing.T) {
	tmp := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCLI("init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".reproforge/config.yaml") {
		t.Fatalf("expected path in output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".reproforge", "config.yaml")); err != nil {
		t.Fatalf("config not created: %v", err)
	}
	// running again without --force fails
	if _, _, err := runCLI("init"); err == nil {
		t.Fatal("expected error on second init")
	}
}

func TestSplitRepo(t *testing.T) {
	if r := splitRepo("o/r"); len(r) != 2 || r[0] != "o" || r[1] != "r" {
		t.Fatalf("got %v", r)
	}
	if r := splitRepo("bad"); r != nil {
		t.Fatalf("expected nil, got %v", r)
	}
}
