// Package diagnose contains the rule-based failure classifier described in
// the SRS section 9.
package diagnose

import (
	"sort"
	"strings"

	"github.com/reproforge/reproforge/internal/parsers"
)

// Category names. Keep in sync with SRS section 6.1 / 9.
const (
	CatCodeOrTest         = "code_or_test_failure"
	CatFlaky              = "flaky_test"
	CatNetwork            = "network_issue"
	CatDependency         = "dependency_resolution"
	CatMissingSecret      = "missing_secret"
	CatRunnerMismatch     = "runner_mismatch"
	CatTimeout            = "timeout"
	CatOOM                = "oom"
	CatPermission         = "permission"
	CatWorkflowConfig     = "workflow_config"
	CatChecksum           = "checksum_mismatch"
	CatUnknown            = "unknown"
)

// Confidence rating for a diagnosis (0..1).
type Confidence float64

// Diagnosis is the structured output of classification.
type Diagnosis struct {
	Category    string     `json:"category"`
	Confidence  float64    `json:"confidence"`
	Evidence    []string   `json:"evidence"`
	NextActions []string   `json:"nextActions"`
	Commands    []string   `json:"commands,omitempty"`
	Tests       []string   `json:"failingTests,omitempty"`
	Notes       []string   `json:"notes,omitempty"`
	ErrorClass  string     `json:"errorClass,omitempty"`
	StackTop    string     `json:"stackTop,omitempty"`
	ExitCode    int        `json:"exitCode"`
	Step        string     `json:"step,omitempty"`
	Command     string     `json:"command,omitempty"`
}

// Input bundles all the signals we have for diagnosis.
type Input struct {
	LogScan       parsers.LogScan
	Suites        parsers.Suites
	WorkflowYAML  string
	JobName       string
	StepName      string
	Command       string
	WorkflowEnvs  []string // env names referenced in workflow (e.g. "secrets.X" → "secrets.X")
	JobLabels     []string
	OS            string
	Arch          string
	Provider      string
}

