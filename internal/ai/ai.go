// Package ai contains the AI patch adapter. The adapter is provider-agnostic
// (Claude, OpenAI, local LLM) and is purposefully thin: ReproForge never
// trusts an AI patch as-is. Patches must pass replay/test verification before
// they may be marked as "verified" in any output.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/diagnose"
	"github.com/reproforge/reproforge/internal/redaction"
)

// Provider is the AI backend identifier.
type Provider string

// Known providers.
const (
	ProviderClaude  Provider = "claude"
	ProviderOpenAI  Provider = "openai"
	ProviderLocal   Provider = "local"
	ProviderNone    Provider = "none"
)

// Patch is the AI's response, parsed and ready to apply.
type Patch struct {
	Plan       string   `json:"plan"`
	Diff       string   `json:"diff"`
	Files      []string `json:"files,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
}

// PromptInput is the sanitised context we send to the AI.
type PromptInput struct {
	Repo        string             `json:"repo"`
	Workflow    string             `json:"workflow"`
	Job         string             `json:"job"`
	Step        string             `json:"step"`
	FailedCmd   string             `json:"failedCommand"`
	Diagnosis   diagnose.Diagnosis `json:"diagnosis"`
	Snippets    []Snippet          `json:"snippets"`
	Constraints []string           `json:"constraints"`
}

// Snippet is a small log/code excerpt safe for AI context.
type Snippet struct {
	Source string `json:"source"`
	Lines  int    `json:"lines"`
	Body   string `json:"body"`
}

// Adapter dispatches requests to a concrete provider.
type Adapter interface {
	// SuggestPatch returns a patch plan + diff for the given context. The
	// implementation MUST sanitise the input first and MUST NOT include any
	// secrets in the request body. Returning a non-nil patch does not imply
	// the patch is correct — callers must verify it via replay.
	SuggestPatch(ctx context.Context, in PromptInput) (*Patch, error)
}

// SanitisedPrompt builds a PromptInput from a capsule + diagnosis, applying
// the default redaction rules to all text bodies.
func SanitisedPrompt(c *capsule.Capsule, d diagnose.Diagnosis, snippets []Snippet) PromptInput {
	r := redaction.NewDefault()
	out := PromptInput{
		Repo:      c.Repo,
		Workflow:  c.Workflow,
		Job:       c.Job,
		Step:      c.Failure.Step,
		FailedCmd: redactString(r, c.Failure.Command),
		Diagnosis: redactDiagnosis(r, d),
		Constraints: []string{
			"Return a unified diff that applies to the repo at HEAD.",
			"Do not introduce new external services.",
			"Do not weaken or remove tests.",
			"Do not add credentials or secrets.",
			"Prefer a minimal change that fixes the failing target only.",
		},
	}
	for _, s := range snippets {
		s.Body = redactString(r, s.Body)
		out.Snippets = append(out.Snippets, s)
	}
	return out
}

func redactString(r *redaction.Engine, s string) string {
	out, _ := r.RedactString(s)
	return out
}

func redactDiagnosis(r *redaction.Engine, d diagnose.Diagnosis) diagnose.Diagnosis {
	d.Command = redactString(r, d.Command)
	for i, e := range d.Evidence {
		d.Evidence[i] = redactString(r, e)
	}
	for i, e := range d.NextActions {
		d.NextActions[i] = redactString(r, e)
	}
	return d
}

// NewAdapter returns the requested adapter or an error if env is missing.
func NewAdapter(p Provider) (Adapter, error) {
	switch p {
	case ProviderClaude:
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, errors.New("ANTHROPIC_API_KEY is not set")
		}
		return &claudeAdapter{key: key, model: defaultEnv("ANTHROPIC_MODEL", "claude-3-5-sonnet-latest")}, nil
	case ProviderOpenAI:
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("OPENAI_API_KEY is not set")
		}
		return &openaiAdapter{key: key, model: defaultEnv("OPENAI_MODEL", "gpt-4o-mini")}, nil
	case ProviderLocal:
		// Local stub returns a deterministic "no-op" patch so tests can
		// exercise the verify path without external dependencies.
		return &localAdapter{}, nil
	case ProviderNone, "":
		return &disabledAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q", p)
	}
}

type disabledAdapter struct{}

func (disabledAdapter) SuggestPatch(ctx context.Context, in PromptInput) (*Patch, error) {
	return nil, errors.New("AI adapter disabled (--ai none); set --ai claude|openai|local to enable")
}

type localAdapter struct{}

// SuggestPatch returns a deterministic stub: plan describes the failure, no
// diff is produced. This is useful for unit tests and "dry mode" CI runs.
func (localAdapter) SuggestPatch(ctx context.Context, in PromptInput) (*Patch, error) {
	plan := fmt.Sprintf("local-stub: would investigate %s failure in step %s", in.Diagnosis.Category, in.Step)
	return &Patch{Plan: plan, Confidence: 0.0, Diff: ""}, nil
}

type claudeAdapter struct {
	key   string
	model string
	http  *http.Client
}

func (a *claudeAdapter) SuggestPatch(ctx context.Context, in PromptInput) (*Patch, error) {
	body, err := json.Marshal(map[string]any{
		"model":      a.model,
		"max_tokens": 2000,
		"system":     systemPrompt(),
		"messages": []any{
			map[string]any{"role": "user", "content": userPrompt(in)},
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	if a.http == nil {
		a.http = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Content []struct {
			Type, Text string
		} `json:"content"`
		Error struct {
			Type, Message string
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Error.Message != "" {
		return nil, fmt.Errorf("claude: %s", raw.Error.Message)
	}
	var text string
	for _, c := range raw.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return parsePatchResponse(text), nil
}

type openaiAdapter struct {
	key   string
	model string
	http  *http.Client
}

func (a *openaiAdapter) SuggestPatch(ctx context.Context, in PromptInput) (*Patch, error) {
	body, err := json.Marshal(map[string]any{
		"model": a.model,
		"messages": []any{
			map[string]any{"role": "system", "content": systemPrompt()},
			map[string]any{"role": "user", "content": userPrompt(in)},
		},
		"temperature": 0,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("content-type", "application/json")
	if a.http == nil {
		a.http = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Error.Message != "" {
		return nil, fmt.Errorf("openai: %s", raw.Error.Message)
	}
	if len(raw.Choices) == 0 {
		return nil, errors.New("openai: empty response")
	}
	return parsePatchResponse(raw.Choices[0].Message.Content), nil
}

func systemPrompt() string {
	return strings.TrimSpace(`
You are ReproForge's verified-patch assistant.

You receive a CI failure context. Respond strictly with:
1) A short PLAN (max 80 words).
2) A unified diff in a single fenced code block (` + "```diff ... ```" + `).
3) A short LIST of files changed.

You MUST NOT introduce or modify secrets.
You MUST NOT remove or weaken tests.
You MUST NOT change unrelated files.
You MUST keep the change minimal.
`)
}

func userPrompt(in PromptInput) string {
	b, _ := json.MarshalIndent(in, "", "  ")
	return "Failure context:\n```json\n" + string(b) + "\n```\nReply with PLAN, then unified diff."
}

func parsePatchResponse(text string) *Patch {
	plan := text
	diff := ""
	if i := strings.Index(text, "```diff"); i >= 0 {
		rest := text[i+len("```diff"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			diff = strings.TrimSpace(rest[:j])
			plan = strings.TrimSpace(text[:i])
		}
	}
	files := []string{}
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "+++ b/") {
			files = append(files, strings.TrimSpace(strings.TrimPrefix(l, "+++ b/")))
		}
	}
	return &Patch{Plan: plan, Diff: diff, Files: files, Confidence: 0.5}
}

func defaultEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
