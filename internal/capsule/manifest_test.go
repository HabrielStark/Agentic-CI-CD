package capsule

import (
	"strings"
	"testing"
	"time"
)

func validCapsule() *Capsule {
	return &Capsule{
		Schema:    SchemaV1,
		CreatedAt: time.Now().UTC(),
		Generator: "reproforge test",
		Provider:  ProviderGitHub,
		Repo:      "octocat/hello",
		Commit:    "abc123",
		Workflow:  "ci.yml",
		Job:       "build",
		Runner:    Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Failure: Failure{
			Step:        "Run tests",
			Command:     "pytest -q",
			ExitCode:    1,
			Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
		Replay:    Replay{Modes: []string{ReplayFailedStep}, Network: NetworkConfigurable},
		Redaction: Redaction{Status: "passed", Rules: 12},
	}
}

func TestValidate_OK(t *testing.T) {
	c := validCapsule()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	cases := map[string]func(*Capsule){
		"empty schema":     func(c *Capsule) { c.Schema = "" },
		"wrong schema":     func(c *Capsule) { c.Schema = "x/v0" },
		"empty provider":   func(c *Capsule) { c.Provider = "" },
		"empty repo":       func(c *Capsule) { c.Repo = "" },
		"empty commit":     func(c *Capsule) { c.Commit = "" },
		"empty workflow":   func(c *Capsule) { c.Workflow = "" },
		"empty job":        func(c *Capsule) { c.Job = "" },
		"empty step":       func(c *Capsule) { c.Failure.Step = "" },
		"empty fingerprint": func(c *Capsule) { c.Failure.Fingerprint = "" },
		"bad fingerprint":  func(c *Capsule) { c.Failure.Fingerprint = "md5:abc" },
		"no os":            func(c *Capsule) { c.Runner.OS = "" },
		"no modes":         func(c *Capsule) { c.Replay.Modes = nil },
		"bad mode":         func(c *Capsule) { c.Replay.Modes = []string{"weird"} },
		"bad network":      func(c *Capsule) { c.Replay.Network = "bogus" },
		"empty redaction":  func(c *Capsule) { c.Redaction.Status = "" },
		"empty log path":   func(c *Capsule) { c.Logs = []LogFile{{Path: "", Job: "j", SHA256: "1"}} },
		"empty artifact path": func(c *Capsule) { c.Artifacts = []Artifact{{Name: "n"}} },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			c := validCapsule()
			mut(c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected error for case %q", name)
			}
		})
	}
}

func TestDecodeStrict_RejectsUnknownFields(t *testing.T) {
	bad := []byte(`{"schema":"reproforge.capsule/v1","unknownField":1}`)
	if _, err := Decode(bad); err == nil {
		t.Fatal("expected decode error for unknown field")
	}
}

func TestHashContents_Stable(t *testing.T) {
	c := validCapsule()
	c1 := c.HashContents()
	c.CreatedAt = time.Now().Add(time.Hour) // should not change hash
	c2 := c.HashContents()
	if c1 != c2 {
		t.Fatalf("hash changed when timestamp changed: %s vs %s", c1, c2)
	}
}

func TestHashContents_DiffersOnPayload(t *testing.T) {
	a := validCapsule()
	b := validCapsule()
	b.Failure.Command = "different"
	if a.HashContents() == b.HashContents() {
		t.Fatal("expected different hashes")
	}
}

func TestMarshalJSON_DeterministicFieldOrder(t *testing.T) {
	c := validCapsule()
	b, err := c.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	// schema must appear before provider; provider before repo (struct order)
	if idx := strings.Index(string(b), `"schema"`); idx < 0 {
		t.Fatal("schema field missing")
	}
	s, p, r := strings.Index(string(b), `"schema"`),
		strings.Index(string(b), `"provider"`),
		strings.Index(string(b), `"repo"`)
	if !(s < p && p < r) {
		t.Fatalf("unexpected field order: %s", string(b))
	}
}
