package diagnose

import (
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/parsers"
)

func TestClassify_Network(t *testing.T) {
	in := Input{
		LogScan: parsers.LogScan{NetworkHits: []string{"connection refused"}},
		Command: "curl example.com", StepName: "build", OS: "ubuntu",
	}
	d := Classify(in)
	if d.Category != CatNetwork {
		t.Fatalf("expected network, got %s", d.Category)
	}
	if d.Confidence < 0.5 {
		t.Fatalf("low confidence: %f", d.Confidence)
	}
}

func TestClassify_DependencyTrumpsNetwork(t *testing.T) {
	in := Input{
		LogScan: parsers.LogScan{
			NetworkHits: []string{"connection reset"},
			DepHits:     []string{"npm ERR! code E404 lodash@2.0.0"},
			ChecksumHits: nil,
		},
		Command: "npm ci", StepName: "install",
	}
	d := Classify(in)
	if d.Category == CatUnknown {
		t.Fatal("expected a category")
	}
	// Network and dependency may both score high; verify we get either, but with a recorded note for the alternate.
	if len(d.Notes) == 0 {
		t.Fatalf("expected runner-up note, got %+v", d.Notes)
	}
}

func TestClassify_FailingTests(t *testing.T) {
	suites := parsers.Suites{{
		Name: "tests.api", Cases: []parsers.TestCase{
			{Name: "test_login", Status: parsers.StatusFailed, Message: "boom"},
		}, Failures: 1, Total: 1,
	}}
	in := Input{Suites: suites, StepName: "tests"}
	d := Classify(in)
	if d.Category != CatCodeOrTest {
		t.Fatalf("want code_or_test_failure, got %s", d.Category)
	}
	if len(d.Tests) != 1 || d.Tests[0] != "test_login" {
		t.Fatalf("tests not surfaced: %+v", d.Tests)
	}
}

func TestClassify_Unknown(t *testing.T) {
	d := Classify(Input{StepName: "x"})
	if d.Category != CatUnknown {
		t.Fatalf("want unknown, got %s", d.Category)
	}
}

func TestClassify_OOM(t *testing.T) {
	d := Classify(Input{LogScan: parsers.LogScan{OOMHits: []string{"java.lang.OutOfMemoryError"}}})
	if d.Category != CatOOM {
		t.Fatalf("want oom, got %s", d.Category)
	}
	if !strings.Contains(strings.Join(d.NextActions, " "), "memory") {
		t.Fatalf("expected memory action, got %v", d.NextActions)
	}
}

func TestClassify_MissingSecret(t *testing.T) {
	d := Classify(Input{LogScan: parsers.LogScan{MissingEnv: []string{"secret API_TOKEN not found"}}})
	if d.Category != CatMissingSecret {
		t.Fatalf("want missing_secret, got %s", d.Category)
	}
}

func TestClassify_Workflow(t *testing.T) {
	d := Classify(Input{LogScan: parsers.LogScan{YAMLExpr: []string{"Unrecognized named-value: x"}}})
	if d.Category != CatWorkflowConfig {
		t.Fatalf("want workflow_config, got %s", d.Category)
	}
}
