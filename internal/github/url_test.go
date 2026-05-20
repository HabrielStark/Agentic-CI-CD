package github

import "testing"

func TestParseRunURL(t *testing.T) {
	cases := []struct {
		in       string
		owner    string
		repo     string
		runID    int64
		jobID    int64
		errLike  string
	}{
		{"https://github.com/octocat/hello-world/actions/runs/12345", "octocat", "hello-world", 12345, 0, ""},
		{"https://github.com/o/r/actions/runs/1/job/9", "o", "r", 1, 9, ""},
		{"https://github.com/o/r/actions/runs/1/attempts/2", "o", "r", 1, 0, ""},
		{"", "", "", 0, 0, "empty"},
		{"https://example.com/foo", "", "", 0, 0, "not a workflow"},
		{"http://github.com/o/r/actions/runs/abc", "", "", 0, 0, "invalid run id"},
		{"github.com/o/r/actions/runs/1", "", "", 0, 0, "invalid url"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := ParseRunURL(c.in)
			if c.errLike != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", c.errLike)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Owner != c.owner || ref.Repo != c.repo || ref.RunID != c.runID || ref.JobID != c.jobID {
				t.Fatalf("mismatch: %+v want owner=%s repo=%s run=%d job=%d", ref, c.owner, c.repo, c.runID, c.jobID)
			}
		})
	}
}
