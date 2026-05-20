package redaction

import (
	"bytes"
	"strings"
	"testing"
)

func TestDefaultRules_RedactsKnownSecrets(t *testing.T) {
	t.Parallel()
	e := NewDefault()

	cases := []struct {
		name string
		in   string
		want string // substring that must NOT appear in the output
	}{
		{"github classic pat", "GH_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij12 leaked", "ghp_"},
		{"github oauth", "auth=gho_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA12 secret", "gho_"},
		{"jwt", "Authorization: Bearer eyJabcdefghij.eyJabcdefghij.signaturepartXX leak", "eyJabcdefghij"},
		{"aws akid", "id=AKIAABCDEFGHIJKLMNOP", "AKIAABCDEFGHIJKLMNOP"},
		{"aws secret kv", "AWS_SECRET_ACCESS_KEY=abcdefghijklmnopqrstuvwxyz0123456789abcd", "abcdefghijklmnopqrstuvwxyz0123456789abcd"},
		{"openai", "key=sk-abcdefghijklmnopqrstuvwxyz1234", "sk-abcdef"},
		{"anthropic", "key=sk-ant-abcdefghijklmnopqrstuvwxyz", "sk-ant-"},
		{"google", "AIzaSyABC1234567890abcdefghij1234567890abc", "AIzaSyABC"},
		{"npm", "npm_abcdefghijklmnopqrstuvwxyz0123456789", "npm_abcdefg"},
		{"slack", "token=xoxb-1234567890-abcdefghij", "xoxb-"},
		{"stripe", "sk_live_abcdefghijklmnop", "sk_live_abcd"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIB\n-----END RSA PRIVATE KEY-----", "BEGIN RSA"},
		{"json private key", `"private_key": "-----BEGIN PRIVATE KEY-----abc-----END-----"`, "private_key"},
		{"env kv", "PASSWORD=Sup3rSecretValueXXXXXXXXX", "Sup3rSecretValueXXXXXXXXX"},
		{"bearer", "authorization: Bearer abcdefghijklmnopqrstuvwxyz12345", "abcdefghijklmnopqrstuvwxyz12345"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, hits := e.RedactString(tc.in)
			if strings.Contains(got, tc.want) {
				t.Fatalf("output still contains forbidden substring %q\noutput=%s", tc.want, got)
			}
			if len(hits) == 0 {
				t.Fatalf("no hit for %q (input=%q)", tc.name, tc.in)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("expected [REDACTED] marker in %q", got)
			}
			for _, h := range hits {
				if h.Hash == "" || h.Length == 0 {
					t.Fatalf("hit missing fields: %+v", h)
				}
			}
		})
	}
}

func TestDenylist(t *testing.T) {
	e := NewDefault()
	e.AddDenylist([]string{"super-secret-value-xx"})
	out, hits := e.RedactString("config: super-secret-value-xx in line")
	if strings.Contains(out, "super-secret-value-xx") {
		t.Fatalf("expected denylist match")
	}
	if len(hits) == 0 {
		t.Fatalf("expected at least one hit")
	}
}

func TestRedactionDoesNotMatchPlainText(t *testing.T) {
	e := NewDefault()
	in := "this is normal log output with no secrets"
	out, hits := e.RedactString(in)
	if out != in {
		t.Fatalf("output mutated: %q", out)
	}
	if len(hits) != 0 {
		t.Fatalf("unexpected hits: %v", hits)
	}
}

func TestRedactStream(t *testing.T) {
	e := NewDefault()
	r := strings.NewReader("line1 ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nline2 normal\nline3 sk-ant-abcdefghijklmnopqrstuvwxyz")
	var w bytes.Buffer
	hits, err := e.Redact(r, &w)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected >=2 hits, got %d", len(hits))
	}
	out := w.String()
	if strings.Contains(out, "ghp_") || strings.Contains(out, "sk-ant-") {
		t.Fatalf("secrets remained in stream output: %q", out)
	}
}

func TestAddPattern_Invalid(t *testing.T) {
	e := NewDefault()
	if err := e.AddPattern("bad", "[invalid"); err == nil {
		t.Fatal("expected error compiling invalid regex")
	}
}

func TestReportFields(t *testing.T) {
	e := NewDefault()
	hits := []Hit{{Rule: "x", Hash: "abc", Length: 4}}
	rep := e.Report([]string{"b.txt", "a.txt"}, hits)
	if rep.Status != "passed" || rep.Rules == 0 {
		t.Fatalf("bad report: %+v", rep)
	}
	if rep.Files[0] != "a.txt" {
		t.Fatalf("files not sorted: %v", rep.Files)
	}
}

func TestRedactionBoundaries_NoFalsePositiveOnVersions(t *testing.T) {
	e := NewDefault()
	in := "version 1.2.3, ID 0123456 number"
	if e.HasMatch(in) {
		t.Fatalf("unexpected match on version-like text: %q", in)
	}
}

// Property-style: every non-empty default rule must be a non-nil regex.
func TestAllDefaultRulesValid(t *testing.T) {
	for _, r := range DefaultRules() {
		if r.Re == nil || r.Name == "" {
			t.Fatalf("invalid default rule: %+v", r)
		}
	}
}
