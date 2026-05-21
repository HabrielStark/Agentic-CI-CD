package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/replay"
	"github.com/spf13/cobra"
)
func newReplayCmd() *cobra.Command {
	var (
		mode, network, image, runtime string
		memoryMB                       int
		cpus                           float64
		envs                           []string
		extraEnv                       []string
		dryRun                         bool
		generateOnly                   bool
		timeout                        int
		profile                        bool
		trace                          string
	)
	cmd := &cobra.Command{
		Use:   "replay <capsule.tar.zst|capsule-dir>",
		Short: "Replay a capsule's failed step in a generated container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			capsulePath := args[0]
			capDir, c, err := loadCapsule(capsulePath)
			if err != nil {
				return err
			}
			eng := replay.NewEngine(rootLogger)
			opts := replay.Options{
				Mode: mode, Network: network, Image: image, Runtime: runtime,
				MemoryMB: memoryMB, CPUs: cpus, EnvAllowlist: envs,
				ExtraEnv: extraEnv, DryRun: dryRun, TimeoutSec: timeout,
				WorkDir: capDir, Profile: profile,
				Trace: replay.TraceMode(trace),
			}
			if err := eng.Generate(c, capDir, opts); err != nil {
				return err
			}
			if generateOnly || dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "replay scripts written to %s/replay/\n", capDir)
				return nil
			}
			ctx := context.Background()
			out, err := eng.Run(ctx, c, capDir, filepath.Join(capDir, "source"), opts)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "replay failed: %v\n", err)
				if out.ExitCode == 0 {
					out.ExitCode = 2
				}
			}
			repro := "no"
			if out.Reproduced {
				repro = "yes"
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"replay: mode=%s network=%s exit=%d duration=%s reproduced=%s\n",
				out.Mode, out.Network, out.ExitCode, out.Duration, repro)
			if out.Profile != nil {
				body, _ := json.MarshalIndent(out.Profile, "", "  ")
				outPath := filepath.Join(capDir, "replay", "resource-profile.json")
				_ = os.WriteFile(outPath, body, 0o644)
				fmt.Fprintf(cmd.OutOrStdout(), "profile: peakCpu=%.1f%% peakMem=%dMB written=%s\n",
					out.Profile.PeakCPUPct, out.Profile.PeakMemBytes/(1<<20), outPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", replay.ModeFailedStep, "replay mode: full-job|failed-step|test-only|dependency-install")
	cmd.Flags().StringVar(&network, "network", replay.NetworkAllow, "network policy: allow|deny")
	cmd.Flags().StringVar(&image, "image", "", "override base image")
	cmd.Flags().StringVar(&runtime, "runtime", "", "container runtime: docker|podman (auto)")
	cmd.Flags().IntVar(&memoryMB, "memory", 0, "memory limit in MB")
	cmd.Flags().Float64Var(&cpus, "cpus", 0, "cpu cores limit")
	cmd.Flags().StringSliceVar(&envs, "env-from", nil, "env vars to forward from caller (key)")
	cmd.Flags().StringSliceVar(&extraEnv, "env", nil, "explicit KEY=VAL env vars")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "generate replay scripts but do not execute")
	cmd.Flags().BoolVar(&generateOnly, "generate-only", false, "alias for --dry-run")
	cmd.Flags().IntVar(&timeout, "timeout", 1800, "max wall-clock seconds for replay")
	cmd.Flags().BoolVar(&profile, "profile", false, "collect 1Hz CPU/memory/IO samples while the replay runs")
	cmd.Flags().StringVar(&trace, "trace", "", "wrap failed command in tracer: strace|ltrace")
	return cmd
}

// loadCapsule accepts either a tar.zst file or an extracted directory.
func loadCapsule(arg string) (string, *capsule.Capsule, error) {
	info, err := os.Stat(arg)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		// look for capsule.json
		c, err := readManifestFile(filepath.Join(arg, capsule.CapsuleFileName))
		return arg, c, err
	}
	if !strings.HasSuffix(strings.ToLower(arg), ".zst") &&
		!strings.HasSuffix(strings.ToLower(arg), ".tar.zst") {
		return "", nil, fmt.Errorf("unsupported capsule path %q", arg)
	}
	tmp, err := os.MkdirTemp("", "rf-extract-*")
	if err != nil {
		return "", nil, err
	}
	c, err := capsule.UnpackFile(arg, tmp)
	if err != nil {
		return "", nil, err
	}
	return tmp, c, nil
}
