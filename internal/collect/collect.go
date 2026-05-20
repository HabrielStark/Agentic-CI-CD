// Package collect orchestrates the run-collection pipeline used by the
// `reproforge collect` and `from-run` commands. It is provider-agnostic via
// internal/provider; the GitHub Actions adapter is the default.
package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/reproforge/reproforge/internal/cache"
	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/fingerprint"
	_ "github.com/reproforge/reproforge/internal/github" // register provider
	_ "github.com/reproforge/reproforge/internal/gitlab" // register provider
	"github.com/reproforge/reproforge/internal/logx"
	"github.com/reproforge/reproforge/internal/parsers"
	"github.com/reproforge/reproforge/internal/provider"
	"github.com/reproforge/reproforge/internal/redaction"
	"github.com/reproforge/reproforge/internal/version"
	"github.com/reproforge/reproforge/internal/wflint"
)

// Options control the collection pipeline.
type Options struct {
	OutputDir        string
	IncludeArtifacts bool
	Provider         string // "github_actions" by default
	Token            string
	Diagnose         bool
	WorkflowLint     bool
	Logger           *logx.Logger
}

// Result is the outcome of a collection.
type Result struct {
	Capsule      *capsule.Capsule
	CapsulePath  string
	OutputDir    string
	Diagnosis    *diagnose.Diagnosis
	FailingTests []string
	JobLogs      map[int64][]byte // job id -> redacted bytes
	WorkflowYAML []byte
	Run          *provider.Run
	Job          *provider.Job
	LintFindings []wflint.Finding
}

