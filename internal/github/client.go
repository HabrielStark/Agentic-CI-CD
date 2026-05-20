package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// API host constants.
const (
	DefaultAPIBase = "https://api.github.com"
)

// Doer abstracts http.Client for testability.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a small GitHub Actions REST client. It supports unauthenticated
// access for public repos and bearer auth via the standard PAT header.
type Client struct {
	APIBase   string
	Token     string
	HTTP      Doer
	UserAgent string
	// MaxRetries for transient errors. 0 means default 3.
	MaxRetries int
}

// NewClient returns a configured client. Auth is read from token if non-empty,
// otherwise from GITHUB_TOKEN/GH_TOKEN env vars.
func NewClient(token string) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	hc := &http.Client{
		Timeout: 60 * time.Second,
		// Don't follow redirects automatically: GitHub returns 302 with
		// presigned URLs that reject the bearer header.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Client{
		APIBase:    DefaultAPIBase,
		Token:      token,
		HTTP:       hc,
		UserAgent:  "reproforge/0.1",
		MaxRetries: 3,
	}
}

// Run is a workflow run summary.
type Run struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Path         string    `json:"path"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	WorkflowID   int64     `json:"workflow_id"`
	URL          string    `json:"html_url"`
	JobsURL      string    `json:"jobs_url"`
	LogsURL      string    `json:"logs_url"`
	ArtifactsURL string    `json:"artifacts_url"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	RunAttempt   int       `json:"run_attempt"`
	Repository   struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// Job is a workflow job result.
type Job struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	StartedAt  time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	RunID      int64     `json:"run_id"`
	RunURL     string    `json:"run_url"`
	HTMLURL    string    `json:"html_url"`
	Labels     []string  `json:"labels"`
	RunnerName string    `json:"runner_name"`
	RunnerOS   string    `json:"runner_os"`
	Steps      []Step    `json:"steps"`
}

// Step is a job step.
type Step struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	Number      int       `json:"number"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

// Artifact is a CI artifact reference.
type Artifact struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	SizeInBytes        int64     `json:"size_in_bytes"`
	URL                string    `json:"url"`
	ArchiveDownloadURL string    `json:"archive_download_url"`
	Expired            bool      `json:"expired"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, method, path, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", c.UserAgent)
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoff(i))
			continue
		}
		// Retry on 5xx and 429.
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("github %s: %d %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
			ra := resp.Header.Get("Retry-After")
			if ra != "" {
				if d, err := strconv.Atoi(ra); err == nil {
					time.Sleep(time.Duration(d) * time.Second)
					continue
				}
			}
			time.Sleep(backoff(i))
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("github: request failed")
	}
	return nil, lastErr
}

func backoff(attempt int) time.Duration {
	d := time.Duration(200*(1<<attempt)) * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// GetRun fetches a workflow run.
func (c *Client) GetRun(ctx context.Context, ref RunRef) (*Run, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d", c.APIBase, ref.Owner, ref.Repo, ref.RunID)
	resp, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get run: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var run Run
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListJobs returns all jobs for a run (paginated).
func (c *Client) ListJobs(ctx context.Context, ref RunRef) ([]Job, error) {
	var out []Job
	page := 1
	for {
		u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/jobs?per_page=100&page=%d&filter=all",
			c.APIBase, ref.Owner, ref.Repo, ref.RunID, page)
		resp, err := c.do(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		var page1 struct {
			TotalCount int   `json:"total_count"`
			Jobs       []Job `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page1); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		out = append(out, page1.Jobs...)
		if len(page1.Jobs) < 100 {
			return out, nil
		}
		page++
	}
}

// DownloadJobLogs fetches plain-text logs for a single job. The endpoint
// returns a redirect to a short-lived presigned URL.
func (c *Client) DownloadJobLogs(ctx context.Context, ref RunRef, jobID int64) ([]byte, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", c.APIBase, ref.Owner, ref.Repo, jobID)
	resp, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 302 {
		loc, err := absoluteLocation(resp, u)
		if err != nil {
			return nil, err
		}
		return c.downloadDirect(ctx, loc)
	}
	if resp.StatusCode == 200 {
		return io.ReadAll(resp.Body)
	}
	body, _ := io.ReadAll(resp.Body)
	return nil, fmt.Errorf("get job logs: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// DownloadRunLogs fetches the zip archive for a run's logs and returns a map
// of inner-path → content.
func (c *Client) DownloadRunLogs(ctx context.Context, ref RunRef) (map[string][]byte, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/logs", c.APIBase, ref.Owner, ref.Repo, ref.RunID)
	resp, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw []byte
	switch {
	case resp.StatusCode == 302:
		loc, err := absoluteLocation(resp, u)
		if err != nil {
			return nil, err
		}
		raw, err = c.downloadDirect(ctx, loc)
		if err != nil {
			return nil, err
		}
	case resp.StatusCode == 200:
		raw, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get run logs: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)
		if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		out[clean] = b
	}
	return out, nil
}

// DownloadWorkflow fetches the workflow YAML for a run.
func (c *Client) DownloadWorkflow(ctx context.Context, ref RunRef, run *Run) ([]byte, error) {
	if run == nil {
		var err error
		run, err = c.GetRun(ctx, ref)
		if err != nil {
			return nil, err
		}
	}
	if run.Path == "" {
		return nil, errors.New("workflow path missing on run")
	}
	// raw.githubusercontent.com is preferred when token is available
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Repo), url.PathEscape(run.HeadSHA), run.Path)
	body, err := c.downloadDirect(ctx, rawURL)
	if err == nil {
		return body, nil
	}
	// Fallback to contents API
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		c.APIBase, ref.Owner, ref.Repo, run.Path, run.HeadSHA)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/vnd.github.raw")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("workflow contents: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

// ListArtifacts returns artifacts for a run.
func (c *Client) ListArtifacts(ctx context.Context, ref RunRef) ([]Artifact, error) {
	var out []Artifact
	page := 1
	for {
		u := fmt.Sprintf("%s/repos/%s/%s/actions/runs/%d/artifacts?per_page=100&page=%d",
			c.APIBase, ref.Owner, ref.Repo, ref.RunID, page)
		resp, err := c.do(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		var pageData struct {
			TotalCount int        `json:"total_count"`
			Artifacts  []Artifact `json:"artifacts"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&pageData); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		out = append(out, pageData.Artifacts...)
		if len(pageData.Artifacts) < 100 {
			return out, nil
		}
		page++
	}
}

// DownloadArtifact returns the zip archive bytes for an artifact.
func (c *Client) DownloadArtifact(ctx context.Context, ref RunRef, artifactID int64) ([]byte, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/actions/artifacts/%d/zip", c.APIBase, ref.Owner, ref.Repo, artifactID)
	resp, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 302 {
		loc, err := absoluteLocation(resp, u)
		if err != nil {
			return nil, err
		}
		return c.downloadDirect(ctx, loc)
	}
	if resp.StatusCode == 200 {
		return io.ReadAll(resp.Body)
	}
	body, _ := io.ReadAll(resp.Body)
	return nil, fmt.Errorf("artifact zip: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func (c *Client) downloadDirect(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download %s: %d %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}
