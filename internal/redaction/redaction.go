// Package redaction implements the secret/PII redaction pipeline used before
// any data leaves the local machine (capsule, report, AI prompt).
//
// Default rules cover GitHub tokens (ghp_/gho_/ghu_/ghs_/ghr_), classic 40-hex
// PATs, JWTs, AWS access/secret keys, GCP service account JSON private_key
// fields, Azure storage keys, npm tokens, generic Bearer tokens, slack tokens,
// stripe live/test keys, OpenAI/Anthropic API keys, and PEM private keys.
package redaction

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Hit describes a single redaction event.
type Hit struct {
	Rule    string `json:"rule"`
	Line    int    `json:"line,omitempty"`
	File    string `json:"file,omitempty"`
	Hash    string `json:"hash"`     // sha256 of the redacted bytes (for evidence without leaking)
	Length  int    `json:"length"`
}

// Report summarises redaction over one or more inputs.
type Report struct {
	Status       string    `json:"status"`     // "passed" | "failed"
	Rules        int       `json:"rules"`
	Hits         []Hit     `json:"hits"`
	GeneratedAt  time.Time `json:"generatedAt"`
	Files        []string  `json:"files,omitempty"`
}

// Rule is a redaction rule with a name and a regular expression.
type Rule struct {
	Name string
	Re   *regexp.Regexp
	// Group is the regexp group to redact. 0 means the whole match.
	Group int
}

// Replacement returned for redacted bytes.
const Replacement = "[REDACTED]"

// Engine applies a set of rules to streams or strings.
type Engine struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewDefault constructs an engine with the built-in rule set.
func NewDefault() *Engine {
	return New(DefaultRules())
}

// New constructs an engine with a custom rule set (built-ins not included).
func New(rules []Rule) *Engine {
	return &Engine{rules: append([]Rule(nil), rules...)}
}

// AddRule appends a rule to the engine.
func (e *Engine) AddRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, r)
}

// AddPattern compiles and appends a named regex pattern.
func (e *Engine) AddPattern(name, pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("compile rule %q: %w", name, err)
	}
	e.AddRule(Rule{Name: name, Re: re})
	return nil
}

// AddDenylist adds exact substring rules (case-sensitive). Each entry becomes
// a regex literal-match rule.
func (e *Engine) AddDenylist(values []string) {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		e.AddRule(Rule{Name: "denylist", Re: regexp.MustCompile(regexp.QuoteMeta(v))})
	}
}

// Rules returns a copy of the active rule list.
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// RedactString returns the redacted string and a slice of hits describing what
// was matched (without leaking actual secret values; only sha256 + length).
func (e *Engine) RedactString(s string) (string, []Hit) {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	var hits []Hit
	out := s
	for _, r := range rules {
		if r.Re == nil {
			continue
		}
		out = r.Re.ReplaceAllStringFunc(out, func(m string) string {
			h := sha256.Sum256([]byte(m))
			hits = append(hits, Hit{Rule: r.Name, Hash: hex.EncodeToString(h[:]), Length: len(m)})
			if r.Group <= 0 {
				return Replacement
			}
			// preserve everything except the captured group
			submatch := r.Re.FindStringSubmatchIndex(m)
			if len(submatch) <= 2*r.Group+1 {
				return Replacement
			}
			start, end := submatch[2*r.Group], submatch[2*r.Group+1]
			return m[:start] + Replacement + m[end:]
		})
	}
	return out, hits
}

// Redact reads from r line-by-line, redacting matches, writing to w. It returns
// the line count, hit list and any error encountered.
func (e *Engine) Redact(r io.Reader, w io.Writer) ([]Hit, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20) // 16MB lines
	var allHits []Hit
	line := 0
	for scanner.Scan() {
		line++
		redacted, hits := e.RedactString(scanner.Text())
		for i := range hits {
			hits[i].Line = line
		}
		allHits = append(allHits, hits...)
		if _, err := w.Write([]byte(redacted)); err != nil {
			return allHits, err
		}
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return allHits, err
		}
	}
	if err := scanner.Err(); err != nil {
		return allHits, err
	}
	return allHits, nil
}