// Classify runs the rule-based classifier and returns a diagnosis.
//
// Classification ordering: we evaluate categories from most-specific to most-
// generic and pick the highest-scoring one. Each rule contributes confidence
// based on how many distinct signals fired.
func Classify(in Input) Diagnosis {
	scan := in.LogScan
	d := Diagnosis{
		ExitCode: scan.ExitCode,
		Step:     in.StepName,
		Command:  in.Command,
		ErrorClass: scan.ErrorClass,
		StackTop:   scan.StackTop,
	}

	type score struct {
		cat        string
		confidence float64
		evidence   []string
		actions    []string
		commands   []string
	}
	var scored []score

	// Workflow config / YAML expression problems.
	if len(scan.YAMLExpr) > 0 {
		scored = append(scored, score{
			cat: CatWorkflowConfig, confidence: 0.85,
			evidence: scan.YAMLExpr,
			actions: []string{
				"Inspect the workflow YAML for invalid expressions or unknown context values.",
				"Compare the failing event payload to expected workflow inputs.",
			},
		})
	}

	// Missing secret / env.
	if len(scan.MissingEnv) > 0 {
		scored = append(scored, score{
			cat: CatMissingSecret, confidence: 0.78,
			evidence: scan.MissingEnv,
			actions: []string{
				"Confirm the listed environment variable or secret is present in the job's environment.",
				"If the secret is required, replay locally with --env KEY=value or set it via .reproforge/local-secrets (never committed).",
			},
		})
	}

	// Permission errors.
	if len(scan.PermHits) > 0 {
		scored = append(scored, score{
			cat: CatPermission, confidence: 0.72,
			evidence: scan.PermHits,
			actions: []string{
				"Verify chmod/chown on relevant paths and that workflow `permissions` allow the required scopes.",
			},
		})
	}

	// OOM.
	if len(scan.OOMHits) > 0 {
		scored = append(scored, score{
			cat: CatOOM, confidence: 0.85,
			evidence: scan.OOMHits,
			actions: []string{
				"Re-run with increased memory or a smaller test shard (--memory 4g).",
				"Identify the offending allocator with --profile during replay.",
			},
		})
	}

	// Timeout.
	if len(scan.TimeoutHits) > 0 {
		scored = append(scored, score{
			cat: CatTimeout, confidence: 0.78,
			evidence: scan.TimeoutHits,
			actions: []string{
				"Increase test/job timeouts or add retries with backoff for known-slow operations.",
				"Profile the failing target during replay to spot regressions.",
			},
		})
	}

	// Network.
	if len(scan.NetworkHits) > 0 || len(scan.DNSHits) > 0 || len(scan.TLSHits) > 0 {
		ev := append([]string{}, scan.NetworkHits...)
		ev = append(ev, scan.DNSHits...)
		ev = append(ev, scan.TLSHits...)
		scored = append(scored, score{
			cat: CatNetwork, confidence: 0.82,
			evidence: ev,
			actions: []string{
				"Replay with --network=allow and --network=deny to confirm network dependency.",
				"Pin or cache the offending dependency to avoid registry/network flakiness.",
			},
			commands: []string{
				"reproforge replay <capsule> --mode failed-step --network=deny",
			},
		})
	}

	// Checksum / integrity.
	if len(scan.ChecksumHits) > 0 {
		scored = append(scored, score{
			cat: CatChecksum, confidence: 0.83,
			evidence: scan.ChecksumHits,
			actions: []string{
				"Verify the lockfile hash. Regenerate it from a clean checkout and re-pin.",
				"If a registry is mirroring corrupted artifacts, switch to upstream and clear caches.",
			},
		})
	}

	// Dependency resolution.
	if len(scan.DepHits) > 0 {
		scored = append(scored, score{
			cat: CatDependency, confidence: 0.80,
			evidence: scan.DepHits,
			actions: []string{
				"Inspect lockfile and registry response. Validate transitive constraints.",
				"Replay with --mode dependency-install to isolate.",
			},
			commands: []string{
				"reproforge replay <capsule> --mode dependency-install",
			},
		})
	}

	// Code or test failure (Python/Java/Node/Go).
	failing := in.Suites.Failed()
	if len(failing) > 0 {
		ev := []string{}
		tests := []string{}
		for _, tc := range failing {
			tests = append(tests, tc.FullName())
			if tc.Message != "" {
				ev = append(ev, tc.FullName()+": "+truncate(tc.Message, 200))
			}
		}
		scored = append(scored, score{
			cat: CatCodeOrTest, confidence: 0.86,
			evidence: ev,
			actions: []string{
				"Reproduce locally with `reproforge replay <capsule> --mode test-only`.",
				"Inspect the failing test bodies and assertions.",
			},
		})
		d.Tests = tests
	}

	// Stack-only failure with no tests.
	if len(failing) == 0 && (scan.ErrorClass != "" || scan.StackTop != "") {
		scored = append(scored, score{
			cat: CatCodeOrTest, confidence: 0.55,
			evidence: scan.StackBlocks,
			actions: []string{
				"Inspect the top of the stack and the failing command.",
				"Replay with --mode failed-step.",
			},
		})
	}

	// Runner mismatch / missing file (best effort).
	if len(scan.MissingFile) > 0 {
		ev := scan.MissingFile
		scored = append(scored, score{
			cat: CatRunnerMismatch, confidence: 0.62,
			evidence: ev,
			actions: []string{
				"Compare local replay image with the GitHub-hosted runner image (ubuntu-latest variants).",
				"Install missing OS packages or adjust paths.",
			},
		})
	}

	if len(scored) == 0 {
		d.Category = CatUnknown
		d.Confidence = 0.2
		d.Evidence = scan.RawTail
		d.NextActions = []string{
			"Inspect the raw log and replay the failed step locally.",
			"Submit the capsule to the maintainers of the action/test framework.",
		}
		return d
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].confidence > scored[j].confidence })

	// adjust top confidence if multiple categories matched (signal redundancy).
	top := scored[0]
	if len(scored) > 1 && scored[1].confidence > 0.5 {
		// if signals overlap we slightly reduce confidence; keep above 0.5.
		top.confidence = (top.confidence*2 + scored[1].confidence) / 3
	}
	d.Category = top.cat
	d.Confidence = round2(top.confidence)
	d.Evidence = dedupShort(top.evidence, 6)
	d.NextActions = top.actions
	d.Commands = top.commands
	if d.Category == CatCodeOrTest && len(d.Tests) == 0 && scan.StackTop != "" {
		d.StackTop = scan.StackTop
	}
	for _, s := range scored[1:] {
		d.Notes = append(d.Notes, "candidate: "+s.cat)
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func dedupShort(in []string, n int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, n)
	for _, s := range in {
		t := truncate(s, 240)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) >= n {
			break
		}
	}
	return out
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
