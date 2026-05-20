package cli

import (
	"fmt"
	"os"

	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/report"
	"github.com/spf13/cobra"
)

func newReportCmd() *cobra.Command {
	var (
		format string
		out    string
	)
	cmd := &cobra.Command{
		Use:   "report <capsule>",
		Short: "Render a Markdown / JSON / SARIF report from a capsule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capDir, c, err := loadCapsule(args[0])
			if err != nil {
				return err
			}
			scan, suites, err := scanCapsuleLogs(capDir)
			if err != nil {
				return err
			}
			d := diagnose.Classify(diagnose.Input{
				LogScan: scan, Suites: suites,
				JobName: c.Job, StepName: c.Failure.Step,
				Command: c.Failure.Command, OS: c.Runner.OS,
				Provider: c.Provider,
			})
			b := report.Bundle{Capsule: c, Diagnosis: d}
			var data []byte
			switch format {
			case "markdown", "md":
				data = []byte(report.Markdown(b))
			case "json":
				data, err = report.JSON(b)
			case "sarif":
				data, err = report.SARIF(b)
			case "issue":
				data = []byte(report.IssueTemplate(b))
			default:
				return fmt.Errorf("unknown format: %s", format)
			}
			if err != nil {
				return err
			}
			if out == "" {
				_, err := cmd.OutOrStdout().Write(data)
				return err
			}
			return os.WriteFile(out, data, 0o644)
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "format: markdown|json|sarif|issue")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output file (default stdout)")
	return cmd
}