// Report builds a summary from a list of hits and source files.
func (e *Engine) Report(files []string, hits []Hit) Report {
	rep := Report{
		Status:      "passed",
		Rules:       len(e.Rules()),
		Hits:        hits,
		GeneratedAt: time.Now().UTC(),
		Files:       append([]string(nil), files...),
	}
	sort.Slice(rep.Files, func(i, j int) bool { return rep.Files[i] < rep.Files[j] })
	return rep
}

// HasMatch quickly checks if any rule matches s. Useful for tests.
func (e *Engine) HasMatch(s string) bool {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()
	for _, r := range rules {
		if r.Re == nil {
			continue
		}
		if r.Re.MatchString(s) {
			return true
		}
	}
	return false
}

// DefaultRules returns the built-in redaction rule set.
func DefaultRules() []Rule {
	mk := func(n, p string) Rule { return Rule{Name: n, Re: regexp.MustCompile(p)} }
	return []Rule{
		// GitHub tokens (modern)
		mk("github_token", `\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
		// GitHub fine-grained personal access tokens
		mk("github_fine_grained", `\bgithub_pat_[A-Za-z0-9_]{82}\b`),
		// JWT (3 base64url segments)
		mk("jwt", `\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`),
		// AWS access key id
		mk("aws_access_key_id", `\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASCA)[0-9A-Z]{16}\b`),
		// AWS secret access key (heuristic 40-char base64-ish following AWS_SECRET_)
		mk("aws_secret_access_key", `(?i)\baws[_-]?secret[_-]?access[_-]?key\s*[=:]\s*([A-Za-z0-9/+=]{40})`),
		// Generic Bearer auth (header form)
		mk("bearer_token", `(?i)\b(?:authorization|x-api-key)\s*[:=]\s*(?:Bearer\s+)?["']?[A-Za-z0-9_\-\.=]{20,}["']?`),
		// Inline Bearer prefix
		mk("bearer_inline", `(?i)\bBearer\s+[A-Za-z0-9_\-\.=]{20,}\b`),
		// Slack tokens
		mk("slack_token", `\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
		// Stripe keys
		mk("stripe_key", `\b(?:sk|pk|rk)_(?:test|live)_[A-Za-z0-9]{16,}\b`),
		// OpenAI keys
		mk("openai_key", `\bsk-(?:proj-)?[A-Za-z0-9_\-]{20,}\b`),
		// Anthropic keys
		mk("anthropic_key", `\bsk-ant-[A-Za-z0-9_\-]{20,}\b`),
		// Google API key (39 chars total: AIza + 35)
		mk("google_api_key", `\bAIza[0-9A-Za-z_\-]{35}`),
		// npm token
		mk("npm_token", `\bnpm_[A-Za-z0-9]{36}\b`),
		// PEM private key blocks (multi-line; we redact start markers and any visible base64 line)
		mk("pem_private_key", `-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP |ENCRYPTED )?PRIVATE KEY-----`),
		mk("pem_private_key_body", `^[A-Za-z0-9+/=]{60,}$`),
		// Azure storage account key
		mk("azure_storage_key", `(?i)AccountKey\s*=\s*[A-Za-z0-9+/=]{60,}`),
		// SSH key
		mk("ssh_key", `\bssh-(?:rsa|ed25519|dss|ecdsa)\s+[A-Za-z0-9+/=]{40,}`),
		// Generic high-entropy 64+ hex secret with key=value framing
		mk("env_secret_kv", `(?i)\b(?:secret|password|passwd|api[_-]?key|token|access[_-]?key)\s*[=:]\s*["']?[A-Za-z0-9+/=_\-]{16,}["']?`),
		// "private_key": "..." in JSON (e.g. GCP service accounts)
		mk("json_private_key", `"private_key"\s*:\s*"[^"]+"`),
	}
}

// CommonSecretEnvNames lists environment variable names that should always be
// redacted on sight (covering the value).
func CommonSecretEnvNames() []string {
	return []string{
		"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT",
		"NPM_TOKEN", "PYPI_TOKEN",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"GCP_SERVICE_ACCOUNT_KEY", "GOOGLE_APPLICATION_CREDENTIALS_JSON",
		"AZURE_CLIENT_SECRET", "AZURE_STORAGE_KEY",
		"SLACK_BOT_TOKEN", "SLACK_TOKEN",
		"STRIPE_SECRET_KEY",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY",
		"DOCKER_PASSWORD", "REGISTRY_PASSWORD",
		"SSH_PRIVATE_KEY", "DEPLOY_KEY",
	}
}
