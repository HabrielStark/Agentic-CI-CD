// Package flake implements the flake-detection rerun loop described in
// FR-017. It supports "controlled variations" by toggling network access,
// memory limits, ordering and seed values across runs.
package flake

import (
	"context"
	"errors"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/logx"
	"github.com/reproforge/reproforge/internal/replay"
)

// Options configure the flake runner.
type Options struct {
	Target      string
	Runs        int
	Mode        string
	Network     string
	VariateNetwork bool
	VariateSeed bool
	MemoryMB    int
	CPUs        float64
	Image       string
	Runtime     string
	TimeoutSec  int
	EnvAllowlist []string
	ExtraEnv    []string
	WorkDir     string
}

// Result is one rerun outcome.
type Result struct {
	Index    int           `json:"index"`
	ExitCode int           `json:"exitCode"`
	Network  string        `json:"network"`
	Seed     string        `json:"seed,omitempty"`
	Duration time.Duration `json:"duration"`
	Status   string        `json:"status"` // pass | fail | skip | error
}

// Summary aggregates rerun outcomes.
type Summary struct {
	Target  string   `json:"target"`
	Runs    int      `json:"runs"`
	Passed  int      `json:"passed"`
	Failed  int      `json:"failed"`
	Skipped int      `json:"skipped"`
	Errored int      `json:"errored"`
	Mode    string   `json:"mode"`
	Outcomes []Result `json:"outcomes"`
}

// IsFlaky returns true when both pass and fail outcomes were observed.
func (s Summary) IsFlaky() bool { return s.Passed > 0 && s.Failed > 0 }

// Runner orchestrates reruns.
type Runner struct {
	Engine  *replay.Engine
	Logger  *logx.Logger
}

// New constructs a runner with the default replay engine.
func New(l *logx.Logger) *Runner {
	return &Runner{Engine: replay.NewEngine(l), Logger: l}
}

// Run performs N reruns.
func (r *Runner) Run(ctx context.Context, c *capsule.Capsule, capsuleDir, sourceDir string, opts Options) (Summary, error) {
	if opts.Runs <= 0 {
		opts.Runs = 10
	}
	if opts.Mode == "" {
		opts.Mode = replay.ModeFailedStep
	}
	if opts.Network == "" {
		opts.Network = replay.NetworkAllow
	}
	if r.Engine == nil {
		r.Engine = replay.NewEngine(r.Logger)
	}
	sum := Summary{Target: opts.Target, Mode: opts.Mode, Runs: opts.Runs}
	for i := 0; i < opts.Runs; i++ {
		network := opts.Network
		if opts.VariateNetwork && i%2 == 1 {
			if network == replay.NetworkAllow {
				network = replay.NetworkDeny
			} else {
				network = replay.NetworkAllow
			}
		}
		extraEnv := append([]string(nil), opts.ExtraEnv...)
		seed := ""
		if opts.VariateSeed {
			seed = nowSeed(i)
			extraEnv = append(extraEnv, "REPROFORGE_SEED="+seed)
			extraEnv = append(extraEnv, "PYTHONHASHSEED="+seed)
		}
		o := replay.Options{
			Mode:    opts.Mode,
			Network: network,
			MemoryMB: opts.MemoryMB,
			CPUs:    opts.CPUs,
			Image:   opts.Image,
			Runtime: opts.Runtime,
			TimeoutSec: opts.TimeoutSec,
			EnvAllowlist: opts.EnvAllowlist,
			ExtraEnv: extraEnv,
		}
		if c == nil {
			return sum, errors.New("capsule is nil")
		}
		out, err := r.Engine.Run(ctx, c, capsuleDir, sourceDir, o)
		res := Result{
			Index:    i,
			ExitCode: out.ExitCode,
			Network:  network,
			Seed:     seed,
			Duration: out.Duration,
		}
		switch {
		case err != nil:
			res.Status = "error"
			sum.Errored++
		case out.ExitCode == 0:
			res.Status = "pass"
			sum.Passed++
		default:
			res.Status = "fail"
			sum.Failed++
		}
		sum.Outcomes = append(sum.Outcomes, res)
		if r.Logger != nil {
			r.Logger.Info("flake run", "i", i, "status", res.Status, "exit", res.ExitCode, "network", network)
		}
	}
	return sum, nil
}

func nowSeed(i int) string {
	t := time.Now().UnixNano()
	const mix uint64 = 0x9E3779B97F4A7C15
	mixed := uint64(t) ^ (mix * uint64(i+1))
	v := int64(mixed % 999_999_999)
	if v < 0 {
		v = -v
	}
	return itoa64(v)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	bp := len(b)
	for n > 0 {
		bp--
		b[bp] = byte('0' + n%10)
		n /= 10
	}
	return string(b[bp:])
}
