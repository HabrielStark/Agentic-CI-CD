// Package buildkite implements a minimal Buildkite CI provider adapter.
// It registers itself as "buildkite" in the provider registry.
package buildkite

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/reproforge/reproforge/internal/provider"
)

// Adapter implements provider.Provider for Buildkite.
type Adapter struct {
	Token string
}

func init() {
	provider.Register("buildkite", func(token string) provider.Provider {
		return &Adapter{Token: token}
	})
}

func (a *Adapter) Name() string { return "buildkite" }

// Parse accepts:
//   https://buildkite.com/<org>/<pipeline>/builds/<number>
func (a *Adapter) Parse(reference string) (provider.RunRef, error) {
	if reference == "" {
		return provider.RunRef{}, provider.ErrUnsupportedReference
	}
	u, err := url.Parse(reference)
	if err != nil || u.Host == "" {
		return provider.RunRef{}, provider.ErrUnsupportedReference
	}
	if !strings.Contains(u.Host, "buildkite.com") {
		return provider.RunRef{}, provider.ErrUnsupportedReference
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// org/pipeline/builds/number
	if len(parts) < 4 || parts[2] != "builds" {
		return provider.RunRef{}, fmt.Errorf("buildkite: not a build URL: %s", reference)
	}
	num, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return provider.RunRef{}, fmt.Errorf("buildkite: bad build number: %w", err)
	}
	return provider.RunRef{
		Owner: parts[0], Repo: parts[1], RunID: num,
		Extra: map[string]string{"org": parts[0], "pipeline": parts[1]},
	}, nil
}

func (a *Adapter) FetchRun(ctx context.Context, ref provider.RunRef) (*provider.Run, error) {
	return nil, errors.New("buildkite: FetchRun not yet implemented (contribute at github.com/reproforge/reproforge)")
}

func (a *Adapter) DownloadLogs(ctx context.Context, ref provider.RunRef) (map[string][]byte, error) {
	return nil, errors.New("buildkite: DownloadLogs not yet implemented")
}

func (a *Adapter) DownloadWorkflowYAML(ctx context.Context, ref provider.RunRef, run *provider.Run) ([]byte, error) {
	return nil, errors.New("buildkite: DownloadWorkflowYAML not yet implemented")
}

func (a *Adapter) ListArtifacts(ctx context.Context, ref provider.RunRef) ([]provider.Artifact, error) {
	return nil, errors.New("buildkite: ListArtifacts not yet implemented")
}

func (a *Adapter) DownloadArtifact(ctx context.Context, ref provider.RunRef, artifactID int64) ([]byte, error) {
	return nil, errors.New("buildkite: DownloadArtifact not yet implemented")
}
