package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/parsers"
	"github.com/spf13/cobra"
)

func newDiagnoseCmd() *cobra.Command {
	var (
		jsonOut string
	)
	cmd := &cobra.Command{
		Use:   "diagnose <capsule.tar.zst|capsule-dir>",
		Short: "Classify the failure inside a capsule and emit a diagnosis",
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
			if jsonOut != "" {
				b, _ := json.MarshalIndent(d, "", "  ")
				if err := os.WriteFile(jsonOut, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", jsonOut)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "category:    %s\nconfidence:  %.2f\nfingerprint: %s\nstep:        %s\n",
				d.Category, d.Confidence, c.Failure.Fingerprint, c.Failure.Step)
			if len(d.Tests) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "failingTests:\n")
				for _, t := range d.Tests {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", t)
				}
			}
			if len(d.Evidence) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "evidence:\n")
				for _, e := range d.Evidence {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonOut, "json", "", "write diagnosis to a JSON file")
	return cmd
}

// scanCapsuleLogs scans every redacted log inside the capsule and returns the
// merged signals plus any JUnit suites it can find under artifacts/.
func scanCapsuleLogs(capDir string) (parsers.LogScan, parsers.Suites, error) {
	patterns := parsers.DefaultPatterns()
	logsDir := filepath.Join(capDir, "logs")
	var merged parsers.LogScan
	if _, err := os.Stat(logsDir); err == nil {
		err := filepath.WalkDir(logsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			s, err := parsers.ScanLog(f, patterns)
			if err != nil {
				return err
			}
			merged = mergeScan(merged, s)
			return nil
		})
		if err != nil {
			return parsers.LogScan{}, nil, err
		}
	}
	suites := scanArtifacts(filepath.Join(capDir, "artifacts"))
	return merged, suites, nil
}

func mergeScan(a, b parsers.LogScan) parsers.LogScan {
	if a.ExitCode == 0 {
		a.ExitCode = b.ExitCode
	}
	if a.StepError == "" {
		a.StepError = b.StepError
	}
	a.NetworkHits = append(a.NetworkHits, b.NetworkHits...)
	a.DNSHits = append(a.DNSHits, b.DNSHits...)
	a.TLSHits = append(a.TLSHits, b.TLSHits...)
	a.TimeoutHits = append(a.TimeoutHits, b.TimeoutHits...)
	a.OOMHits = append(a.OOMHits, b.OOMHits...)
	a.PermHits = append(a.PermHits, b.PermHits...)
	a.DepHits = append(a.DepHits, b.DepHits...)
	a.ChecksumHits = append(a.ChecksumHits, b.ChecksumHits...)
	a.MissingEnv = append(a.MissingEnv, b.MissingEnv...)
	a.MissingFile = append(a.MissingFile, b.MissingFile...)
	a.YAMLExpr = append(a.YAMLExpr, b.YAMLExpr...)
	a.StackBlocks = append(a.StackBlocks, b.StackBlocks...)
	if a.StackTop == "" {
		a.StackTop = b.StackTop
	}
	if a.ErrorClass == "" {
		a.ErrorClass = b.ErrorClass
	}
	if len(a.RawTail) == 0 {
		a.RawTail = b.RawTail
	}
	return a
}

// scanArtifacts walks a directory and aggregates JUnit suites from any *.xml.
func scanArtifacts(root string) parsers.Suites {
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	var out parsers.Suites
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(p)
		if !strings.HasSuffix(lower, ".xml") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		s, err := parsers.JUnitXML(f)
		if err != nil {
			return nil
		}
		out = append(out, s...)
		return nil
	})
	return out
}
