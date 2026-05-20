package fingerprint

import (
	"strings"
	"testing"
)

func TestComputeStable(t *testing.T) {
	in := Input{
		Command: "pytest -q tests/", ExitCode: 1, Step: "Run tests",
		FailingTests: []string{"test_login"},
		ErrorClass: "AssertionError",
		StackTopFrame: "/runner/_work/repo/repo/tests/test_api.py:42 in test_login",
		DepPackages: []string{"requests==2.31.0"},
		Provider: "github_actions", OS: "Ubuntu-24.04",
	}
	f1 := Compute(in)
	f2 := Compute(in)
	if f1 != f2 {
		t.Fatal("fingerprint not stable")
	}
	if !strings.HasPrefix(f1, "sha256:") {
		t.Fatalf("missing prefix: %s", f1)
	}
}

func TestNormalisesRunnerPaths(t *testing.T) {
	a := Input{
		Command: "pytest", ExitCode: 1, Step: "Run tests",
		StackTopFrame: "/home/runner/work/repo/repo/tests/test_api.py:42 in test_login",
	}
	b := a
	b.StackTopFrame = "/runner/_work/repo/repo/tests/test_api.py:42 in test_login"
	if Compute(a) != Compute(b) {
		t.Fatalf("expected runner paths to normalise to same fingerprint:\n  %s\n  %s", Compute(a), Compute(b))
	}
}

func TestNormalisesNumbers(t *testing.T) {
	a := Input{Command: "pytest --port 12345", Step: "Run tests"}
	b := Input{Command: "pytest --port 65432", Step: "Run tests"}
	if Compute(a) != Compute(b) {
		t.Fatal("numbers should normalise")
	}
}

func TestDifferentFailingTestsProduceDifferentFingerprints(t *testing.T) {
	a := Input{Command: "pytest", Step: "tests", FailingTests: []string{"a"}}
	b := Input{Command: "pytest", Step: "tests", FailingTests: []string{"b"}}
	if Compute(a) == Compute(b) {
		t.Fatal("fingerprints should differ")
	}
}

func TestDepListNormalised(t *testing.T) {
	a := Input{DepPackages: []string{"react", "lodash"}}
	b := Input{DepPackages: []string{"LODASH", "react"}}
	if Compute(a) != Compute(b) {
		t.Fatal("dep list ordering/case should normalise")
	}
}
