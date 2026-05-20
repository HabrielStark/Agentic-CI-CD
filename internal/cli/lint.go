package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/wflint"
	"github.com/spf13/cobra"
)

func newLintCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "lint <workflow.yml | capsule>",
		Short: "Statically lint a GitHub Actions workflow for security issues",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, path, err := loadWorkflow(args[0])
			if err != nil {
				return err
			}
			findings := wflint.Lint(body)
			switch format {
			case "json":
				b, _ := json.MarshalIndent(findings, "", "  ")
				_, _ = cmd.OutOrStdout().Write(b)
				fmt.Fprintln(cmd.OutOrStdout())
			default:
				if len(findings) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: clean\n", path)
				}
				for _, f := range findings {
					fmt.Fprintf(cmd.OutOrStdout(), "%s:%d [%s] %s\n  %s\n", path, f.Line, f.Severity, f.Rule, f.Message)
					if f.Hint != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "  hint: %s\n", f.Hint)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "text|json")
	return cmd
}

// loadWorkflow returns the workflow YAML body for either a path or the
// workflow/* file inside an extracted capsule directory.
func loadWorkflow(path string) ([]byte, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		b, err := os.ReadFile(path)
		return b, path, err
	}
	wfDir := filepath.Join(path, "workflow")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		if !e.IsDir() && (filepath.Ext(e.Name()) == ".yml" || filepath.Ext(e.Name()) == ".yaml") {
			full := filepath.Join(wfDir, e.Name())
			b, err := os.ReadFile(full)
			return b, full, err
		}
	}
	return nil, "", fmt.Errorf("no workflow .yml found under %s", wfDir)
}
