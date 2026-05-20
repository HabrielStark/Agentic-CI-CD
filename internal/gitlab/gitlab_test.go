package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	a := NewAdapter("")
	cases := []struct {
		in      string
		owner   string
		repo    string
		runID   int64
		wantErr bool
	}{
		{"https://gitlab.com/foo/bar/-/pipelines/12345", "foo", "bar", 12345, false},
		{"https://gitlab.com/grp/sub/proj/-/jobs/9", "grp", "sub/proj", 9, false},
		{"42:1000", "project", "42", 1000, false},
		{"not a url", "", "", 0, true},
		{"", "", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := a.Parse(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.Owner != c.owner || ref.Repo != c.repo || ref.RunID != c.runID {
				t.Fatalf("got %+v want owner=%s repo=%s run=%d", ref, c.owner, c.repo, c.runID)
			}
		})
	}
}

func TestFetchRunAndLogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/foo%2Fbar/pipelines/1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "sha": "abc", "ref": "main", "status": "failed", "web_url": "https://gitlab.com/foo/bar/-/pipelines/1",
		})
	})
	mux.HandleFunc("/projects/foo%2Fbar/pipelines/1/jobs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": 9, "name": "test", "status": "failed", "stage": "test", "tag_list": []string{"linux"}},
		})
	})
	mux.HandleFunc("/projects/foo%2Fbar/jobs/9/trace", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tests failed: assertion error"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := NewAdapter("token")
	a.APIBase = srv.URL

	ref, err := a.Parse("https://gitlab.com/foo/bar/-/pipelines/1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := a.FetchRun(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if run.HeadSHA != "abc" || len(run.Jobs) != 1 || run.Jobs[0].Conclusion != "failed" {
		t.Fatalf("run wrong: %+v", run)
	}
	logs, err := a.DownloadLogs(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logs["test.txt"]), "assertion error") {
		t.Fatalf("logs wrong: %v", logs)
	}
}
