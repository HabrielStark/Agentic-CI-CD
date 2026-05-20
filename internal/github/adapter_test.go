package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reproforge/reproforge/internal/provider"
)

func TestAdapter_FetchRunAndParse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/actions/runs/42", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Run{
			ID: 42, Path: ".github/workflows/ci.yml", HeadSHA: "abc", HeadBranch: "main",
			Status: "completed", Conclusion: "failure",
		})
	})
	mux.HandleFunc("/repos/o/r/actions/runs/42/jobs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"jobs": []Job{{ID: 1, Name: "build", Conclusion: "failure", Steps: []Step{
				{Name: "test", Conclusion: "failure"},
			}}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := NewAdapter("token").(*Adapter)
	a.c.APIBase = srv.URL
	a.c.HTTP = srv.Client()
	if hc, ok := a.c.HTTP.(*http.Client); ok {
		hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	ref, err := a.Parse("https://github.com/o/r/actions/runs/42")
	if err != nil {
		t.Fatal(err)
	}
	if ref.RunID != 42 {
		t.Fatalf("ref wrong: %+v", ref)
	}

	run, err := a.FetchRun(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if run.HeadSHA != "abc" || len(run.Jobs) != 1 || run.Jobs[0].Steps[0].Name != "test" {
		t.Fatalf("run wrong: %+v", run)
	}

	j, st := provider.PickFailedJob(run.Jobs, 0)
	if j == nil || st == nil {
		t.Fatal("PickFailedJob returned nil")
	}
}
