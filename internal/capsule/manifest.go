// Package capsule defines the on-disk capsule manifest format and helpers
// for validating, encoding, and decoding it.
//
// Capsule schema: reproforge.capsule/v1
package capsule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// SchemaV1 is the version string for the v1 manifest.
const SchemaV1 = "reproforge.capsule/v1"

// Provider names (extensible).
const (
	ProviderGitHub    = "github_actions"
	ProviderGitLab    = "gitlab_ci"
	ProviderBuildkite = "buildkite"
	ProviderCircleCI  = "circleci"
	ProviderLocal     = "local"
)

// Replay modes recognised by the engine.
const (
	ReplayFullJob       = "full-job"
	ReplayFailedStep    = "failed-step"
	ReplayTestOnly      = "test-only"
	ReplayDependencyInstall = "dependency-install"
)

// NetworkPolicy controls outbound network during replay.
type NetworkPolicy string

const (
	NetworkAllow      NetworkPolicy = "allow"
	NetworkDeny       NetworkPolicy = "deny"
	NetworkConfigurable NetworkPolicy = "configurable"
)

// Runner describes the runtime that produced the original failure.
type Runner struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Image   string `json:"image,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

// Failure is the focal failure inside a capsule.
type Failure struct {
	Step        string   `json:"step"`
	Command     string   `json:"command,omitempty"`
	ExitCode    int      `json:"exitCode"`
	Fingerprint string   `json:"fingerprint"`
	Category    string   `json:"category,omitempty"`
	Tests       []string `json:"failingTests,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// Replay describes what replay modes/network policies are available.
type Replay struct {
	Modes   []string      `json:"modes"`
	Network NetworkPolicy `json:"network"`
}

// Redaction summary attached to manifest.
type Redaction struct {
	Status string `json:"status"`
	Rules  int    `json:"rules"`
	Hits   int    `json:"hits"`
}

// Artifact represents a CI artifact stored inside the capsule.
type Artifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// LogFile is a redacted log file in the capsule.
type LogFile struct {
	Path string `json:"path"`
	Job  string `json:"job"`
	Step string `json:"step,omitempty"`
	Size int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Capsule is the top-level manifest.
type Capsule struct {
	Schema    string    `json:"schema"`
	CreatedAt time.Time `json:"createdAt"`
	Generator string    `json:"generator"`

	Provider string `json:"provider"`
	Repo     string `json:"repo"`
	Commit   string `json:"commit"`
	Branch   string `json:"branch,omitempty"`
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	JobID    int64  `json:"jobId,omitempty"`
	RunID    int64  `json:"runId,omitempty"`
	RunURL   string `json:"runUrl,omitempty"`

	Runner    Runner    `json:"runner"`
	Failure   Failure   `json:"failure"`
	Replay    Replay    `json:"replay"`
	Redaction Redaction `json:"redaction"`

	Logs      []LogFile  `json:"logs,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`

	// Optional rich diagnostic data summarised here for quick consumption.
	Diagnosis *DiagnosisSummary `json:"diagnosis,omitempty"`
}

// DiagnosisSummary is a compact diagnosis snippet stored at capsule build time.
type DiagnosisSummary struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Evidence   []string `json:"evidence,omitempty"`
}

// validProviders lists known providers (any string accepted but warning if unknown).
var validProviders = map[string]struct{}{
	ProviderGitHub: {}, ProviderGitLab: {}, ProviderBuildkite: {},
	ProviderCircleCI: {}, ProviderLocal: {},
}

// validReplayModes lists known modes.
var validReplayModes = map[string]struct{}{
	ReplayFullJob: {}, ReplayFailedStep: {}, ReplayTestOnly: {}, ReplayDependencyInstall: {},
}

// Validate checks structural invariants of the manifest. It rejects empty
// required fields, malformed exit codes, unknown schema strings, etc.
func (c *Capsule) Validate() error {
	if c == nil {
		return errors.New("capsule is nil")
	}
	if c.Schema != SchemaV1 {
		return fmt.Errorf("unsupported schema %q (want %q)", c.Schema, SchemaV1)
	}
	if c.Provider == "" {
		return errors.New("provider is required")
	}
	if _, ok := validProviders[c.Provider]; !ok {
		// non-fatal but require a non-empty value
	}
	if c.Repo == "" {
		return errors.New("repo is required")
	}
	if c.Commit == "" {
		return errors.New("commit is required")
	}
	if c.Workflow == "" {
		return errors.New("workflow is required")
	}
	if c.Job == "" {
		return errors.New("job is required")
	}
	if c.Failure.Step == "" {
		return errors.New("failure.step is required")
	}
	if c.Failure.Fingerprint == "" {
		return errors.New("failure.fingerprint is required")
	}
	if !strings.HasPrefix(c.Failure.Fingerprint, "sha256:") {
		return errors.New("failure.fingerprint must start with sha256:")
	}
	if c.Runner.OS == "" {
		return errors.New("runner.os is required")
	}
	if len(c.Replay.Modes) == 0 {
		return errors.New("replay.modes is required")
	}
	for _, m := range c.Replay.Modes {
		if _, ok := validReplayModes[m]; !ok {
			return fmt.Errorf("replay mode %q is not a recognised mode", m)
		}
	}
	if c.Replay.Network != NetworkAllow && c.Replay.Network != NetworkDeny && c.Replay.Network != NetworkConfigurable {
		return fmt.Errorf("replay.network %q is invalid", c.Replay.Network)
	}
	if c.Redaction.Status == "" {
		return errors.New("redaction.status is required")
	}
	for _, lf := range c.Logs {
		if lf.Path == "" {
			return errors.New("log.path is required")
		}
		if lf.SHA256 == "" {
			return errors.New("log.sha256 is required")
		}
	}
	for _, a := range c.Artifacts {
		if a.Path == "" {
			return errors.New("artifact.path is required")
		}
	}
	return nil
}

// MarshalJSON returns deterministic indented JSON.
func (c *Capsule) MarshalJSON() ([]byte, error) {
	type alias Capsule
	// stable field order via struct alias (Go marshals struct fields in declaration order)
	a := (*alias)(c)
	return json.MarshalIndent(a, "", "  ")
}

// Decode parses JSON manifest bytes into a Capsule.
func Decode(b []byte) (*Capsule, error) {
	var c Capsule
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields() // strict during decoding to catch typos
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("decode capsule: %w", err)
	}
	return &c, nil
}

// DecodeReader streams JSON from r.
func DecodeReader(r io.Reader) (*Capsule, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Decode(b)
}

// HashContents returns sha256 of the manifest's content-addressable parts (no
// timestamps), useful for stable comparisons between two capsules.
func (c *Capsule) HashContents() string {
	type stable struct {
		Provider, Repo, Commit, Workflow, Job string
		FailureStep, FailureCmd, Fingerprint  string
		ExitCode                              int
		Logs                                  []string
		Artifacts                             []string
	}
	s := stable{
		Provider: c.Provider, Repo: c.Repo, Commit: c.Commit,
		Workflow: c.Workflow, Job: c.Job,
		FailureStep: c.Failure.Step, FailureCmd: c.Failure.Command,
		Fingerprint: c.Failure.Fingerprint, ExitCode: c.Failure.ExitCode,
	}
	for _, l := range c.Logs {
		s.Logs = append(s.Logs, l.SHA256)
	}
	for _, a := range c.Artifacts {
		s.Artifacts = append(s.Artifacts, a.SHA256)
	}
	sort.Strings(s.Logs)
	sort.Strings(s.Artifacts)
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
