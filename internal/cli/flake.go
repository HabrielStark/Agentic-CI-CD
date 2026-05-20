package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/flake"
	"github.com/reproforge/reproforge/internal/replay"
	"github.com/spf13/cobra"
)

func newFlakeCmd() *cobra.Command {
	var (
		runs       int
		mode       string
		network    string
		variateNet bool
		variateSeed bool
		target     string
		image      string
		runtime    string
		timeout    int
		jsonOut    string
	)
	cmd := &cobra.Command{
		Use:   "flake <capsule>",
		Short: "Re-run a target N times to detect flakiness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capDir, c, err := loadCapsule(args[0])
			if err != nil {
				return err
			}
			r := flake.New(rootLogger)
			ctx := context.Background()
			sum, err := r.Run(ctx, c, capDir, filepath.Join(capDir, "source"), flake.Options{
				Target: target, Runs: runs, Mode: mode, Network: network,
				VariateNetwork: variateNet, VariateSeed: variateSeed,
				Image: image, Runtime: runtime, TimeoutSec: timeout,
			})
			if err != nil {
				return err
			}
			if sum.IsFlaky() {
				fmt.Fprintf(cmd.OutOrStdout(), "FLAKY: %d/%d passed, %d/%d failed\n", sum.Passed, sum.Runs, sum.Failed, sum.Runs)
			} else if sum.Failed > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "stable failure across %d runs\n", sum.Runs)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "stable pass across %d runs\n", sum.Runs)
			}
			if jsonOut != "" {
				b, _ := json.MarshalIndent(sum, "", "  ")
				if err := os.WriteFile(jsonOut, b, 0o644); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&runs, "runs", 10, "number of reruns")
	cmd.Flags().StringVar(&mode, "mode", replay.ModeFailedStep, "replay mode")
	cmd.Flags().StringVar(&network, "network", replay.NetworkAllow, "network policy")
	cmd.Flags().BoolVar(&variateNet, "variate-network", false, "alternate network on/off across runs")
	cmd.Flags().BoolVar(&variateSeed, "variate-seed", true, "vary REPROFORGE_SEED/PYTHONHASHSEED across runs")
	cmd.Flags().StringVar(&target, "target", "", "explicit test target id (informational)")
	cmd.Flags().StringVar(&image, "image", "", "override base image")
	cmd.Flags().StringVar(&runtime, "runtime", "", "container runtime")
	cmd.Flags().IntVar(&timeout, "timeout", 600, "per-run timeout in seconds")
	cmd.Flags().StringVar(&jsonOut, "json", "", "write summary to a JSON file")
	return cmd
}
