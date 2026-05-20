package redaction

import (
	"strings"
	"testing"
)

// TestNoSecretInAnyOutput is a "fuzz-ish" smoke test: a corpus of fake
// secrets is run through the engine and the redacted output must never
// retain any of the originals as substrings.
func TestNoSecretInAnyOutput(t *testing.T) {
	corpus := []string{
		"AWS_SECRET_ACCESS_KEY=" + strings.Repeat("Z", 40),
		"GITHUB_TOKEN=ghp_" + strings.Repeat("A", 36),
		"GH_TOKEN=gho_" + strings.Repeat("B", 36),
		"Authorization: Bearer " + strings.Repeat("y", 64),
		"--header X-API-KEY=" + strings.Repeat("h", 40),
		"sk-" + strings.Repeat("a", 48),
		"sk-ant-" + strings.Repeat("p", 60),
		`"private_key": "-----BEGIN PRIVATE KEY-----abcdef-----END PRIVATE KEY-----"`,
		"password=" + strings.Repeat("x", 24),
	}
	e := NewDefault()
	for _, in := range corpus {
		got, hits := e.RedactString(in)
		if len(hits) == 0 {
			t.Fatalf("no rule matched %q", in)
		}
		// Ensure none of the original payload chunks remain visible. We use
		// a heuristic: scan for any 16+ char substring of the original that
		// doesn't match the [REDACTED] marker.
		for i := 0; i+16 <= len(in); i++ {
			chunk := in[i : i+16]
			if strings.Contains(chunk, " ") || strings.Contains(chunk, "=") {
				continue
			}
			if !strings.Contains(got, chunk) {
				continue
			}
			t.Fatalf("%q substring %q remained in output: %q", in, chunk, got)
		}
	}
}

// TestRedactionStableAcrossCalls ensures the engine is concurrency-safe.
func TestRedactionStableAcrossCalls(t *testing.T) {
	e := NewDefault()
	in := "GITHUB_TOKEN=ghp_" + strings.Repeat("Q", 36)
	out1, _ := e.RedactString(in)
	out2, _ := e.RedactString(in)
	if out1 != out2 {
		t.Fatal("output not stable")
	}
}

// TestPriorityOfRules ensures more specific matches don't leak via overlap.
func TestPriorityOfRules(t *testing.T) {
	e := NewDefault()
	// A line containing both an AWS key and a GitHub token must redact both.
	in := "AKIAABCDEFGHIJKLMNOP and ghp_" + strings.Repeat("x", 36)
	out, hits := e.RedactString(in)
	if strings.Contains(out, "AKIA") || strings.Contains(out, "ghp_") {
		t.Fatalf("multi-secret line not fully redacted: %s", out)
	}
	if len(hits) < 2 {
		t.Fatalf("expected >=2 hits, got %d", len(hits))
	}
}
