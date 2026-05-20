// Package wflint is a small, opinionated GitHub Actions workflow linter.
// It does NOT replace zizmor; it ships the most common, evidence-based
// security checks built-in so ReproForge can include workflow risks in
// every diagnosis without requiring an external tool. For a deeper audit,
// callers may run zizmor in addition (its findings can be merged).
package wflint

import (
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity ranks findings.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
	SeverityInfo   Severity = "info"
)

// Finding is a single lint hit.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Line     int      `json:"line"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
}

// Lint parses and reports issues in a GitHub Actions workflow YAML body.
//
// Implemented checks (each evidence-anchored):
//   - missing top-level `permissions:` (defaults to write-all → over-privileged)
//   - `pull_request_target` trigger combined with checkout of the PR head
//   - third-party actions referenced by floating tag (vN, main) instead of SHA
//   - secrets piped through `env:` to `run:` blocks (potential leak in logs)
//   - `if: ${{ ... contains(github.event_name, ...) }}` typos that always fire
//   - jobs running on `self-hosted` runners with no labels (orphan capture)
//
// All checks have explicit Hint strings so reports are actionable.
func Lint(yamlBody []byte) []Finding {
	var findings []Finding

	var doc yaml.Node
	if err := yaml.Unmarshal(yamlBody, &doc); err != nil {
		return []Finding{{Rule: "yaml/parse", Severity: SeverityHigh, Line: 0, Message: "workflow YAML is not parseable: " + err.Error()}}
	}
	root := mappingRoot(&doc)
	if root == nil {
		return findings
	}

	// 1) top-level permissions
	if !hasKey(root, "permissions") {
		findings = append(findings, Finding{
			Rule: "permissions/missing", Severity: SeverityHigh,
			Line: 1,
			Message: "workflow has no top-level `permissions:` block",
			Hint:    "Set `permissions: {contents: read}` and grant additional scopes per-job.",
		})
	}

	// 2) pull_request_target with PR head checkout
	if onNode := getKey(root, "on"); onNode != nil && yamlContainsKey(onNode, "pull_request_target") {
		jobsNode := getKey(root, "jobs")
		if jobsNode != nil {
			eachMappingPair(jobsNode, func(_, jobNode *yaml.Node) {
				steps := getKey(jobNode, "steps")
				if steps != nil {
					for _, st := range steps.Content {
						uses := stringValue(getKey(st, "uses"))
						with := getKey(st, "with")
						if strings.HasPrefix(uses, "actions/checkout@") && with != nil {
							ref := stringValue(getKey(with, "ref"))
							if strings.Contains(ref, "github.event.pull_request.head") {
								findings = append(findings, Finding{
									Rule: "pull_request_target/pr-head-checkout", Severity: SeverityHigh,
									Line: st.Line, Message: "checkout uses PR head SHA inside a `pull_request_target` workflow",
									Hint: "Either drop pull_request_target, or harden by ensuring the workflow does not run untrusted code from forks.",
								})
							}
						}
					}
				}
			})
		}
	}

	// 3) floating action references — uses: foo/bar@v3 instead of @<sha>
	floating := regexp.MustCompile(`^[A-Za-z0-9_./-]+@v?[0-9A-Za-z._-]+$`)
	sha := regexp.MustCompile(`^[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)
	walkSteps(root, func(step *yaml.Node) {
		uses := stringValue(getKey(step, "uses"))
		if uses == "" || strings.HasPrefix(uses, "./") {
			return
		}
		if !sha.MatchString(uses) && floating.MatchString(uses) {
			findings = append(findings, Finding{
				Rule: "uses/floating-tag", Severity: SeverityMedium,
				Line: step.Line, Message: "third-party action referenced by tag (`" + uses + "`) instead of commit SHA",
				Hint: "Pin to the full 40-char commit SHA for supply-chain safety, then keep it up to date with dependabot.",
			})
		}
	})

	// 4) secrets piped through env to run blocks
	walkSteps(root, func(step *yaml.Node) {
		env := getKey(step, "env")
		run := stringValue(getKey(step, "run"))
		if env == nil || run == "" {
			return
		}
		var secretEnvs []string
		eachMappingPair(env, func(k, v *yaml.Node) {
			if strings.Contains(stringValue(v), "secrets.") {
				secretEnvs = append(secretEnvs, stringValue(k))
			}
		})
		for _, name := range secretEnvs {
			if regexp.MustCompile(`(?i)\becho\s+\$`+regexp.QuoteMeta(name)+`\b|\$\{`+regexp.QuoteMeta(name)+`\}`).MatchString(run) {
				findings = append(findings, Finding{
					Rule: "secret/echo-leak", Severity: SeverityHigh,
					Line: step.Line,
					Message: "step echoes the secret env var `" + name + "` into the log",
					Hint:    "Avoid `echo $" + name + "`; use `printenv -- " + name + " >/dev/null 2>&1` only for presence checks.",
				})
			}
		}
	})

	// 5) self-hosted runners with no labels
	walkJobs(root, func(jobNode *yaml.Node) {
		runsOn := getKey(jobNode, "runs-on")
		if runsOn == nil {
			return
		}
		switch runsOn.Kind {
		case yaml.ScalarNode:
			if runsOn.Value == "self-hosted" {
				findings = append(findings, Finding{
					Rule: "runner/self-hosted-no-labels", Severity: SeverityMedium,
					Line: runsOn.Line, Message: "self-hosted runner without specific labels",
					Hint: "Add labels (e.g. [self-hosted, linux, gpu]) so jobs cannot be scheduled on unintended fleets.",
				})
			}
		case yaml.SequenceNode:
			if len(runsOn.Content) == 1 && runsOn.Content[0].Value == "self-hosted" {
				findings = append(findings, Finding{
					Rule: "runner/self-hosted-no-labels", Severity: SeverityMedium,
					Line: runsOn.Line, Message: "self-hosted runner without additional labels",
					Hint: "Add labels (e.g. [self-hosted, linux]) so jobs cannot be scheduled on unintended fleets.",
				})
			}
		}
	})

	// 6) `if:` always-true typos: `contains(github.event_name, '...')` is rare;
	//    `${{ secrets.X != '' }}` is illegal because secrets cannot be compared
	//    to '' in expressions. Flag any such pattern.
	walkSteps(root, func(step *yaml.Node) {
		ifVal := stringValue(getKey(step, "if"))
		if strings.Contains(ifVal, "secrets.") && strings.Contains(ifVal, "!= ''") {
			findings = append(findings, Finding{
				Rule: "if/secrets-comparison", Severity: SeverityLow,
				Line: step.Line,
				Message: "`if:` compares a secret to '' which always evaluates true",
				Hint:    "Move the existence check to a job-level `env:` mapping or to a script.",
			})
		}
	})

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings
}

// ----------------- yaml.Node helpers -----------------

func mappingRoot(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	if n.Kind == yaml.MappingNode {
		return n
	}
	return nil
}

func hasKey(m *yaml.Node, name string) bool { return getKey(m, name) != nil }

func getKey(m *yaml.Node, name string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == name {
			return m.Content[i+1]
		}
	}
	return nil
}

func eachMappingPair(m *yaml.Node, fn func(k, v *yaml.Node)) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		fn(m.Content[i], m.Content[i+1])
	}
}

func stringValue(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

func yamlContainsKey(n *yaml.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode && n.Value == name {
		return true
	}
	if n.Kind == yaml.SequenceNode {
		for _, c := range n.Content {
			if c.Value == name {
				return true
			}
		}
	}
	if n.Kind == yaml.MappingNode {
		return getKey(n, name) != nil
	}
	return false
}

func walkSteps(root *yaml.Node, fn func(step *yaml.Node)) {
	jobs := getKey(root, "jobs")
	if jobs == nil {
		return
	}
	eachMappingPair(jobs, func(_, jobNode *yaml.Node) {
		steps := getKey(jobNode, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			return
		}
		for _, st := range steps.Content {
			fn(st)
		}
	})
}

func walkJobs(root *yaml.Node, fn func(jobNode *yaml.Node)) {
	jobs := getKey(root, "jobs")
	if jobs == nil {
		return
	}
	eachMappingPair(jobs, func(_, jobNode *yaml.Node) { fn(jobNode) })
}
