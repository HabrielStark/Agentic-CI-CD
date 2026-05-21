// Package workflow parses GitHub Actions workflow YAML to extract structured
// information: jobs, steps, their run commands, environment variables, and
// conditions. This allows ReproForge to populate capsule.Failure.Command
// with the real shell command instead of leaving it blank.
package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Workflow is a parsed GitHub Actions workflow.
type Workflow struct {
	Name        string            `yaml:"name"`
	On          interface{}       `yaml:"on"`
	Env         map[string]string `yaml:"env"`
	Permissions interface{}       `yaml:"permissions"`
	Jobs        map[string]Job    `yaml:"jobs"`
}

// Job is a single workflow job.
type Job struct {
	Name        string                 `yaml:"name"`
	RunsOn      interface{}            `yaml:"runs-on"`
	Env         map[string]string      `yaml:"env"`
	If          string                 `yaml:"if"`
	Steps       []Step                 `yaml:"steps"`
	Services    map[string]interface{} `yaml:"services"`
	Container   interface{}            `yaml:"container"`
	TimeoutMin  int                    `yaml:"timeout-minutes"`
	Strategy    interface{}            `yaml:"strategy"`
}

// Step is a workflow step.
type Step struct {
	ID              string            `yaml:"id"`
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	Shell           string            `yaml:"shell"`
	WorkingDir      string            `yaml:"working-directory"`
	Env             map[string]string `yaml:"env"`
	With            map[string]string `yaml:"with"`
	If              string            `yaml:"if"`
	TimeoutMin      int               `yaml:"timeout-minutes"`
	ContinueOnError interface{}       `yaml:"continue-on-error"`
}

// Parse parses a GitHub Actions workflow YAML body.
func Parse(body []byte) (*Workflow, error) {
	var w Workflow
	if err := yaml.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("workflow parse: %w", err)
	}
	return &w, nil
}

// FindStep searches all jobs for a step matching the given name (case-insensitive
// prefix match, to handle GitHub's name-truncation in logs). Returns the job name,
// step, and whether it was found.
func (w *Workflow) FindStep(stepName string) (jobName string, step *Step, found bool) {
	if w == nil || stepName == "" {
		return "", nil, false
	}
	lower := strings.ToLower(strings.TrimSpace(stepName))
	for jn, job := range w.Jobs {
		for i := range job.Steps {
			s := &job.Steps[i]
			candidate := strings.ToLower(strings.TrimSpace(s.Name))
			if candidate == "" {
				// skip unnamed steps for name-based matching
			} else if candidate == lower || strings.HasPrefix(candidate, lower) || strings.HasPrefix(lower, candidate) {
				return jn, s, true
			}
			// GitHub sometimes prefixes with "Run " for unnamed steps
			if s.Run != "" && strings.HasPrefix(lower, "run ") {
				cmdFirst := strings.ToLower(strings.SplitN(s.Run, "\n", 2)[0])
				if strings.Contains(lower, cmdFirst[:min(len(cmdFirst), 30)]) {
					return jn, s, true
				}
			}
		}
	}
	return "", nil, false
}

// FindStepByNumber finds by 1-based step number within a job.
func (w *Workflow) FindStepByNumber(jobName string, number int) *Step {
	job, ok := w.Jobs[jobName]
	if !ok {
		return nil
	}
	idx := number - 1
	if idx < 0 || idx >= len(job.Steps) {
		return nil
	}
	return &job.Steps[idx]
}

// ExtractCommand returns the shell command for a step. For `uses:` steps it
// returns an empty string. For `run:` steps it returns the full multi-line
// script. The shell field is prepended as a comment for context.
func (s *Step) ExtractCommand() string {
	if s == nil || s.Run == "" {
		return ""
	}
	return strings.TrimSpace(s.Run)
}

// ExtractEnv merges workflow-level, job-level and step-level env into one map.
// Later levels override earlier ones (step > job > workflow).
func (w *Workflow) ExtractEnv(jobName string, step *Step) map[string]string {
	merged := map[string]string{}
	for k, v := range w.Env {
		merged[k] = v
	}
	if job, ok := w.Jobs[jobName]; ok {
		for k, v := range job.Env {
			merged[k] = v
		}
	}
	if step != nil {
		for k, v := range step.Env {
			merged[k] = v
		}
	}
	return merged
}

// RunnerLabels extracts the runs-on field as a slice of strings.
func (j Job) RunnerLabels() []string {
	switch v := j.RunsOn.(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// AllRunCommands returns every run: block across all jobs+steps.
func (w *Workflow) AllRunCommands() []string {
	var out []string
	for _, job := range w.Jobs {
		for _, s := range job.Steps {
			if s.Run != "" {
				out = append(out, s.Run)
			}
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
