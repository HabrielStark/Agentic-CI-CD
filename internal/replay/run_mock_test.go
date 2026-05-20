package replay

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/logx"
)

// fakeRuntime writes a tiny shell script that masquerades as
// `docker`/`podman`. It accepts `build` (returns 0 immediately) and
// `run ...<image>` (executes /bin/echo with the rest of the args).
func fakeRuntime(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("fakeRuntime requires bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakedocker")
	body := `#!/usr/bin/env bash
case "$1" in
  build)
    exit 0
    ;;
  run)
    shift
    # consume flags until we see the image name (no leading -)
    while [ $# -gt 0 ]; do
      case "$1" in
        -*)
          shift
          if [ "${1:-}" != "" ]; then
            shift || true
          fi
          ;;
        *)
          break
          ;;
      esac
    done
    # remaining args: <image> [cmd...]
    shift # image
    "$@"
    ;;
  stats)
    echo '{"CPUPerc":"3.0%","MemUsage":"10MiB / 100MiB","MemPerc":"10%","NetIO":"0B / 0B","BlockIO":"0B / 0B"}'
    ;;
  version)
    echo "fake-runtime"
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestEngine_RunReproducesFailure(t *testing.T) {
	// failedstep.sh will run "false" → exit 1; capsule expects exit 1.
	dir := t.TempDir()
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, Provider: capsule.ProviderGitHub,
		Repo: "o/r", Commit: "abc", Workflow: "ci.yml", Job: "build",
		Runner: capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "false", ExitCode: 1,
			Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
		Replay:    capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
	}
	eng := NewEngine(logx.New(os.Stderr, logx.LevelError, false))
	if err := eng.Generate(c, dir, Options{Mode: ModeFailedStep}); err != nil {
		t.Fatal(err)
	}
	bin := fakeRuntime(t)
	out, _ := eng.Run(context.Background(), c, dir, dir, Options{
		Mode:    ModeFailedStep,
		Network: NetworkAllow,
		Runtime: bin,
	})
	// We invoked `false` so exit code should be non-zero matching original.
	if out.ExitCode == 0 {
		t.Fatalf("expected non-zero exit, got %+v", out)
	}
	if !out.Reproduced {
		t.Fatalf("expected Reproduced=true, got %+v", out)
	}
	if out.Image == "" {
		t.Fatalf("expected image to be filled in: %+v", out)
	}
}

func TestEngine_RunDryRun(t *testing.T) {
	dir := t.TempDir()
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, Provider: capsule.ProviderGitHub,
		Repo: "o/r", Commit: "abc", Workflow: "ci.yml", Job: "build",
		Runner:    capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Replay:    capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "true", ExitCode: 0,
			Fingerprint: "sha256:" + strings.Repeat("b", 64),
		},
	}
	eng := NewEngine(nil)
	if err := eng.Generate(c, dir, Options{}); err != nil {
		t.Fatal(err)
	}
	out, err := eng.Run(context.Background(), c, dir, dir, Options{DryRun: true, Runtime: fakeRuntime(t)})
	if err != nil {
		t.Fatal(err)
	}
	if out.Reason != "dry-run" {
		t.Fatalf("expected dry-run reason, got %+v", out)
	}
}
