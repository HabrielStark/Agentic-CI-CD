package collect

import (
	"context"
	"net/http"

	"github.com/reproforge/reproforge/internal/github"
)

// fromRunWithClientForTest is a test-only helper that performs the same
// FromRun pipeline but against a custom GitHub API base URL.
//
// We deliberately keep this in a non-test file so the integration test from
// another file can use it without exporting it from the package.
func fromRunWithClientForTest(ctx context.Context, apiBase string, ref github.RunRef, opts Options) (*Result, error) {
	// Indirection: build an http client that doesn't follow redirects (matches
	// production NewClient). Provide it via a closure-driven RunClient.
	hc := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// monkeypatch via the package helper below
	prev := newClientForCollect
	newClientForCollect = func(token string) *github.Client {
		c := github.NewClient(token)
		c.APIBase = apiBase
		c.HTTP = hc
		return c
	}
	defer func() { newClientForCollect = prev }()
	return FromRun(ctx, ref, opts)
}