// FromRun runs the full pipeline for a given provider RunRef.
func FromRun(ctx context.Context, ref provider.RunRef, opts Options) (*Result, error) {
	if opts.Logger == nil {
		opts.Logger = logx.Default()
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "./reproforge-out"
	}
	prov, err := buildProvider(opts.Provider, opts.Token)
	if err != nil {
		return nil, err
	}

	opts.Logger.Info("collect: fetch run", "repo", ref.FullRepo(), "run", ref.RunID, "provider", prov.Name())
	run, err := prov.FetchRun(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("fetch run: %w", err)
	}
	failedJob, failedStep := provider.PickFailedJob(run.Jobs, ref.JobID)
	if failedJob == nil {
		return nil, errors.New("no failed job in run")
	}

	opts.Logger.Info("collect: download workflow")
	workflowYAML, err := prov.DownloadWorkflowYAML(ctx, ref, run)
	if err != nil {
		opts.Logger.Warn("could not fetch workflow yaml", "err", err)
		workflowYAML = []byte{}
	}

	opts.Logger.Info("collect: download logs")
	logsZip, err := prov.DownloadLogs(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("download logs: %w", err)
	}

	// redact logs
	r := redaction.NewDefault()
	r.AddDenylist(redaction.CommonSecretEnvNames())
	allHits := []redaction.Hit{}
	redactedLogs := map[string][]byte{}
	for path, raw := range logsZip {
		out, hits := r.RedactString(string(raw))
		for i := range hits {
			hits[i].File = path
		}
		allHits = append(allHits, hits...)
		redactedLogs[path] = []byte(out)
	}

	// pick the focal log (the failed job's log)
	scan, focalPath := scanFocalLogs(redactedLogs, failedJob.Name)

	// optionally lint workflow
	var lintFindings []wflint.Finding
	if opts.WorkflowLint && len(workflowYAML) > 0 {
		lintFindings = wflint.Lint(workflowYAML)
	}

	// parse failing tests from artifacts when requested
	var suites parsers.Suites
	if opts.IncludeArtifacts {
		artifacts, aerr := prov.ListArtifacts(ctx, ref)
		if aerr != nil {
			opts.Logger.Warn("list artifacts failed", "err", aerr)
		}
		for _, a := range artifacts {
			if a.Expired || a.SizeInBytes > 50*1024*1024 {
				continue
			}
			zipBytes, derr := prov.DownloadArtifact(ctx, ref, a.ID)
			if derr != nil {
				opts.Logger.Warn("artifact download failed", "name", a.Name, "err", derr)
				continue
			}
			suites = append(suites, parseArtifactJUnit(zipBytes)...)
			outDir := filepath.Join(opts.OutputDir, "artifacts", a.Name)
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(outDir, "archive.zip"), zipBytes, 0o644); err != nil {
				return nil, err
			}
		}
	}

	failingTests := suites.Failed()
	failingNames := make([]string, 0, len(failingTests))
	for _, tc := range failingTests {
		failingNames = append(failingNames, tc.FullName())
	}

	cmd := pickCommand(failedJob, failedStep)
	fp := fingerprint.Compute(fingerprint.Input{
		Command: cmd, ExitCode: scan.ExitCode,
		Step:          stepName(failedStep),
		FailingTests:  failingNames,
		ErrorClass:    scan.ErrorClass,
		StackTopFrame: scan.StackTop,
		Provider:      prov.Name(),
		OS:            failedJob.RunnerOS,
	})

	cap_ := &capsule.Capsule{
		Schema:    capsule.SchemaV1,
		CreatedAt: time.Now().UTC(),
		Generator: "reproforge " + version.Version,
		Provider:  prov.Name(),
		Repo:      ref.FullRepo(),
		Commit:    run.HeadSHA,
		Branch:    run.HeadBranch,
		Workflow:  filepath.Base(run.Workflow),
		Job:       failedJob.Name,
		JobID:     failedJob.ID,
		RunID:     ref.RunID,
		RunURL:    run.URL,
		Runner: capsule.Runner{
			OS: orDefault(failedJob.RunnerOS, "Linux"),
			Arch: "x86_64",
			Image: "",
			Labels: failedJob.Labels,
		},
		Failure: capsule.Failure{
			Step:        stepName(failedStep),
			Command:     cmd,
			ExitCode:    nonZero(scan.ExitCode, defaultExit(failedJob, failedStep)),
			Tests:       failingNames,
			Fingerprint: fp,
		},
		Replay: capsule.Replay{
			Modes:   []string{capsule.ReplayFailedStep, capsule.ReplayTestOnly, capsule.ReplayDependencyInstall},
			Network: capsule.NetworkConfigurable,
		},
		Redaction: capsule.Redaction{
			Status: "passed", Rules: len(r.Rules()), Hits: len(allHits),
		},
	}

	if opts.Diagnose {
		d := diagnose.Classify(diagnose.Input{
			LogScan: scan, Suites: suites,
			WorkflowYAML: string(workflowYAML),
			JobName:      failedJob.Name, StepName: stepName(failedStep),
			Command: cmd, OS: failedJob.RunnerOS, Provider: prov.Name(),
		})
		cap_.Diagnosis = &capsule.DiagnosisSummary{
			Category: d.Category, Confidence: d.Confidence, Evidence: d.Evidence,
		}
	}

	capDir := filepath.Join(opts.OutputDir, "capsule")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		return nil, err
	}
	for path, body := range redactedLogs {
		out := filepath.Join(capDir, "logs", failedJob.Name, sanitizePath(path))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out, body, 0o644); err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(capDir, out)
		sum := sha256.Sum256(body)
		cap_.Logs = append(cap_.Logs, capsule.LogFile{
			Path: filepath.ToSlash(rel), Job: failedJob.Name, Step: stepName(failedStep),
			Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
		})
	}
	if len(workflowYAML) > 0 {
		out := filepath.Join(capDir, "workflow", filepath.Base(run.Workflow))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out, workflowYAML, 0o644); err != nil {
			return nil, err
		}
	}
	if len(lintFindings) > 0 {
		body, _ := jsonIndent(lintFindings)
		out := filepath.Join(capDir, "workflow", "lint.json")
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		_ = os.WriteFile(out, body, 0o644)
	}
	rep := r.Report(sortedKeys(redactedLogs), allHits)
	repBytes, _ := jsonIndent(rep)
	if err := os.MkdirAll(filepath.Join(capDir, "redaction"), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(capDir, "redaction", "redaction-report.json"), repBytes, 0o644); err != nil {
		return nil, err
	}

	// FR-028: capture cache hint files (lockfiles) so replay can prime caches.
	if hints := cache.DetectHints(string(workflowYAML)); len(hints) > 0 {
		cdir := filepath.Join(capDir, "cache")
		_ = os.MkdirAll(cdir, 0o755)
		hintBytes, _ := jsonIndent(hints)
		_ = os.WriteFile(filepath.Join(cdir, "cache-hints.json"), hintBytes, 0o644)
	}

	if err := cap_.Validate(); err != nil {
		return nil, fmt.Errorf("capsule validate: %w", err)
	}

	res := &Result{
		Capsule: cap_, OutputDir: opts.OutputDir,
		Run: run, Job: failedJob, WorkflowYAML: workflowYAML,
		FailingTests: failingNames,
		JobLogs:      map[int64][]byte{failedJob.ID: redactedLogs[focalPath]},
		LintFindings: lintFindings,
	}
	if opts.Diagnose {
		d := diagnose.Classify(diagnose.Input{
			LogScan: scan, Suites: suites,
			WorkflowYAML: string(workflowYAML),
			JobName:      failedJob.Name, StepName: stepName(failedStep),
			Command: cmd, OS: failedJob.RunnerOS, Provider: prov.Name(),
		})
		res.Diagnosis = &d
	}
	return res, nil
}

