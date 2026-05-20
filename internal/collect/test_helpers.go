package collect

import (
	"context"
	"net/http"

	"github.com/reproforge/reproforge/internal/github"
	"github.com/reproforge/reproforge/internal/provider"
)

// fromRunWithClientForTest is a test-only helper that performs the same
// FromRun pipeline but against a custom GitHub API base URL.
func fromRunWithClientForTest(ctx context.Context, apiBase string, ref provider.RunRef, opts Options) (*Result, error) {
	hc := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	c := github.NewClient("test")
	c.APIBase = apiBase
	c.HTTP = hc
	prev := testProvider
	testProvider = &fakeProviderFromGH{c: c}
	defer func() { testProvider = prev }()
	return FromRun(ctx, ref, opts)
}

// fakeProviderFromGH wraps a github.Client and exposes it via the provider
// interface, but without having to register the adapter (the same Client
// type is already used by production code).
type fakeProviderFromGH struct {
	c *github.Client
}

func (f *fakeProviderFromGH) Name() string { return "github_actions" }

func (f *fakeProviderFromGH) Parse(ref string) (provider.RunRef, error) {
	return provider.RunRef{}, nil
}

func (f *fakeProviderFromGH) FetchRun(ctx context.Context, ref provider.RunRef) (*provider.Run, error) {
	r := github.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}
	run, err := f.c.GetRun(ctx, r)
	if err != nil {
		return nil, err
	}
	jobs, err := f.c.ListJobs(ctx, r)
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

func (f *fakeProviderFromGH) DownloadLogs(ctx context.Context, ref provider.RunRef) (map[string][]byte, error) {
	return f.c.DownloadRunLogs(ctx, github.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID})
}
func (f *fakeProviderFromGH) DownloadWorkflowYAML(ctx context.Context, ref provider.RunRef, run *provider.Run) ([]byte, error) {
	gr := &github.Run{Path: run.Workflow, HeadSHA: run.HeadSHA}
	return f.c.DownloadWorkflow(ctx, github.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}, gr)
}
func (f *fakeProviderFromGH) ListArtifacts(ctx context.Context, ref provider.RunRef) ([]provider.Artifact, error) {
	arts, err := f.c.ListArtifacts(ctx, github.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID})
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
func (f *fakeProviderFromGH) DownloadArtifact(ctx context.Context, ref provider.RunRef, artifactID int64) ([]byte, error) {
	return f.c.DownloadArtifact(ctx, github.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID}, artifactID)
}
