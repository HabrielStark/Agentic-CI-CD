package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/collect"
	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/github"
	"github.com/reproforge/reproforge/internal/report"
	"github.com/spf13/cobra"
)

func newFromRunCmd() *cobra.Command {
	var (
		runURL string
		runID  int64
		repo   string
		withArtifacts bool
		writeCapsule bool
		writeReport  string
	)
	cmd := &cobra.Command{
		Use:   "from-run [url]",
		Short: "Single-shot pipeline: collect, build capsule, diagnose, and emit a report",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && runURL == "" {
				runURL = args[0]
			}
			ref, err := resolveRunRef(runURL, runID, repo)
			if err != nil {
				return err
			}
			ctx := context.Background()
			res, err := collect.FromRun(ctx, ref, collect.Options{
				OutputDir: flagOutputDir, IncludeArtifacts: withArtifacts,
				Token: flagToken, Diagnose: true, Logger: rootLogger,
			})
			if err != nil {
				return err
			}
			capDir := filepath.Join(res.OutputDir, "capsule")
			scan, suites, err := scanCapsuleLogs(capDir)
			if err != nil {
				return err
			}
			d := diagnose.Classify(diagnose.Input{
				LogScan: scan, Suites: suites,
				JobName: res.Capsule.Job, StepName: res.Capsule.Failure.Step,
				Command: res.Capsule.Failure.Command, OS: res.Capsule.Runner.OS,
				Provider: res.Capsule.Provider,
			})
			b := report.Bundle{Capsule: res.Capsule, Diagnosis: d}
			md := report.Markdown(b)
			fmt.Fprint(cmd.OutOrStdout(), md)
			if writeReport != "" {
				if err := writeFile(writeReport, []byte(md)); err != nil {
					return err
				}
			}
			if writeCapsule {
				out := filepath.Join(flagOutputDir, fmt.Sprintf("rf-%d.tar.zst", ref.RunID))
				if err := capsule.PackFile(out, capsule.PackOptions{
					SourceDir: capDir, Manifest: res.Capsule,
				}); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\nwrote capsule: %s\n", out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runURL, "url", "", "GitHub Actions run URL")
	cmd.Flags().Int64Var(&runID, "run", 0, "Run ID")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo")
	cmd.Flags().BoolVar(&withArtifacts, "artifacts", false, "include artifacts")
	cmd.Flags().BoolVar(&writeCapsule, "write-capsule", true, "also write tar.zst capsule")
	cmd.Flags().StringVar(&writeReport, "write-report", "", "write Markdown report to file")
	return cmd
}

func writeFile(p string, body []byte) error {
	return writeFileImpl(p, body)
}

// indirected so tests can inject; default is os.WriteFile.
var writeFileImpl = func(p string, body []byte) error {
	return osWriteFile(p, body, 0o644)
}

// Avoid an extra import line in CLI files; we re-export os.WriteFile here.
func osWriteFile(p string, body []byte, mode uint32) error {
	return writeWithMode(p, body, mode)
}

// suppress unused warning for github import (used elsewhere indirectly)
var _ = github.RunRef{}
