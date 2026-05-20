package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_RunsAndFingerprints(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	fp := "sha256:" + strings.Repeat("a", 64)
	_, err = s.Insert(ctx, RunRecord{
		Repo: "o/r", Provider: "github_actions", Workflow: "ci.yml", Job: "build",
		RunID: 1, CommitSHA: "deadbeef", Fingerprint: fp,
		Category: "network_issue", Confidence: 0.82, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Insert(ctx, RunRecord{
		Repo: "o/r", Provider: "github_actions", Workflow: "ci.yml", Job: "build",
		RunID: 2, CommitSHA: "feedf00d", Fingerprint: fp,
		Category: "network_issue", Confidence: 0.85, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CountByFingerprint(ctx, fp)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
	hist, err := s.HistoryByFingerprint(ctx, fp, 10)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history wrong: %v err=%v", hist, err)
	}
	stats, err := s.Aggregate(ctx, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRuns != 2 || stats.Categories["network_issue"] != 2 {
		t.Fatalf("aggregate wrong: %+v", stats)
	}
}

func TestStore_FlakeAndReplay(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, err = s.InsertFlake(ctx, FlakeStat{Fingerprint: "fp", Target: "t", Runs: 10, Passed: 6, Failed: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.InsertReplay(ctx, ReplayRecord{Fingerprint: "fp", Mode: "failed-step", Network: "allow", ExitCode: 1, Reproduced: true, DurationMs: 1234})
	if err != nil {
		t.Fatal(err)
	}
}
