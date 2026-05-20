package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
)

// buildSyntheticCapsule creates a fully formed extracted capsule directory
// (capsule.json + redacted log) and returns its path. Used by E2E CLI tests.
func buildSyntheticCapsule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := "logs/build/job.log.redacted"
	logBody := []byte(strings.Join([]string{
		"##[group]Run pytest -q",
		"+ pytest -q",
		"FAILED tests/test_arith.py::test_add_is_correct - assert 4 == 5",
		"##[error]Process completed with exit code 1.",
	}, "\n"))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, logPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, logPath), logBody, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(logBody)
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, CreatedAt: time.Now().UTC(),
		Generator: "test", Provider: capsule.ProviderGitHub,
		Repo: "octocat/hello", Commit: "abc123", Workflow: "ci.yml", Job: "build",
		Runner: capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "pytest -q",
			ExitCode: 1, Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
		Replay: capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 17},
		Logs: []capsule.LogFile{{
			Path: logPath, Job: "build", Step: "Run tests",
			Size: int64(len(logBody)), SHA256: hex.EncodeToString(sum[:]),
		}},
	}
	manifest, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, capsule.CapsuleFileName), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCLI_DiagnoseReport(t *testing.T) {
	dir := buildSyntheticCapsule(t)

	// `reproforge diagnose <capsule-dir>`
	out, _, err := runCLI("diagnose", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "category:") || !strings.Contains(out, "fingerprint:") {
		t.Fatalf("diagnose output missing fields:\n%s", out)
	}

	// `reproforge report <capsule-dir>`
	out, _, err = runCLI("report", dir, "--format", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ReproForge CI Report") {
		t.Fatalf("report missing header:\n%s", out)
	}
}

func TestCLI_CapsuleInspect(t *testing.T) {
	dir := buildSyntheticCapsule(t)

	// pack into a tar.zst, then inspect via CLI
	out := filepath.Join(t.TempDir(), "rf.tar.zst")
	m, err := readManifestFile(filepath.Join(dir, capsule.CapsuleFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := capsule.PackFile(out, capsule.PackOptions{SourceDir: dir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runCLI("capsule", "inspect", out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reproforge.capsule/v1", "octocat/hello", "Run tests"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in:\n%s", want, stdout)
		}
	}
}

// ensure runCLI's stderr buffer is fresh for parallel use
func TestRunCLI_DistinctBuffers(t *testing.T) {
	cmd := NewRoot()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"schema"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
