package workflow

import (
	"strings"
	"testing"
)

const sampleWorkflow = `
name: CI
on: [push, pull_request]
env:
  CI: "true"
  GO_VERSION: "1.25"
jobs:
  test:
    runs-on: ubuntu-24.04
    timeout-minutes: 30
    env:
      GOFLAGS: "-mod=mod"
    steps:
      - uses: actions/checkout@v4
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      - name: Run tests
        run: |
          go test -race -count=1 ./...
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - name: Build
        run: go build -o bin/app ./cmd/app
        shell: bash
  lint:
    runs-on: [self-hosted, linux]
    steps:
      - uses: actions/checkout@v4
      - name: Lint
        run: golangci-lint run ./...
`

func TestParse(t *testing.T) {
	w, err := Parse([]byte(sampleWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "CI" {
		t.Fatalf("name: %q", w.Name)
	}
	if len(w.Jobs) != 2 {
		t.Fatalf("jobs: %d", len(w.Jobs))
	}
	testJob := w.Jobs["test"]
	if testJob.TimeoutMin != 30 {
		t.Fatalf("timeout: %d", testJob.TimeoutMin)
	}
	if len(testJob.Steps) != 4 {
		t.Fatalf("steps: %d", len(testJob.Steps))
	}
}

func TestFindStep(t *testing.T) {
	w, _ := Parse([]byte(sampleWorkflow))

	jn, s, ok := w.FindStep("Run tests")
	if !ok || jn != "test" || s == nil {
		t.Fatalf("FindStep failed: jn=%s ok=%v", jn, ok)
	}
	cmd := s.ExtractCommand()
	if !strings.Contains(cmd, "go test -race") {
		t.Fatalf("command wrong: %q", cmd)
	}

	// Case insensitive
	_, s2, ok := w.FindStep("run tests")
	if !ok || s2 == nil {
		t.Fatal("case insensitive lookup failed")
	}

	// Not found
	_, _, ok = w.FindStep("nonexistent step")
	if ok {
		t.Fatal("should not find nonexistent")
	}

	// By number
	s3 := w.FindStepByNumber("test", 3)
	if s3 == nil || s3.Name != "Run tests" {
		t.Fatalf("by number: %+v", s3)
	}
	if s3 := w.FindStepByNumber("test", 99); s3 != nil {
		t.Fatal("should be nil for out of range")
	}
}

func TestExtractEnv(t *testing.T) {
	w, _ := Parse([]byte(sampleWorkflow))
	_, step, _ := w.FindStep("Run tests")
	env := w.ExtractEnv("test", step)
	// workflow level
	if env["CI"] != "true" {
		t.Fatalf("missing CI env")
	}
	// job level
	if env["GOFLAGS"] != "-mod=mod" {
		t.Fatalf("missing GOFLAGS")
	}
	// step level (contains expression, but it's there)
	if _, ok := env["GITHUB_TOKEN"]; !ok {
		t.Fatal("missing step-level GITHUB_TOKEN")
	}
}

func TestRunnerLabels(t *testing.T) {
	w, _ := Parse([]byte(sampleWorkflow))
	labels := w.Jobs["lint"].RunnerLabels()
	if len(labels) != 2 || labels[0] != "self-hosted" {
		t.Fatalf("labels: %v", labels)
	}
	labels2 := w.Jobs["test"].RunnerLabels()
	if len(labels2) != 1 || labels2[0] != "ubuntu-24.04" {
		t.Fatalf("labels2: %v", labels2)
	}
}

func TestAllRunCommands(t *testing.T) {
	w, _ := Parse([]byte(sampleWorkflow))
	cmds := w.AllRunCommands()
	if len(cmds) != 3 {
		t.Fatalf("expected 3 run commands, got %d", len(cmds))
	}
}

func TestParse_Invalid(t *testing.T) {
	if _, err := Parse([]byte("not: [valid: yaml")); err == nil {
		t.Fatal("expected error")
	}
}

func TestFindStep_Nil(t *testing.T) {
	var w *Workflow
	if _, _, ok := w.FindStep("x"); ok {
		t.Fatal("nil workflow should return false")
	}
}
