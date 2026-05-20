package wflint

import (
	"strings"
	"testing"
)

const safeWorkflow = `name: CI
on:
  push:
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - run: go test ./...
`

const unsafeWorkflow = `name: CI
on:
  pull_request_target:
jobs:
  build:
    runs-on: self-hosted
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - name: leak
        env:
          TOK: ${{ secrets.GITHUB_TOKEN }}
        run: |
          echo $TOK
      - if: ${{ secrets.OPTIONAL != '' }}
        run: echo hi
`

func TestLint_Clean(t *testing.T) {
	got := Lint([]byte(safeWorkflow))
	if len(got) != 0 {
		t.Fatalf("expected clean, got: %+v", got)
	}
}

func TestLint_AllRulesFire(t *testing.T) {
	got := Lint([]byte(unsafeWorkflow))
	rules := map[string]bool{}
	for _, f := range got {
		rules[f.Rule] = true
	}
	for _, want := range []string{
		"permissions/missing",
		"pull_request_target/pr-head-checkout",
		"uses/floating-tag",
		"secret/echo-leak",
		"runner/self-hosted-no-labels",
		"if/secrets-comparison",
	} {
		if !rules[want] {
			t.Fatalf("missing rule %q in findings: %+v", want, got)
		}
	}
	// every finding must have a non-empty hint or message
	for _, f := range got {
		if f.Message == "" {
			t.Fatalf("finding without message: %+v", f)
		}
	}
}

func TestLint_Unparseable(t *testing.T) {
	got := Lint([]byte(":\n  - [oops"))
	if len(got) == 0 {
		t.Fatal("expected at least a yaml/parse finding")
	}
	if !strings.Contains(got[0].Rule, "yaml/parse") {
		t.Fatalf("expected yaml/parse, got %q", got[0].Rule)
	}
}
