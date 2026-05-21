package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
)

func makeCapsule(t *testing.T, dir string) []byte {
	t.Helper()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "logs", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "logs", "build", "j.log"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &capsule.Capsule{
		Schema: capsule.SchemaV1, CreatedAt: time.Now().UTC(),
		Provider: capsule.ProviderGitHub, Repo: "octocat/hello",
		Commit: "abc", Workflow: "ci.yml", Job: "build",
		Runner: capsule.Runner{OS: "ubuntu-24.04", Arch: "x86_64"},
		Replay: capsule.Replay{Modes: []string{capsule.ReplayFailedStep}, Network: capsule.NetworkConfigurable},
		Redaction: capsule.Redaction{Status: "passed", Rules: 1},
		Failure: capsule.Failure{
			Step: "Run tests", Command: "false", ExitCode: 1,
			Fingerprint: "sha256:" + strings.Repeat("a", 64),
		},
		Logs: []capsule.LogFile{{Path: "logs/build/j.log", Job: "build"}},
	}
	out := filepath.Join(dir, "rf.tar.zst")
	if err := capsule.PackFile(out, capsule.PackOptions{SourceDir: src, Manifest: c}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestServer_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	srv, err := New(Config{Storage: filepath.Join(dir, "store"), Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	body := makeCapsule(t, dir)
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()

	// healthz
	if r, err := http.Get(hs.URL + "/healthz"); err != nil || r.StatusCode != 200 {
		t.Fatalf("healthz: err=%v status=%d", err, r.StatusCode)
	}

	// upload without token → 401
	r1, err := http.Post(hs.URL+"/api/v1/capsules", "application/zstd", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if r1.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r1.StatusCode)
	}

	// upload with token → 201
	req, _ := http.NewRequest("POST", hs.URL+"/api/v1/capsules", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.StatusCode != 201 {
		b, _ := io.ReadAll(r2.Body)
		t.Fatalf("upload: %d %s", r2.StatusCode, b)
	}

	// list
	r3, err := http.Get(hs.URL + "/api/v1/capsules?repo=octocat/hello")
	if err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	_ = json.NewDecoder(r3.Body).Decode(&listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 entry, got %v", listed)
	}

	// download
	fp := "sha256:" + strings.Repeat("a", 64)
	r4, err := http.Get(hs.URL + "/api/v1/capsules/" + strings.TrimPrefix(fp, "sha256:"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r4.Body)
	if r4.StatusCode != 200 || len(got) != len(body) {
		t.Fatalf("download mismatch: status=%d len=%d want=%d", r4.StatusCode, len(got), len(body))
	}
	// content equality via sha
	a := sha256.Sum256(got)
	b := sha256.Sum256(body)
	if hex.EncodeToString(a[:]) != hex.EncodeToString(b[:]) {
		t.Fatal("downloaded body differs")
	}

	// not found
	r5, _ := http.Get(hs.URL + "/api/v1/capsules/nonexistent")
	if r5.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", r5.StatusCode)
	}
}

func TestServer_RejectsTooLarge(t *testing.T) {
	srv, err := New(Config{Storage: t.TempDir(), Token: "x", MaxBody: 64})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()
	huge := bytes.Repeat([]byte{'X'}, 1024)
	req, _ := http.NewRequest("POST", hs.URL+"/api/v1/capsules", bytes.NewReader(huge))
	req.Header.Set("Authorization", "Bearer x")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode == 201 {
		t.Fatal("expected non-201 for oversize body")
	}
}
