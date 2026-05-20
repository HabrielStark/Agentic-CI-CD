package cli

import (
	"context"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/replay"
	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	var (
		image, runtime string
		network        string
		memMB          int
		envs, extraEnv []string
		dryRun         bool
	)
	cmd := &cobra.Command{
		Use:   "shell <capsule>",
		Short: "Open an interactive bash inside the replay container at the failed step",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capDir, c, err := loadCapsule(args[0])
			if err != nil {
				return err
			}
			eng := replay.NewEngine(rootLogger)
			opts := replay.Options{
				Mode: replay.ModeFailedStep, Network: network, Image: image,
				Runtime: runtime, MemoryMB: memMB, EnvAllowlist: envs, ExtraEnv: extraEnv,
				DryRun: dryRun,
			}
			if err := eng.Generate(c, capDir, opts); err != nil {
				return err
			}
			ctx := context.Background()
			return eng.Shell(ctx, c, capDir, filepath.Join(capDir, "source"), opts)
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "override base image")
	cmd.Flags().StringVar(&runtime, "runtime", "", "container runtime")
	cmd.Flags().StringVar(&network, "network", replay.NetworkAllow, "allow|deny")
	cmd.Flags().IntVar(&memMB, "memory", 0, "memory limit in MB")
	cmd.Flags().StringSliceVar(&envs, "env-from", nil, "env vars to forward from caller")
	cmd.Flags().StringSliceVar(&extraEnv, "env", nil, "explicit KEY=VAL env vars")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the command without executing")
	return cmd
}
