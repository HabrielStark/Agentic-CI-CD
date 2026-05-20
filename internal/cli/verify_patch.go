package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/fingerprint"
	"github.com/reproforge/reproforge/internal/parsers"
	"github.com/reproforge/reproforge/internal/replay"
	"github.com/spf13/cobra"
)

// PatchOutcome is the JSON document produced by verify-patch.
type PatchOutcome struct {
	OriginalFingerprint string  `json:"originalFingerprint"`
	BeforeFingerprint   string  `json:"beforeFingerprint"`
	AfterFingerprint    string  `json:"afterFingerprint"`
	BeforeReproduced    bool    `json:"beforeReproduced"`
	AfterReproduced     bool    `json:"afterReproduced"`
	Verified            bool    `json:"verified"`
	Reason              string  `json:"reason"`
	BeforeExitCode      int     `json:"beforeExitCode"`
	AfterExitCode       int     `json:"afterExitCode"`
	DurationMs          int64   `json:"durationMs"`
	PatchSHA256         string  `json:"patchSha256"`
	BeforeCategory      string  `json:"beforeCategory,omitempty"`
	AfterCategory       string  `json:"afterCategory,omitempty"`
}

func newVerifyPatchCmd() *cobra.Command {
	var (
		patchFile string
		jsonOut   string
		runtime   string
		network   string
		mode      string
		image     string
		timeout   int
	)
	cmd := &cobra.Command{
		Use:   "verify-patch <capsule>",
		Short: "Apply a patch in a temp branch and confirm the failure no longer reproduces (FR-029)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if patchFile == "" {
				return errors.New("--patch is required")
			}
			capDir, c, err := loadCapsule(args[0])
			if err != nil {
				return err
			}
			patchBytes, err := os.ReadFile(patchFile)
			if err != nil {
				return fmt.Errorf("read patch: %w", err)
			}
			sum := sha256.Sum256(patchBytes)

			eng := replay.NewEngine(rootLogger)
			ropts := replay.Options{Mode: mode, Network: network, Runtime: runtime, Image: image, TimeoutSec: timeout}
			if err := eng.Generate(c, capDir, ropts); err != nil {
				return err
			}

			ctx := context.Background()
			sourceDir := filepath.Join(capDir, "source")

			before, err := eng.Run(ctx, c, capDir, sourceDir, ropts)
			if err != nil && before.ExitCode == 0 {
				before.ExitCode = 2
			}

			// Apply patch in a temp copy of source.
			patchedDir, perr := applyPatchToCopy(sourceDir, patchBytes)
			if perr != nil {
				return fmt.Errorf("apply patch: %w", perr)
			}
			defer os.RemoveAll(patchedDir)

			after, aerr := eng.Run(ctx, c, capDir, patchedDir, ropts)
			if aerr != nil && after.ExitCode == 0 {
				after.ExitCode = 2
			}

			beforeFP := fingerprintFor(c, before, capDir)
			afterFP := fingerprintFor(c, after, capDir)

			out := PatchOutcome{
				OriginalFingerprint: c.Failure.Fingerprint,
				BeforeFingerprint:   beforeFP,
				AfterFingerprint:    afterFP,
				BeforeReproduced:    before.Reproduced || before.ExitCode != 0,
				AfterReproduced:     after.Reproduced || after.ExitCode != 0,
				BeforeExitCode:      before.ExitCode,
				AfterExitCode:       after.ExitCode,
				DurationMs:          (before.Duration + after.Duration).Milliseconds(),
				PatchSHA256:         hex.EncodeToString(sum[:]),
			}
			out.Verified = out.BeforeReproduced && !out.AfterReproduced && out.AfterExitCode == 0
			switch {
			case out.Verified:
				out.Reason = "patch reproduces failure on HEAD and resolves it on patched tree"
			case !out.BeforeReproduced:
				out.Reason = "did not reproduce on HEAD; patch cannot be verified"
			case out.AfterReproduced:
				out.Reason = "patch did not resolve the failure (still reproduces)"
			default:
				out.Reason = "verification inconclusive"
			}

			body, _ := json.MarshalIndent(out, "", "  ")
			if jsonOut != "" {
				if err := os.WriteFile(jsonOut, body, 0o644); err != nil {
					return err
				}
			}
			_, _ = cmd.OutOrStdout().Write(body)
			fmt.Fprintln(cmd.OutOrStdout())
			if !out.Verified {
				return errors.New("patch verification failed: " + out.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&patchFile, "patch", "", "path to a unified diff to apply (required)")
	cmd.Flags().StringVar(&jsonOut, "json", "", "write outcome to a JSON file")
	cmd.Flags().StringVar(&runtime, "runtime", "", "container runtime")
	cmd.Flags().StringVar(&network, "network", replay.NetworkAllow, "allow|deny")
	cmd.Flags().StringVar(&mode, "mode", replay.ModeFailedStep, "replay mode")
	cmd.Flags().StringVar(&image, "image", "", "override base image")
	cmd.Flags().IntVar(&timeout, "timeout", 1800, "max wall-clock seconds per replay")
	return cmd
}

// applyPatchToCopy clones src into a temp directory and runs `git apply`
// against the patch. If src is not a git checkout, falls back to `patch -p1`.
func applyPatchToCopy(src string, patch []byte) (string, error) {
	dst, err := os.MkdirTemp("", "rf-patched-*")
	if err != nil {
		return "", err
	}
	if src != "" {
		if _, err := os.Stat(src); err == nil {
			if err := copyTree(src, dst); err != nil {
				return "", err
			}
		}
	}
	patchPath := filepath.Join(dst, ".reproforge.patch")
	if err := os.WriteFile(patchPath, patch, 0o644); err != nil {
		return "", err
	}
	// Try git first
	if _, err := exec.LookPath("git"); err == nil {
		// Initialise repo if needed so git apply works.
		init := exec.Command("git", "init", "-q")
		init.Dir = dst
		_ = init.Run()
		add := exec.Command("git", "add", "-A")
		add.Dir = dst
		_ = add.Run()
		commit := exec.Command("git", "-c", "user.name=rf", "-c", "user.email=rf@local", "commit", "--allow-empty", "-q", "-m", "rf-base")
		commit.Dir = dst
		_ = commit.Run()
		apply := exec.Command("git", "apply", "--whitespace=nowarn", patchPath)
		apply.Dir = dst
		out, err := apply.CombinedOutput()
		if err == nil {
			return dst, nil
		}
		_ = out
	}
	// Fall back to patch(1) if available
	if _, err := exec.LookPath("patch"); err == nil {
		p := exec.Command("patch", "-p1", "-i", patchPath)
		p.Dir = dst
		if out, err := p.CombinedOutput(); err == nil {
			return dst, nil
		} else {
			return "", fmt.Errorf("patch -p1 failed: %s", strings.TrimSpace(string(out)))
		}
	}
	return "", errors.New("no git or patch binary available to apply diff")
}

// copyTree is a small helper duplicated here to avoid an import cycle.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(p); err == nil {
			mode = info.Mode().Perm()
		}
		return os.WriteFile(target, b, mode)
	})
}