func buildProvider(name, token string) (provider.Provider, error) {
	if name == "" {
		name = "github_actions"
	}
	if testProvider != nil {
		return testProvider, nil
	}
	f, err := provider.Get(name)
	if err != nil {
		return nil, err
	}
	return f(token), nil
}

func pickCommand(j *provider.Job, st *provider.Step) string {
	if st == nil || j == nil {
		return ""
	}
	return ""
}

func stepName(st *provider.Step) string {
	if st == nil {
		return "unknown"
	}
	return st.Name
}

func defaultExit(j *provider.Job, st *provider.Step) int {
	if st != nil && (strings.EqualFold(st.Conclusion, "failure") || strings.EqualFold(st.Conclusion, "failed")) {
		return 1
	}
	if j != nil && strings.EqualFold(j.Conclusion, "timed_out") {
		return 124
	}
	if j != nil && strings.EqualFold(j.Conclusion, "cancelled") {
		return 130
	}
	return 1
}

func nonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func sanitizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.ReplaceAll(p, "..", "_")
	p = strings.Trim(p, "/")
	return p
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func scanFocalLogs(logs map[string][]byte, jobName string) (parsers.LogScan, string) {
	patterns := parsers.DefaultPatterns()
	var bestPath string
	var bestScan parsers.LogScan
	bestScore := -1
	for path, raw := range logs {
		score := 0
		if strings.Contains(strings.ToLower(path), strings.ToLower(jobName)) {
			score += 100
		}
		score += len(raw) / 1024
		if score > bestScore {
			bestScore = score
			s, err := parsers.ScanLog(strings.NewReader(string(raw)), patterns)
			if err != nil {
				continue
			}
			bestScan, bestPath = s, path
		}
	}
	return bestScan, bestPath
}

func parseArtifactJUnit(zipBytes []byte) parsers.Suites {
	r, err := openZip(zipBytes)
	if err != nil {
		return nil
	}
	var out parsers.Suites
	for path, b := range r {
		lower := strings.ToLower(path)
		if !(strings.HasSuffix(lower, ".xml") && (strings.Contains(lower, "junit") || strings.Contains(lower, "test-results") || strings.Contains(lower, "surefire") || strings.Contains(lower, "TEST-"))) {
			continue
		}
		s, err := parsers.JUnitXML(strings.NewReader(string(b)))
		if err != nil {
			continue
		}
		out = append(out, s...)
	}
	return out
}

func jsonIndent(v any) ([]byte, error) {
	return jsonMarshalIndent(v)
}

// testProvider, when non-nil, replaces the registered provider lookup. Used
// by integration tests to inject a fake.
var testProvider provider.Provider
