package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/reproforge/reproforge/internal/ai"
	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/spf13/cobra"
)

func newPatchCmd() *cobra.Command {
	var (
		provider string
		jsonOut  string
		verify   bool
		noSecrets bool
	)
	cmd := &cobra.Command{
		Use:   "patch <capsule>",
		Short: "Ask an AI for a patch plan and (optionally) verify it via replay",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = noSecrets // always true; secrets are never sent
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
			adapter, err := ai.NewAdapter(ai.Provider(provider))
			if err != nil {
				return err
			}
			ctx := context.Background()
			prompt := ai.SanitisedPrompt(c, d, nil)
			patch, err := adapter.SuggestPatch(ctx, prompt)
			if err != nil {
				return err
			}
			result := map[string]any{
				"plan":       patch.Plan,
				"diff":       patch.Diff,
				"files":      patch.Files,
				"verified":   false,
				"verifyMode": verify,
			}
			if verify {
				// In MVP: we generate replay scripts but don't apply patches
				// without source. We mark verified=false to be honest unless
				// downstream `verify` succeeds.
				result["note"] = "patch verification requires the source tree; run `reproforge verify` after applying the patch in a temp branch."
			}
			if jsonOut != "" {
				b, _ := json.MarshalIndent(result, "", "  ")
				if err := os.WriteFile(jsonOut, b, 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", jsonOut)
			} else {
				b, _ := json.MarshalIndent(result, "", "  ")
				_, _ = cmd.OutOrStdout().Write(b)
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "ai", "none", "ai provider: none|local|claude|openai")
	cmd.Flags().StringVar(&jsonOut, "json", "", "write patch info to a JSON file")
	cmd.Flags().BoolVar(&verify, "verify", true, "require replay verification before claiming patch is verified")
	cmd.Flags().BoolVar(&noSecrets, "no-secrets", true, "never include secrets in AI requests (always true)")
	return cmd
}
