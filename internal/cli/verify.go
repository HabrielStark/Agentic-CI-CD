package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/replay"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	var (
		mode    string
		network string
		image   string
		runtime string
		timeout int
		jsonOut string
	)
	cmd := &cobra.Command{
		Use:   "verify <capsule>",
		Short: "Replay the capsule and confirm whether the failure reproduces",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capDir, c, err := loadCapsule(args[0])
			if err != nil {
				return err
			}
			eng := replay.NewEngine(rootLogger)
			opts := replay.Options{
				Mode: mode, Network: network, Image: image, Runtime: runtime, TimeoutSec: timeout,
			}
			if err := eng.Generate(c, capDir, opts); err != nil {
				return err
			}
			ctx := context.Background()
			out, err := eng.Run(ctx, c, capDir, filepath.Join(capDir, "source"), opts)
			result := map[string]any{
				"mode":            out.Mode,
				"network":         out.Network,
				"exitCode":        out.ExitCode,
				"originalExit":    c.Failure.ExitCode,
				"reproduced":      out.Reproduced,
				"durationMs":      out.Duration.Milliseconds(),
				"reason":          out.Reason,
			}
			if jsonOut != "" {
				b, _ := json.MarshalIndent(result, "", "  ")
				_ = os.WriteFile(jsonOut, b, 0o644)
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			_, _ = cmd.OutOrStdout().Write(b)
			fmt.Fprintln(cmd.OutOrStdout())
			if err != nil && !out.Reproduced {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", replay.ModeFailedStep, "replay mode")
	cmd.Flags().StringVar(&network, "network", replay.NetworkAllow, "network policy")
	cmd.Flags().StringVar(&image, "image", "", "override base image")
	cmd.Flags().StringVar(&runtime, "runtime", "", "container runtime")
	cmd.Flags().IntVar(&timeout, "timeout", 1800, "max wall-clock seconds")
	cmd.Flags().StringVar(&jsonOut, "json", "", "write result to JSON")
	return cmd
}
