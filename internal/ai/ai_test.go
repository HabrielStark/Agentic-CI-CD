package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/diagnose"
)

func sampleCapsule() *capsule.Capsule {
	return &capsule.Capsule{
		Schema: capsule.SchemaV1, CreatedAt: time.Now(),
		Provider: capsule.ProviderGitHub, Repo: "o/r", Commit: "abc",
		Workflow: "ci.yml", Job: "build",
		Runner:    capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Replay:    capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
		Failure: capsule.Failure{
			Step: "tests", Command: "GITHUB_TOKEN=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA pytest",
			ExitCode: 1, Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
	}
}

func TestSanitisedPrompt_RedactsSecrets(t *testing.T) {
	c := sampleCapsule()
	d := diagnose.Diagnosis{
		Category: "network_issue", Command: c.Failure.Command,
		Evidence: []string{"failed with token=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA on host"},
	}
	p := SanitisedPrompt(c, d, []Snippet{{Source: "log", Body: "token ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}})
	for _, s := range []string{p.FailedCmd, p.Diagnosis.Command, p.Diagnosis.Evidence[0], p.Snippets[0].Body} {
		if strings.Contains(s, "ghp_") {
			t.Fatalf("secret leaked: %s", s)
		}
	}
}

func TestNewAdapter(t *testing.T) {
	if _, err := NewAdapter(ProviderClaude); err == nil {
		t.Fatal("expected error without ANTHROPIC_API_KEY")
	}
	a, err := NewAdapter(ProviderLocal)
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.SuggestPatch(context.Background(), PromptInput{})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("nil patch")
	}
	if _, err := NewAdapter("bogus"); err == nil {
		t.Fatal("expected unknown provider error")
	}
	dis, _ := NewAdapter(ProviderNone)
	if _, err := dis.SuggestPatch(context.Background(), PromptInput{}); err == nil {
		t.Fatal("disabled adapter should error")
	}
}

func TestParsePatchResponse(t *testing.T) {
	in := "PLAN: do x\n```diff\ndiff --git a/x b/x\n+++ b/x\n+hello\n```\n"
	p := parsePatchResponse(in)
	if !strings.Contains(p.Plan, "PLAN") || !strings.Contains(p.Diff, "+hello") {
		t.Fatalf("parse wrong: %+v", p)
	}
	if len(p.Files) != 1 || p.Files[0] != "x" {
		t.Fatalf("files wrong: %+v", p.Files)
	}
}
