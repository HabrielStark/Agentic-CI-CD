package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/reproforge/reproforge/internal/collect"
	"github.com/reproforge/reproforge/internal/github"
	"github.com/reproforge/reproforge/internal/provider"
	"github.com/spf13/cobra"
)

func newCollectCmd() *cobra.Command {
	var (
		runURL string
		runID  int64
		repo   string
		withArtifacts bool
		alsoDiagnose bool
		providerName string
	)
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect a CI run (logs, workflow, jobs, artifacts) into a sanitised capsule directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRunRef(runURL, runID, repo)
			if err != nil {
				return err
			}
			ctx := context.Background()
			res, err := collect.FromRun(ctx, ref, collect.Options{
				OutputDir: flagOutputDir, IncludeArtifacts: withArtifacts,
				Token: flagToken, Diagnose: alsoDiagnose, Logger: rootLogger,
				Provider: providerName,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "fingerprint: %s\n", res.Capsule.Failure.Fingerprint)
			return nil
		},
	}
	cmd.Flags().StringVar(&runURL, "url", "", "GitHub Actions run URL")
	cmd.Flags().Int64Var(&runID, "run", 0, "GitHub Actions run ID (requires --repo)")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo")
	cmd.Flags().BoolVar(&withArtifacts, "artifacts", false, "also download artifacts")
	cmd.Flags().BoolVar(&alsoDiagnose, "diagnose", false, "compute diagnosis during collection")
	cmd.Flags().StringVar(&providerName, "provider", "github_actions", "github_actions | gitlab_ci")
	return cmd
}

// resolveRunRef returns a provider.RunRef from the user-facing flags.
func resolveRunRef(runURL string, runID int64, repo string) (provider.RunRef, error) {
	if runURL != "" {
		ref, err := github.ParseRunURL(runURL)
		if err == nil {
			return provider.RunRef{Owner: ref.Owner, Repo: ref.Repo, RunID: ref.RunID, JobID: ref.JobID}, nil
		}
		// fall through and let provider Detect handle non-github URLs.
		name, derr := provider.Detect(runURL)
		if derr != nil {
			return provider.RunRef{}, fmt.Errorf("unrecognised run URL: %v / %v", err, derr)
		}
		f, _ := provider.Get(name)
		p := f("")
		return p.Parse(runURL)
	}
	if runID == 0 || repo == "" {
		return provider.RunRef{}, errors.New("provide --url, or both --run and --repo")
	}
	parts := splitRepo(repo)
	if len(parts) != 2 {
		return provider.RunRef{}, errors.New("--repo must be owner/repo")
	}
	return provider.RunRef{Owner: parts[0], Repo: parts[1], RunID: runID}, nil
}

func splitRepo(s string) []string {
	out := []string{"", ""}
	for i, r := range s {
		if r == '/' {
			out[0] = s[:i]
			out[1] = s[i+1:]
			break
		}
	}
	if out[0] == "" || out[1] == "" {
		return nil
	}
	return out
}
