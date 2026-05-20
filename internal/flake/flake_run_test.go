package flake

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/logx"
	"github.com/reproforge/reproforge/internal/replay"
)

// fakeRuntimeBin returns the path of a docker stub that exits with $RFTEST_RC
// on every "run" invocation. Build a small bash script for the duration of
// the test.
func fakeRuntimeBin(t *testing.T, rc int) string {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake")
	body := `#!/usr/bin/env bash
case "$1" in
  build) exit 0 ;;
  stats) echo '{"CPUPerc":"0%","MemUsage":"1MiB / 1MiB","MemPerc":"0%","NetIO":"0B / 0B","BlockIO":"0B / 0B"}' ;;
  run)
    exit ` + itoa(rc) + `
    ;;
  *)
    exit 0 ;;
esac
`
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	bp := len(b)
	for i > 0 {
		bp--
		b[bp] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		bp--
		b[bp] = '-'
	}
	return string(b[bp:])
}

func sampleCap(t *testing.T) (string, *capsule.Capsule) {
	dir := t.TempDir()
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, Provider: capsule.ProviderGitHub,
		Repo: "o/r", Commit: "abc", Workflow: "ci.yml", Job: "build",
		Runner:    capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Replay:    capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "true", ExitCode: 1,
			Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
	}
	eng := replay.NewEngine(logx.New(os.Stderr, logx.LevelError, false))
	if err := eng.Generate(c, dir, replay.Options{}); err != nil {
		t.Fatal(err)
	}
	return dir, c
}

func TestRunnerRun_AlwaysFails(t *testing.T) {
	dir, c := sampleCap(t)
	bin := fakeRuntimeBin(t, 1)
	r := New(logx.New(os.Stderr, logx.LevelError, false))
	sum, err := r.Run(context.Background(), c, dir, dir, Options{
		Runs: 3, Mode: replay.ModeFailedStep, Network: replay.NetworkAllow,
		Runtime: bin, TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Failed != 3 || sum.Passed != 0 {
		t.Fatalf("expected 3 fails, got %+v", sum)
	}
	if sum.IsFlaky() {
		t.Fatalf("not flaky if all fail: %+v", sum)
	}
}

func TestRunnerRun_AlwaysPasses(t *testing.T) {
	dir, c := sampleCap(t)
	bin := fakeRuntimeBin(t, 0)
	r := New(logx.New(os.Stderr, logx.LevelError, false))
	sum, err := r.Run(context.Background(), c, dir, dir, Options{
		Runs: 4, Runtime: bin, TimeoutSec: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Passed != 4 || sum.Failed != 0 {
		t.Fatalf("expected 4 passes, got %+v", sum)
	}
	if sum.IsFlaky() {
		t.Fatalf("not flaky if all pass: %+v", sum)
	}
}
