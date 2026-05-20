// Package gitlab implements a minimal GitLab CI provider adapter using only
// the public REST API. It is wired into provider.Registry as "gitlab_ci".
//
// The adapter is intentionally small. It fetches the pipeline, its jobs,
// per-job trace logs, the .gitlab-ci.yml at the pipeline ref, and any
// artifacts. It does NOT yet support GitLab's nested includes; the raw
// .gitlab-ci.yml as committed is returned.
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/reproforge/reproforge/internal/provider"
)

const defaultBase = "https://gitlab.com/api/v4"

// Adapter implements provider.Provider for GitLab CI.
type Adapter struct {
	APIBase string
	Token   string
	HTTP    *http.Client
}

// NewAdapter returns a configured adapter. Token defaults to GITLAB_TOKEN
// if empty.
func NewAdapter(token string) *Adapter {
	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}
	return &Adapter{
		APIBase: defaultBase,
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements provider.Provider.
func (a *Adapter) Name() string { return "gitlab_ci" }

// Parse implements provider.Provider.
//
// Supported reference formats:
//   https://gitlab.com/<group>/<project>/-/pipelines/<id>
//   https://gitlab.com/<group>/<project>/-/jobs/<id>
//   <project_id>:<pipeline_id>            (numeric, server-agnostic)
func (a *Adapter) Parse(reference string) (provider.RunRef, error) {
	if reference == "" {
		return provider.RunRef{}, provider.ErrUnsupportedReference
	}
	if i := strings.Index(reference, ":"); i > 0 && !strings.HasPrefix(reference, "http") {
		owner, repo := "project", reference[:i]
		runID, err := strconv.ParseInt(reference[i+1:], 10, 64)
		if err != nil {
			return provider.RunRef{}, fmt.Errorf("gitlab: bad numeric ref %q: %w", reference, err)
		}
		return provider.RunRef{Owner: owner, Repo: repo, RunID: runID, Extra: map[string]string{"project": reference[:i]}}, nil
	}
	u, err := url.Parse(reference)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return provider.RunRef{}, fmt.Errorf("gitlab: invalid url %q", reference)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return provider.RunRef{}, fmt.Errorf("gitlab: not a pipeline url: %s", reference)
	}
	// find "-" segment
	dashAt := -1
	for i, p := range parts {
		if p == "-" {
			dashAt = i
			break
		}
	}
	if dashAt < 0 || dashAt == len(parts)-1 {
		return provider.RunRef{}, fmt.Errorf("gitlab: missing /-/ segment: %s", reference)
	}
	kind := parts[dashAt+1]
	if kind != "pipelines" && kind != "jobs" {
		return provider.RunRef{}, fmt.Errorf("gitlab: only /pipelines/ or /jobs/ urls are supported, got %q", kind)
	}
	if dashAt+2 >= len(parts) {
		return provider.RunRef{}, fmt.Errorf("gitlab: missing id in %s", reference)
	}
	id, err := strconv.ParseInt(parts[dashAt+2], 10, 64)
	if err != nil {
		return provider.RunRef{}, fmt.Errorf("gitlab: bad id %q: %w", parts[dashAt+2], err)
	}
	owner := parts[0]
	repo := strings.Join(parts[1:dashAt], "/") // group/sub/project supported
	ref := provider.RunRef{Owner: owner, Repo: repo, RunID: id, Extra: map[string]string{"server": u.Host}}
	if kind == "jobs" {
		ref.JobID = id
	}
	return ref, nil
}

func (a *Adapter) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := a.APIBase + path
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if a.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", a.Token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "reproforge/0.1")
	return a.HTTP.Do(req)
}

func (a *Adapter) projectPath(ref provider.RunRef) string {
	if pid, ok := ref.Extra["project"]; ok && pid != "" {
		return pid
	}
	// URL-encode "owner/repo" → owner%2Frepo
	return url.PathEscape(ref.Owner + "/" + ref.Repo)
}

