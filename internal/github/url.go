// Package github implements the GitHub Actions provider adapter.
package github

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// RunRef identifies a GitHub Actions run.
type RunRef struct {
	Owner  string
	Repo   string
	RunID  int64
	JobID  int64 // optional
	Server string // e.g. "github.com" (for GHES support later)
}

// FullRepo returns "owner/repo".
func (r RunRef) FullRepo() string { return r.Owner + "/" + r.Repo }

// ParseRunURL parses URLs of the form
//   https://github.com/<owner>/<repo>/actions/runs/<id>
//   https://github.com/<owner>/<repo>/actions/runs/<id>/attempts/<n>
//   https://github.com/<owner>/<repo>/actions/runs/<id>/job/<jobID>
func ParseRunURL(s string) (RunRef, error) {
	if s == "" {
		return RunRef{}, errors.New("empty url")
	}
	u, err := url.Parse(s)
	if err != nil {
		return RunRef{}, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return RunRef{}, fmt.Errorf("invalid url: missing scheme/host")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// owner/repo/actions/runs/<id>[/attempts/<n>][/job/<jobid>]
	if len(parts) < 5 || parts[2] != "actions" || parts[3] != "runs" {
		return RunRef{}, fmt.Errorf("not a workflow run url: %s", s)
	}
	id, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return RunRef{}, fmt.Errorf("invalid run id %q: %w", parts[4], err)
	}
	ref := RunRef{Owner: parts[0], Repo: parts[1], RunID: id, Server: u.Host}
	for i := 5; i < len(parts)-1; i++ {
		if parts[i] == "job" {
			if v, err := strconv.ParseInt(parts[i+1], 10, 64); err == nil {
				ref.JobID = v
			}
		}
	}
	return ref, nil
}