// fingerprintFor recomputes a fingerprint from an outcome using a fresh
// log scan. Used to detect whether before/after replays land in the same
// "bucket".
func fingerprintFor(c interface{}, o replay.Outcome, capDir string) string {
	scan, _ := mergedScanFromCapDir(capDir)
	combined := scan.StackTop
	if combined == "" && o.Stderr != "" {
		ll := strings.Split(o.Stderr, "\n")
		if len(ll) > 0 {
			combined = strings.TrimSpace(ll[len(ll)-1])
		}
	}
	return fingerprint.Compute(fingerprint.Input{
		ExitCode: o.ExitCode, Step: "verify-patch", Command: "",
		ErrorClass: scan.ErrorClass, StackTopFrame: combined,
	})
}

// mergedScanFromCapDir scans the redacted logs already in the capsule
// directory; returns an empty scan if none exist.
func mergedScanFromCapDir(capDir string) (parsers.LogScan, parsers.Suites) {
	patterns := parsers.DefaultPatterns()
	logsDir := filepath.Join(capDir, "logs")
	merged := parsers.LogScan{}
	if _, err := os.Stat(logsDir); err == nil {
		_ = filepath.WalkDir(logsDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			s, err := parsers.ScanLog(f, patterns)
			if err == nil {
				merged.NetworkHits = append(merged.NetworkHits, s.NetworkHits...)
				if merged.ErrorClass == "" {
					merged.ErrorClass = s.ErrorClass
				}
				if merged.StackTop == "" {
					merged.StackTop = s.StackTop
				}
			}
			return nil
		})
	}
	return merged, nil
}

// suppress unused warnings if dependencies change; diagnose is included to
// keep the package easily extendable for future heuristic ranking.
var _ = diagnose.CatUnknown
