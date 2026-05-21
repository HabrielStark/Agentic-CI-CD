package buildkite

import (
	"testing"
)

func TestParse(t *testing.T) {
	a := &Adapter{}
	cases := []struct {
		in      string
		org     string
		pipe    string
		num     int64
		wantErr bool
	}{
		{"https://buildkite.com/myorg/my-pipeline/builds/42", "myorg", "my-pipeline", 42, false},
		{"https://buildkite.com/a/b/builds/100", "a", "b", 100, false},
		{"https://github.com/x/y/actions/runs/1", "", "", 0, true},
		{"", "", "", 0, true},
		{"not a url", "", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := a.Parse(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ref.Owner != c.org || ref.Repo != c.pipe || ref.RunID != c.num {
				t.Fatalf("got %+v", ref)
			}
		})
	}
}
