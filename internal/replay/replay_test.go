package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/capsule"
)

func sampleCapsule() *capsule.Capsule {
	return &capsule.Capsule{
		Schema:   capsule.SchemaV1,
		Provider: capsule.ProviderGitHub,
		Repo:     "o/r", Commit: "abc", Workflow: "ci.yml", Job: "build",
		Runner:  capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "pytest -q tests/",
			ExitCode: 1, Fingerprint: "sha256:" + strings.Repeat("a", 64),
			Tests: []string{"tests/test_api.py::test_login"},
		},
		Replay: capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
	}
}

func TestGenerate_FilesWritten(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil)
	c := sampleCapsule()
	if err := e.Generate(c, dir, Options{Mode: ModeFailedStep, Network: NetworkAllow}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"replay/Dockerfile", "replay/replay.sh", "replay/failed-step.sh"} {
		full := filepath.Join(dir, p)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if info.Size() < 10 {
			t.Fatalf("file %s too small: %d", p, info.Size())
		}
	}
	body, _ := os.ReadFile(filepath.Join(dir, "replay/Dockerfile"))
	if !strings.Contains(string(body), "FROM ubuntu:24.04") {
		t.Fatalf("base image wrong: %s", body)
	}
	body, _ = os.ReadFile(filepath.Join(dir, "replay/replay.sh"))
	if !strings.Contains(string(body), "REPROFORGE_MODE") {
		t.Fatalf("replay.sh wrong: %s", body)
	}
	body, _ = os.ReadFile(filepath.Join(dir, "replay/failed-step.sh"))
	if !strings.Contains(string(body), "pytest -q") {
		t.Fatalf("failed-step.sh wrong: %s", body)
	}
}

func TestAutoImage(t *testing.T) {
	cases := map[string]string{
		"ubuntu-24.04": "ubuntu:24.04",
		"Ubuntu-22.04": "ubuntu:22.04",
		"alpine":       "alpine:3.20",
		"fedora-40":    "fedora:40",
		"":             "ubuntu:24.04",
	}
	for in, want := range cases {
		if got := AutoImage(capsule.Runner{OS: in}); got != want {
			t.Fatalf("AutoImage(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPickTestCommand(t *testing.T) {
	c := sampleCapsule()
	c.Failure.Command = "pytest tests/"
	if got := pickTestCommand(c); !strings.Contains(got, "pytest") {
		t.Fatalf("unexpected: %s", got)
	}
	c.Failure.Command = "go test ./..."
	c.Failure.Tests = []string{"TestX", "TestY"}
	got := pickTestCommand(c)
	if !strings.Contains(got, "go test") || !strings.Contains(got, "TestX|TestY") {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestShellEscape(t *testing.T) {
	cases := map[string]string{
		"":          "''",
		"safe":      "safe",
		"with sp":   "'with sp'",
		"a'b":       "'a'\\''b'",
	}
	for in, want := range cases {
		if got := shellEscape(in); got != want {
			t.Fatalf("shellEscape(%q)=%q want %q", in, got, want)
		}
	}
}
