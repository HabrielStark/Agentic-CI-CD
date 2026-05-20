package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/store"
)

func TestHistoryCommands(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".reproforge", 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(".reproforge", "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Insert(context.Background(), store.RunRecord{
		Repo: "o/r", Provider: "github_actions", Workflow: "ci.yml", Job: "build",
		RunID: 1, CommitSHA: "abc",
		Fingerprint: "sha256:" + strings.Repeat("a", 64),
		Category:    "network_issue", Confidence: 0.8, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	out, _, err := runCLI("history", "show", "--fingerprint", "sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "network_issue") {
		t.Fatalf("expected category in output: %s", out)
	}
	out, _, err = runCLI("history", "stats", "--repo", "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "network_issue") {
		t.Fatalf("expected aggregate output: %s", out)
	}
	if _, _, err := runCLI("history", "show"); err == nil {
		t.Fatal("expected error without --fingerprint")
	}
}
