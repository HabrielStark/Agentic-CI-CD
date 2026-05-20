package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/collect"
	"github.com/spf13/cobra"
)

func newCapsuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capsule",
		Short: "Build, inspect, or extract capsules",
	}
	cmd.AddCommand(newCapsuleCreateCmd())
	cmd.AddCommand(newCapsuleInspectCmd())
	cmd.AddCommand(newCapsuleExtractCmd())
	return cmd
}

func newCapsuleCreateCmd() *cobra.Command {
	var (
		runURL string
		runID  int64
		repo   string
		out    string
		withArtifacts bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a tar.zst capsule from a GitHub Actions run",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := resolveRunRef(runURL, runID, repo)
			if err != nil {
				return err
			}
			ctx := context.Background()
			res, err := collect.FromRun(ctx, ref, collect.Options{
				OutputDir: flagOutputDir, IncludeArtifacts: withArtifacts,
				Token: flagToken, Diagnose: false, Logger: rootLogger,
			})
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(flagOutputDir, fmt.Sprintf("rf-%d.tar.zst", ref.RunID))
			}
			capDir := filepath.Join(res.OutputDir, "capsule")
			if err := capsule.PackFile(out, capsule.PackOptions{
				SourceDir: capDir, Manifest: res.Capsule,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&runURL, "url", "", "GitHub Actions run URL")
	cmd.Flags().Int64Var(&runID, "run", 0, "Run ID")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output capsule path (default: <out>/rf-<run>.tar.zst)")
	cmd.Flags().BoolVar(&withArtifacts, "artifacts", false, "include artifacts")
	return cmd
}

func newCapsuleInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <capsule.tar.zst>",
		Short: "Print capsule manifest summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tmp, err := os.MkdirTemp("", "rf-inspect-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			c, err := capsule.UnpackFile(args[0], tmp)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Schema:      %s\n", c.Schema)
			fmt.Fprintf(cmd.OutOrStdout(), "Provider:    %s\n", c.Provider)
			fmt.Fprintf(cmd.OutOrStdout(), "Repo:        %s\n", c.Repo)
			fmt.Fprintf(cmd.OutOrStdout(), "Workflow:    %s\n", c.Workflow)
			fmt.Fprintf(cmd.OutOrStdout(), "Job:         %s\n", c.Job)
			fmt.Fprintf(cmd.OutOrStdout(), "Commit:      %s\n", c.Commit)
			fmt.Fprintf(cmd.OutOrStdout(), "Step:        %s\n", c.Failure.Step)
			fmt.Fprintf(cmd.OutOrStdout(), "ExitCode:    %d\n", c.Failure.ExitCode)
			fmt.Fprintf(cmd.OutOrStdout(), "Fingerprint: %s\n", c.Failure.Fingerprint)
			fmt.Fprintf(cmd.OutOrStdout(), "Logs:        %d  Artifacts: %d\n", len(c.Logs), len(c.Artifacts))
			return nil
		},
	}
	return cmd
}

func newCapsuleExtractCmd() *cobra.Command {
	var dest string
	cmd := &cobra.Command{
		Use:   "extract <capsule.tar.zst>",
		Short: "Extract a capsule to a directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dest == "" {
				dest = "./capsule"
			}
			c, err := capsule.UnpackFile(args[0], dest)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "extracted to %s (job=%s)\n", dest, c.Job)
			return nil
		},
	}
	cmd.Flags().StringVarP(&dest, "dest", "d", "", "destination directory")
	return cmd
}

// readManifestFile reads the manifest JSON from a path.
func readManifestFile(p string) (*capsule.Capsule, error) {
	if p == "" {
		return nil, errors.New("path empty")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return capsule.DecodeReader(bufio.NewReader(f))
}
