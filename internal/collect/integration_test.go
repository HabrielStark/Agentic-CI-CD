// End-to-end exercise of the capsule pipeline using synthetic GitHub API
// responses served by an in-process httptest server. This avoids any network
// access and proves all components compose correctly.
package collect

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/github"
)

func TestFromRun_EndToEnd(t *testing.T) {
	// Compose a fake GitHub server.
	mux := http.NewServeMux()
	run := github.Run{
		ID: 42, Path: ".github/workflows/ci.yml", HeadSHA: "deadbeef",
		HeadBranch: "main", Status: "completed", Conclusion: "failure",
		URL: "https://github.com/octocat/hello-world/actions/runs/42",
	}
	mux.HandleFunc("/repos/octocat/hello-world/actions/runs/42", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(run)
	})
	jobsBody := map[string]any{
		"total_count": 1,
		"jobs": []github.Job{{
			ID: 9, Name: "build", Conclusion: "failure", RunnerOS: "Linux",
			Steps: []github.Step{
				{Name: "Checkout", Conclusion: "success", Number: 1},
				{Name: "Run tests", Conclusion: "failure", Number: 2},
			},
		}},
	}
	mux.HandleFunc("/repos/octocat/hello-world/actions/runs/42/jobs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jobsBody)
	})

	logBody := []byte(`
2026-05-20T12:00:00Z ##[group]Run pytest -q
2026-05-20T12:00:01Z + GITHUB_TOKEN=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA pytest tests/
2026-05-20T12:00:02Z Traceback (most recent call last):
2026-05-20T12:00:02Z   File "/runner/_work/r/r/tests/test_api.py", line 42, in test_login
2026-05-20T12:00:02Z     assert resp.status_code == 200
2026-05-20T12:00:02Z AssertionError: 500 != 200
2026-05-20T12:00:02Z ##[error]Process completed with exit code 1.
`)

	// build a zip archive with one log file
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	f, _ := zw.Create("build/2_Run tests.txt")
	_, _ = f.Write(logBody)
	_ = zw.Close()

	mux.HandleFunc("/repos/octocat/hello-world/actions/runs/42/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(z.Bytes())
	})

	// raw.githubusercontent.com fallback path: contents API
	mux.HandleFunc("/repos/octocat/hello-world/contents/.github/workflows/ci.yml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.github.raw")
		_, _ = w.Write([]byte("name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: pytest -q\n"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Use a custom NewClient via env override is not available; instead we
	// bypass FromRun and replicate it with our own client to make the test
	// deterministic. But to avoid duplicating logic, we point the client by
	// using a small monkey patch via the package-level test helper.

	tmp := t.TempDir()
	res, err := fromRunWithClientForTest(context.Background(), srv.URL, github.RunRef{Owner: "octocat", Repo: "hello-world", RunID: 42}, Options{
		OutputDir: tmp, Token: "x", Diagnose: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Capsule
	if c == nil {
		t.Fatal("nil capsule")
	}
	if c.Repo != "octocat/hello-world" {
		t.Fatalf("repo wrong: %s", c.Repo)
	}
	if !strings.HasPrefix(c.Failure.Fingerprint, "sha256:") {
		t.Fatalf("fingerprint missing: %s", c.Failure.Fingerprint)
	}

	// Logs were redacted: ghp_ token must be gone in any file under capsule/logs.
	logsDir := filepath.Join(tmp, "capsule", "logs")
	var found bool
	err = filepath.WalkDir(logsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, _ := os.ReadFile(p)
		if strings.Contains(string(body), "ghp_") {
			t.Fatalf("token leaked into capsule log %s", p)
		}
		found = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("no redacted logs written")
	}

	// pack capsule
	out := filepath.Join(tmp, "rf.tar.zst")
	if err := capsule.PackFile(out, capsule.PackOptions{SourceDir: filepath.Join(tmp, "capsule"), Manifest: c}); err != nil {
		t.Fatal(err)
	}

	// unpack
	dest := filepath.Join(tmp, "extracted")
	got, err := capsule.UnpackFile(out, dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failure.Fingerprint != c.Failure.Fingerprint {
		t.Fatal("fingerprint changed across pack/unpack")
	}
}
