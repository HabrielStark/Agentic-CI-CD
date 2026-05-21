package diagnose

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/store"
)

func TestRerank_NilStorePassthrough(t *testing.T) {
	in := map[string]float64{"network_issue": 0.5, "code_or_test_failure": 0.5}
	out, err := Rerank(context.Background(), nil, "o/r", in, []string{"foo"}, 0.6)
	if err != nil {
		t.Fatal(err)
	}
	if out["network_issue"] != 0.5 || out["code_or_test_failure"] != 0.5 {
		t.Fatalf("expected pass-through, got %+v", out)
	}
}

func TestRerank_EmptyStorePassthrough(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in := map[string]float64{"a": 0.4, "b": 0.6}
	out, err := Rerank(context.Background(), s, "o/r", in, []string{}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if out["a"] != 0.4 || out["b"] != 0.6 {
		t.Fatalf("expected pass-through with empty store, got %+v", out)
	}
}

func TestRerank_WithHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Insert several network_issue runs to teach the model a strong prior.
	for i := 0; i < 5; i++ {
		_, _ = s.Insert(context.Background(), store.RunRecord{
			Repo: "o/r", Provider: "github_actions", Workflow: "ci.yml",
			Job: "build", RunID: int64(i + 1), CommitSHA: "abc",
			Fingerprint: "sha256:network", Category: "network_issue",
			Confidence: 0.9, CreatedAt: time.Now(),
		})
	}
	in := map[string]float64{"network_issue": 0.5, "code_or_test_failure": 0.5}
	out, err := Rerank(context.Background(), s, "o/r", in, []string{"network connection refused"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if out["network_issue"] < 0.5 {
		t.Fatalf("rule floor violated: %+v", out)
	}
}
