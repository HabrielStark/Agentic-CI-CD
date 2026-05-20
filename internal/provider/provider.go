// Package provider defines a CI-provider-agnostic interface used by the
// collector. The default implementation lives in internal/github; a stub
// for GitLab CI lives in internal/gitlab.
package provider

import (
	"context"
	"errors"
	"strings"
)

// Provider is the abstraction over a single CI provider's REST API surface.
// Each method must be safe to call concurrently and must return clear errors
// for unsupported run identifiers.
type Provider interface {
	// Name returns a stable identifier ("github_actions", "gitlab_ci", ...).
	Name() string

	// Parse turns a user-facing reference (URL or "<repo>:<runID>") into a
	// provider-specific RunRef encoded as opaque string. The Provider is
	// expected to round-trip its own format. The reference is consumed by
	// FetchRun.
	Parse(reference string) (RunRef, error)

	// FetchRun returns the structured Run + selected Job + step.
	FetchRun(ctx context.Context, ref RunRef) (*Run, error)

	// DownloadLogs returns a path-keyed map of the run's redacted logs
	// (keys: "<job>/<step>.txt"). Producers should NOT redact themselves;
	// the collector handles redaction.
	DownloadLogs(ctx context.Context, ref RunRef) (map[string][]byte, error)

	// DownloadWorkflowYAML returns the raw workflow YAML / pipeline DSL.
	DownloadWorkflowYAML(ctx context.Context, ref RunRef, run *Run) ([]byte, error)

	// ListArtifacts returns artifact metadata for the run.
	ListArtifacts(ctx context.Context, ref RunRef) ([]Artifact, error)

	// DownloadArtifact returns the zip bytes of an artifact.
	DownloadArtifact(ctx context.Context, ref RunRef, artifactID int64) ([]byte, error)
}

// RunRef is an opaque, provider-encoded reference. The collector treats
// `Owner/Repo` as a hint for capsule manifest fields but never inspects the
// rest.
type RunRef struct {
	Owner string
	Repo  string
	RunID int64
	JobID int64 // optional
	Extra map[string]string
}

// FullRepo returns "owner/repo".
func (r RunRef) FullRepo() string { return r.Owner + "/" + r.Repo }

// Run is a normalised view of a CI run.
type Run struct {
	ID         int64
	URL        string
	Workflow   string // workflow file path (e.g. ".github/workflows/ci.yml" or ".gitlab-ci.yml")
	HeadSHA    string
	HeadBranch string
	Status     string
	Conclusion string
	Jobs       []Job
}

// Job is a normalised view of a single CI job.
type Job struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	RunnerOS   string
	Labels     []string
	Steps      []Step
}

// Step is a single workflow step.
type Step struct {
	Name       string
	Status     string
	Conclusion string
	Number     int
}

// Artifact metadata.
type Artifact struct {
	ID                 int64
	Name               string
	SizeInBytes        int64
	URL                string
	ArchiveDownloadURL string
	Expired            bool
}

// PickFailedJob returns the first failing job (or the requested one) along
// with the step that triggered the failure.
func PickFailedJob(jobs []Job, want int64) (*Job, *Step) {
	for i := range jobs {
		j := &jobs[i]
		if want != 0 && j.ID != want {
			continue
		}
		if !strings.EqualFold(j.Conclusion, "failure") &&
			!strings.EqualFold(j.Conclusion, "timed_out") &&
			!strings.EqualFold(j.Conclusion, "cancelled") &&
			!strings.EqualFold(j.Conclusion, "failed") {
			continue
		}
		for k := range j.Steps {
			st := &j.Steps[k]
			if strings.EqualFold(st.Conclusion, "failure") || strings.EqualFold(st.Conclusion, "timed_out") || strings.EqualFold(st.Conclusion, "failed") {
				return j, st
			}
		}
		if n := len(j.Steps); n > 0 {
			return j, &j.Steps[n-1]
		}
		return j, nil
	}
	if want == 0 && len(jobs) > 0 {
		return &jobs[0], nil
	}
	return nil, nil
}

// ErrUnsupportedReference is returned by Parse on a malformed reference.
var ErrUnsupportedReference = errors.New("provider: unsupported reference")
