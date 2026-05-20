package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helper: build a fake GitHub server responding to the documented endpoints.
func newServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewClient("test-token")
	c.APIBase = srv.URL
	c.HTTP = srv.Client()
	// Force CheckRedirect on the test client too:
	if hc, ok := c.HTTP.(*http.Client); ok {
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return srv, c
}

func TestGetRun(t *testing.T) {
	want := Run{ID: 42, Path: ".github/workflows/ci.yml", HeadSHA: "deadbeef", Status: "completed", Conclusion: "failure"}
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/repos/o/r/actions/runs/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(want)
	})
	got, err := c.GetRun(context.Background(), RunRef{Owner: "o", Repo: "r", RunID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Conclusion != "failure" {
		t.Fatalf("decode mismatch: %+v", got)
	}
}

func TestListJobs(t *testing.T) {
	page1 := map[string]any{
		"total_count": 1,
		"jobs": []Job{{ID: 1, Name: "build", Conclusion: "failure", Steps: []Step{{Name: "tests", Conclusion: "failure"}}}},
	}
	_, c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(page1)
	})
	jobs, err := c.ListJobs(context.Background(), RunRef{Owner: "o", Repo: "r", RunID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Name != "build" {
		t.Fatalf("got %+v", jobs)
	}
}

func TestDownloadRunLogs_FollowsRedirect(t *testing.T) {
	// Build a small zip in memory that the test "presigned" handler returns.
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	f, _ := zw.Create("build/job.txt")
	_, _ = f.Write([]byte("line1\nline2\n"))
	_ = zw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/presigned", func(w http.ResponseWriter, r *http.Request) {
		// would fail if Authorization header is present
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "auth must not leak to presigned", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = io.Copy(w, bytes.NewReader(z.Bytes()))
	})
	mux.HandleFunc("/repos/o/r/actions/runs/", func(w http.ResponseWriter, r *http.Request) {
		// simulate redirect
		http.Redirect(w, r, "/presigned", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("token")
	c.APIBase = srv.URL
	c.HTTP = srv.Client()
	if hc, ok := c.HTTP.(*http.Client); ok {
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	logs, err := c.DownloadRunLogs(context.Background(), RunRef{Owner: "o", Repo: "r", RunID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := logs["build/job.txt"]; !ok {
		t.Fatalf("missing log entry: %v", logs)
	}
}