// FetchRun implements provider.Provider.
func (a *Adapter) FetchRun(ctx context.Context, ref provider.RunRef) (*provider.Run, error) {
	pid := a.projectPath(ref)

	resp, err := a.get(ctx, "/projects/"+pid+"/pipelines/"+strconv.FormatInt(ref.RunID, 10), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab pipeline: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var pipe struct {
		ID         int64  `json:"id"`
		SHA        string `json:"sha"`
		Ref        string `json:"ref"`
		Status     string `json:"status"`
		WebURL     string `json:"web_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pipe); err != nil {
		return nil, err
	}

	// list jobs
	resp, err = a.get(ctx, "/projects/"+pid+"/pipelines/"+strconv.FormatInt(ref.RunID, 10)+"/jobs", url.Values{"per_page": []string{"100"}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab jobs: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var jobs []struct {
		ID     int64    `json:"id"`
		Name   string   `json:"name"`
		Status string   `json:"status"`
		Stage  string   `json:"stage"`
		Tags   []string `json:"tag_list"`
		Runner *struct {
			Description string `json:"description"`
		} `json:"runner"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, err
	}

	out := &provider.Run{
		ID: pipe.ID, URL: pipe.WebURL,
		Workflow: ".gitlab-ci.yml", HeadSHA: pipe.SHA,
		HeadBranch: pipe.Ref, Status: pipe.Status, Conclusion: pipe.Status,
	}
	for _, j := range jobs {
		conclusion := j.Status
		if j.Status == "success" {
			conclusion = "success"
		}
		runnerOS := ""
		if j.Runner != nil {
			runnerOS = j.Runner.Description
		}
		out.Jobs = append(out.Jobs, provider.Job{
			ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: conclusion,
			RunnerOS: runnerOS, Labels: j.Tags,
			Steps: []provider.Step{{Name: j.Stage + ": " + j.Name, Status: j.Status, Conclusion: conclusion, Number: 1}},
		})
	}
	return out, nil
}

// DownloadLogs implements provider.Provider.
func (a *Adapter) DownloadLogs(ctx context.Context, ref provider.RunRef) (map[string][]byte, error) {
	run, err := a.FetchRun(ctx, ref)
	if err != nil {
		return nil, err
	}
	pid := a.projectPath(ref)
	out := map[string][]byte{}
	for _, j := range run.Jobs {
		resp, err := a.get(ctx, "/projects/"+pid+"/jobs/"+strconv.FormatInt(j.ID, 10)+"/trace", nil)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		out[fmt.Sprintf("%s.txt", j.Name)] = body
	}
	if len(out) == 0 {
		return nil, errors.New("gitlab: no logs available")
	}
	return out, nil
}

// DownloadWorkflowYAML implements provider.Provider.
func (a *Adapter) DownloadWorkflowYAML(ctx context.Context, ref provider.RunRef, run *provider.Run) ([]byte, error) {
	pid := a.projectPath(ref)
	resp, err := a.get(ctx, "/projects/"+pid+"/repository/files/.gitlab-ci.yml/raw",
		url.Values{"ref": []string{run.HeadSHA}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gitlab workflow: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ListArtifacts implements provider.Provider.
func (a *Adapter) ListArtifacts(ctx context.Context, ref provider.RunRef) ([]provider.Artifact, error) {
	// GitLab artifacts are attached to jobs; we expose them as one artifact
	// per job to keep the API symmetric with GitHub.
	run, err := a.FetchRun(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Artifact, 0, len(run.Jobs))
	for _, j := range run.Jobs {
		out = append(out, provider.Artifact{
			ID: j.ID, Name: j.Name, SizeInBytes: 0,
			ArchiveDownloadURL: fmt.Sprintf("%s/projects/%s/jobs/%d/artifacts",
				a.APIBase, a.projectPath(ref), j.ID),
		})
	}
	return out, nil
}

// DownloadArtifact implements provider.Provider.
func (a *Adapter) DownloadArtifact(ctx context.Context, ref provider.RunRef, artifactID int64) ([]byte, error) {
	pid := a.projectPath(ref)
	resp, err := a.get(ctx, "/projects/"+pid+"/jobs/"+strconv.FormatInt(artifactID, 10)+"/artifacts", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab artifact: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func init() {
	provider.Register("gitlab_ci", func(token string) provider.Provider {
		return NewAdapter(token)
	})
}
