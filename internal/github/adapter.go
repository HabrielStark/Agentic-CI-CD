package github

import (
	"context"
	"fmt"

	"github.com/reproforge/reproforge/internal/provider"
)

// Adapter wraps Client and exposes it via the provider.Provider interface.
type Adapter struct {
	c *Client
}

// NewAdapter returns a provider.Provider backed by Client.
func NewAdapter(token string) provider.Provider {
	return &Adapter{c: NewClient(token)}
}

// Name implements provider.Provider.
func (a *Adapter) Name() string { return "github_actions" }

// Parse implements provider.Provider. Accepts only HTTPS run URLs.
func (a *Adapter) Parse(reference string) (provider.RunRef, error) {
	ref, err := ParseRunURL(reference)
	if err != nil {
		return provider.RunRef{}, err
	}
	return provider.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID, JobID: ref.JobID}, nil
}

// FetchRun implements provider.Provider.
func (a *Adapter) FetchRun(ctx context.Context, ref provider.RunRef) (*provider.Run, error) {
	r := RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID, JobID: ref.JobID}
	run, err := a.c.GetRun(ctx, r)
	if err != nil {
		return nil, err
	}
	jobs, err := a.c.ListJobs(ctx, r)
	if err != nil {
		return nil, err
	}
	out := &provider.Run{
		ID: run.ID, URL: run.URL, Workflow: run.Path,
		HeadSHA: run.HeadSHA, HeadBranch: run.HeadBranch,
		Status: run.Status, Conclusion: run.Conclusion,
	}
	for _, j := range jobs {
		pj := provider.Job{
			ID: j.ID, Name: j.Name,
			Status: j.Status, Conclusion: j.Conclusion,
			RunnerOS: j.RunnerOS, Labels: j.Labels,
		}
		for _, s := range j.Steps {
			pj.Steps = append(pj.Steps, provider.Step{
				Name: s.Name, Status: s.Status, Conclusion: s.Conclusion, Number: s.Number,
			})
		}
		out.Jobs = append(out.Jobs, pj)
	}
	return out, nil
}

// DownloadLogs implements provider.Provider.
func (a *Adapter) DownloadLogs(ctx context.Context, ref provider.RunRef) (map[string][]byte, error) {
	logs, err := a.c.DownloadRunLogs(ctx, RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID})
	if err != nil {
		// fall back to per-job logs if the run zip is unavailable
		if ref.JobID != 0 {
			body, jerr := a.c.DownloadJobLogs(ctx, RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}, ref.JobID)
			if jerr != nil {
				return nil, fmt.Errorf("download logs: %w", jerr)
			}
			return map[string][]byte{fmt.Sprintf("job-%d.txt", ref.JobID): body}, nil
		}
		return nil, err
	}
	return logs, nil
}

// DownloadWorkflowYAML implements provider.Provider.
func (a *Adapter) DownloadWorkflowYAML(ctx context.Context, ref provider.RunRef, run *provider.Run) ([]byte, error) {
	gr := &Run{Path: run.Workflow, HeadSHA: run.HeadSHA}
	return a.c.DownloadWorkflow(ctx, RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}, gr)
}

// ListArtifacts implements provider.Provider.
func (a *Adapter) ListArtifacts(ctx context.Context, ref provider.RunRef) ([]provider.Artifact, error) {
	arts, err := a.c.ListArtifacts(ctx, RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID})
	if err != nil {
		return nil, err
	}
	out := make([]provider.Artifact, 0, len(arts))
	for _, ar := range arts {
		out = append(out, provider.Artifact{
			ID: ar.ID, Name: ar.Name, SizeInBytes: ar.SizeInBytes,
			URL: ar.URL, ArchiveDownloadURL: ar.ArchiveDownloadURL, Expired: ar.Expired,
		})
	}
	return out, nil
}

// DownloadArtifact implements provider.Provider.
func (a *Adapter) DownloadArtifact(ctx context.Context, ref provider.RunRef, artifactID int64) ([]byte, error) {
	return a.c.DownloadArtifact(ctx, RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}, artifactID)
}

func init() {
	provider.Register("github_actions", func(token string) provider.Provider {
		return NewAdapter(token)
	})
}
