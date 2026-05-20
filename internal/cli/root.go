// Package cli wires the cobra command tree for the reproforge binary.
package cli

import (
	"github.com/reproforge/reproforge/internal/logx"
	"github.com/reproforge/reproforge/internal/version"
	"github.com/spf13/cobra"
)

// Globals for flags.
var (
	flagLog       string
	flagLogJSON   bool
	flagOutputDir string
	flagToken     string
	flagConfig    string
)

// rootLogger is the shared logger.
var rootLogger *logx.Logger

// NewRoot returns the root command. Exposed for tests.
func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reproforge",
		Short: "Reproduce, diagnose and verify CI/CD failures",
		Long:  "ReproForge CI converts GitHub Actions failures into reproducible, sanitised, evidence-based capsules and verified diagnoses.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			rootLogger = logx.New(cmd.ErrOrStderr(), logx.ParseLevel(flagLog), flagLogJSON)
		},
		Version: version.Version + " (" + version.Commit + ")",
		SilenceUsage: true,
	}
	cmd.PersistentFlags().StringVar(&flagLog, "log", "info", "log level: debug|info|warn|error")
	cmd.PersistentFlags().BoolVar(&flagLogJSON, "log-json", false, "emit JSON logs to stderr")
	cmd.PersistentFlags().StringVar(&flagOutputDir, "out", "./reproforge-out", "output directory")
	cmd.PersistentFlags().StringVar(&flagToken, "token", "", "GitHub token (defaults to GITHUB_TOKEN/GH_TOKEN env)")
	cmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to .reproforge/config.yaml (default: search upwards)")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newCollectCmd())
	cmd.AddCommand(newCapsuleCmd())
	cmd.AddCommand(newReplayCmd())
	cmd.AddCommand(newDiagnoseCmd())
	cmd.AddCommand(newFlakeCmd())
	cmd.AddCommand(newReportCmd())
	cmd.AddCommand(newPatchCmd())
	cmd.AddCommand(newVerifyCmd())
	cmd.AddCommand(newFromRunCmd())
	cmd.AddCommand(newSchemaCmd())

	return cmd
}
