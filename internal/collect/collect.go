// Package collect orchestrates the run-collection pipeline used by the
// `reproforge collect` and `from-run` commands.
//
// It glues together the GitHub provider, redaction engine, log/test parsers,
// fingerprint computation, capsule builder and (optionally) the diagnoser.
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

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/fingerprint"
	"github.com/reproforge/reproforge/internal/github"
	"github.com/reproforge/reproforge/internal/logx"
	"github.com/reproforge/reproforge/internal/parsers"
	"github.com/reproforge/reproforge/internal/redaction"
	"github.com/reproforge/reproforge/internal/version"
)

// Options control the collection pipeline.
type Options struct {
	OutputDir       string
	IncludeArtifacts bool
	Provider        string // "github_actions" only for now
	Token           string
	Diagnose        bool
	Logger          *logx.Logger
}

// Result is the outcome of a collection.
type Result struct {
	Capsule        *capsule.Capsule
	CapsulePath    string
	OutputDir      string
	Diagnosis      *diagnose.Diagnosis
	FailingTests   []string
	JobLogs        map[int64][]byte // job id -> redacted bytes
	WorkflowYAML   []byte
	Run            *github.Run
	Job            *github.Job
}

// FromRun runs the full pipeline for a GitHub Actions run URL or refspec.
func FromRun(ctx context.Context, ref github.RunRef, opts Options) (*Result, error) {
	if opts.Logger == nil {
		opts.Logger = logx.Default()
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "./reproforge-out"
	}
	gh := newClientForCollect(opts.Token)

	opts.Logger.Info("collect: fetch run", "repo", ref.FullRepo(), "run", ref.RunID)
	run, err := gh.GetRun(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	jobs, err := gh.ListJobs(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	failedJob, failedStep := pickFailedJob(jobs, ref.JobID)
	if failedJob == nil {
		return nil, errors.New("no failed job in run")
	}

	opts.Logger.Info("collect: download workflow")
	workflowYAML, err := gh.DownloadWorkflow(ctx, ref, run)
	if err != nil {
		opts.Logger.Warn("could not fetch workflow yaml", "err", err)
		workflowYAML = []byte{}
	}

	opts.Logger.Info("collect: download logs")
	logsZip, err := gh.DownloadRunLogs(ctx, ref)
	if err != nil {
		// fall back to per-job logs
		opts.Logger.Warn("run logs zip failed; trying per-job logs", "err", err)
		jobBytes, jerr := gh.DownloadJobLogs(ctx, ref, failedJob.ID)
		if jerr != nil {
			return nil, fmt.Errorf("download logs: %w", jerr)
		}
		logsZip = map[string][]byte{
			fmt.Sprintf("%s.txt", failedJob.Name): jobBytes,
		}
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

	// parse failing tests from any junit-xml looking artifacts later if requested
	var suites parsers.Suites
	if opts.IncludeArtifacts {
		artifacts, err := gh.ListArtifacts(ctx, ref)
		if err != nil {
			opts.Logger.Warn("list artifacts failed", "err", err)
		}
		for _, a := range artifacts {
			if a.Expired || a.SizeInBytes > 50*1024*1024 {
				continue
			}
			zipBytes, err := gh.DownloadArtifact(ctx, ref, a.ID)
			if err != nil {
				opts.Logger.Warn("artifact download failed", "name", a.Name, "err", err)
				continue
			}
			suites = append(suites, parseArtifactJUnit(zipBytes)...)
			// store artifact
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
		Step: stepName(failedStep), FailingTests: failingNames,
		ErrorClass: scan.ErrorClass, StackTopFrame: scan.StackTop,
		Provider: "github_actions",
		OS: failedJob.RunnerOS,
	})

	cap_ := &capsule.Capsule{
		Schema:    capsule.SchemaV1,
		CreatedAt: time.Now().UTC(),
		Generator: "reproforge " + version.Version,
		Provider:  capsule.ProviderGitHub,
		Repo:      ref.FullRepo(),
		Commit:    run.HeadSHA,
		Branch:    run.HeadBranch,
		Workflow:  filepath.Base(run.Path),
		Job:       failedJob.Name,
		JobID:     failedJob.ID,
		RunID:     ref.RunID,
		RunURL:    run.URL,
		Runner: capsule.Runner{
			OS:     orDefault(failedJob.RunnerOS, "Linux"),
			Arch:   "x86_64",
			Image:  "",
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

	// Optionally diagnose now and embed a summary in the capsule.
	if opts.Diagnose {
		d := diagnose.Classify(diagnose.Input{
			LogScan: scan, Suites: suites,
			WorkflowYAML: string(workflowYAML),
			JobName: failedJob.Name, StepName: stepName(failedStep),
			Command: cmd, OS: failedJob.RunnerOS, Provider: "github_actions",
		})
		cap_.Diagnosis = &capsule.DiagnosisSummary{
			Category: d.Category, Confidence: d.Confidence, Evidence: d.Evidence,
		}
	}

	// Materialise capsule on disk under OutputDir/capsule/
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
		out := filepath.Join(capDir, "workflow", filepath.Base(run.Path))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out, workflowYAML, 0o644); err != nil {
			return nil, err
		}
	}
	// redaction report
	rep := r.Report(sortedKeys(redactedLogs), allHits)
	repBytes, _ := jsonIndent(rep)
	if err := os.MkdirAll(filepath.Join(capDir, "redaction"), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(capDir, "redaction", "redaction-report.json"), repBytes, 0o644); err != nil {
		return nil, err
	}

	if err := cap_.Validate(); err != nil {
		return nil, fmt.Errorf("capsule validate: %w", err)
	}

	res := &Result{
		Capsule: cap_, OutputDir: opts.OutputDir,
		Run: run, Job: failedJob, WorkflowYAML: workflowYAML,
		FailingTests: failingNames,
		JobLogs: map[int64][]byte{failedJob.ID: redactedLogs[focalPath]},
	}
	if opts.Diagnose && cap_.Diagnosis != nil {
		// expose full diagnosis
		d := diagnose.Classify(diagnose.Input{
			LogScan: scan, Suites: suites,
			WorkflowYAML: string(workflowYAML),
			JobName: failedJob.Name, StepName: stepName(failedStep),
			Command: cmd, OS: failedJob.RunnerOS, Provider: "github_actions",
		})
		res.Diagnosis = &d
	}
	return res, nil
}

// pickFailedJob returns the first failing job (or the requested one).
func pickFailedJob(jobs []github.Job, want int64) (*github.Job, *github.Step) {
	for i := range jobs {
		j := &jobs[i]
		if want != 0 && j.ID != want {
			continue
		}
		if !strings.EqualFold(j.Conclusion, "failure") &&
			!strings.EqualFold(j.Conclusion, "timed_out") &&
			!strings.EqualFold(j.Conclusion, "cancelled") {
			continue
		}
		for k := range j.Steps {
			st := &j.Steps[k]
			if strings.EqualFold(st.Conclusion, "failure") || strings.EqualFold(st.Conclusion, "timed_out") {
				return j, st
			}
		}
		// fall back to the last step if none marked failure
		if n := len(j.Steps); n > 0 {
			return j, &j.Steps[n-1]
		}
		return j, nil
	}
	if want == 0 && len(jobs) > 0 {
		return &jobs[0], nil
	}
	return nil, nil
}

func pickCommand(j *github.Job, st *github.Step) string {
	if st == nil || j == nil {
		return ""
	}
	// We don't have raw command, but the step name is informative.
	// Future: parse the workflow YAML and resolve `run:` for the matching step.
	return ""
}

func stepName(st *github.Step) string {
	if st == nil {
		return "unknown"
	}
	return st.Name
}

func defaultExit(j *github.Job, st *github.Step) int {
	if st != nil && strings.EqualFold(st.Conclusion, "failure") {
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

// scanFocalLogs picks a single representative log file for analysis. It
// prefers files whose name contains the failed job name; otherwise falls back
// to the largest file.
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

// parseArtifactJUnit unzips an artifact and tries to parse JUnit XML inside.
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

// jsonIndent is a thin helper to keep imports tidy.
func jsonIndent(v any) ([]byte, error) {
	return jsonMarshalIndent(v)
}



// newClientForCollect is indirected to make integration testing possible.
var newClientForCollect = func(token string) *github.Client {
	return github.NewClient(token)
}
