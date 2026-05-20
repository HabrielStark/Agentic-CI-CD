package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/diagnose"
)

func sample() Bundle {
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, CreatedAt: time.Now(),
		Provider: capsule.ProviderGitHub, Repo: "octocat/hello",
		Commit: "deadbeefcafe", Workflow: "ci.yml", Job: "build",
		Runner:    capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Replay:    capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 17, Hits: 3},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "pytest -q", ExitCode: 1,
			Fingerprint: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			Tests:       []string{"tests/test_api.py::test_login"},
		},
	}
	d := diagnose.Diagnosis{
		Category: diagnose.CatNetwork, Confidence: 0.85,
		Evidence: []string{"connection refused"},
		NextActions: []string{"replay with --network=deny", "pin lockfile"},
		Commands: []string{"reproforge replay rf.tar.zst --mode failed-step --network=deny"},
		Tests: []string{"test_login"},
		ExitCode: 1, Step: "Run tests",
	}
	return Bundle{Capsule: c, Diagnosis: d}
}

func TestMarkdown(t *testing.T) {
	out := Markdown(sample())
	for _, want := range []string{
		"ReproForge CI Report",
		"octocat/hello",
		"network", // category appears (humanised)
		"Network",
		"connection refused",
		"replay with --network=deny",
		"```bash",
		"```text",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestJSON(t *testing.T) {
	b, err := JSON(sample())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["capsule"]; !ok {
		t.Fatal("missing capsule")
	}
	if _, ok := m["diagnosis"]; !ok {
		t.Fatal("missing diagnosis")
	}
}

func TestSARIF(t *testing.T) {
	b, err := SARIF(sample())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != "2.1.0" {
		t.Fatalf("not sarif 2.1.0: %v", m["version"])
	}
	if !strings.Contains(string(b), "reproforge/network_issue") {
		t.Fatalf("rule id missing: %s", string(b))
	}
}

func TestIssueTemplate(t *testing.T) {
	out := IssueTemplate(sample())
	if !strings.Contains(out, "ReproForge:") {
		t.Fatal("missing header")
	}
}

func TestMarkdownWithReplay(t *testing.T) {
	b := sample()
	b.Replay = &ReplayOutcome{Mode: "failed-step", Network: "allow", ExitCode: 1, OriginalExit: 1, Reproduced: true, DurationMs: 1234}
	b.FlakeStats = &FlakeStats{Target: "t", Runs: 10, Passed: 4, Failed: 6, Mode: "failed-step"}
	out := Markdown(b)
	if !strings.Contains(out, "reproduced") || !strings.Contains(out, "flaky") {
		t.Fatalf("expected replay+flake sections:\n%s", out)
	}
}
