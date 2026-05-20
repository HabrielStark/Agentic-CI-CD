package replay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/logx"
)

// Shell launches an interactive bash inside a freshly built replay
// container at the failed step (FR-027). The shell shares stdin/stdout/
// stderr with the parent terminal. The image is built but the entrypoint
// is overridden to /bin/bash. Implementation requires an attached TTY.
func (e *Engine) Shell(ctx context.Context, c *capsule.Capsule, capsuleDir, sourceDir string, opts Options) error {
	if c == nil {
		return errors.New("capsule is nil")
	}
	if e.Logger == nil {
		e.Logger = logx.Default()
	}
	runtime := opts.Runtime
	if runtime == "" {
		runtime = detectRuntime()
	}
	if runtime == "" {
		return errors.New("docker/podman not found in PATH")
	}
	image := opts.Image
	if image == "" {
		image = AutoImage(c.Runner)
	}
	tag := "reproforge-replay:" + shortFingerprint(c.Failure.Fingerprint)

	ctxDir, err := os.MkdirTemp("", "rf-shell-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(ctxDir)
	if err := os.MkdirAll(filepath.Join(ctxDir, "source"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(ctxDir, "replay"), 0o755); err != nil {
		return err
	}
	if sourceDir != "" {
		if _, err := os.Stat(sourceDir); err == nil {
			if err := copyTree(sourceDir, filepath.Join(ctxDir, "source")); err != nil {
				return fmt.Errorf("copy source: %w", err)
			}
		} else {
			_ = os.WriteFile(filepath.Join(ctxDir, "source", ".keep"), []byte("(no source)\n"), 0o644)
		}
	} else {
		_ = os.WriteFile(filepath.Join(ctxDir, "source", ".keep"), []byte("(no source)\n"), 0o644)
	}
	if err := copyTree(filepath.Join(capsuleDir, "replay"), filepath.Join(ctxDir, "replay")); err != nil {
		return fmt.Errorf("copy replay: %w", err)
	}

	if !opts.DryRun {
		build := exec.CommandContext(ctx, runtime, "build", "-q", "-t", tag,
			"-f", filepath.Join(ctxDir, "replay", "Dockerfile"), ctxDir)
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	args := []string{"run", "-it", "--rm", "--entrypoint", "/bin/bash"}
	if opts.Network == NetworkDeny {
		args = append(args, "--network", "none")
	}
	if opts.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", opts.MemoryMB))
	}
	for _, k := range opts.EnvAllowlist {
		if v, ok := os.LookupEnv(k); ok {
			args = append(args, "-e", k+"="+v)
		}
	}
	for _, kv := range opts.ExtraEnv {
		args = append(args, "-e", kv)
	}
	args = append(args, tag)

	if opts.DryRun {
		e.Logger.Info("shell: dry-run", "cmd", runtime+" "+joinArgs(args))
		return nil
	}

	cmd := exec.CommandContext(ctx, runtime, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	return cmd.Run()
}

func joinArgs(in []string) string {
	out := ""
	for i, a := range in {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
