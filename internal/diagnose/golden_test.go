package diagnose

import (
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/parsers"
)

// Golden-style: a curated table of (input log + suites) → expected diagnosis
// category. This is the core of the rule-based classifier and changes here
// should be deliberate.
func TestGolden(t *testing.T) {
	patterns := parsers.DefaultPatterns()
	cases := []struct {
		name        string
		log         string
		suites      parsers.Suites
		wantCat     string
		mustContain []string
	}{
		{
			name: "pip dependency resolution",
			log:  "ERROR: Could not find a version that satisfies the requirement leftpad\nERROR: No matching distribution found for leftpad",
			wantCat: CatDependency,
			mustContain: []string{"leftpad"},
		},
		{
			name: "go module checksum",
			log:  "verifying github.com/foo/bar: checksum mismatch\ngo: downloading github.com/foo/bar v1.0.0",
			wantCat: CatChecksum,
		},
		{
			name: "missing secret",
			log:  "Error: secret API_TOKEN not found in environment",
			wantCat: CatMissingSecret,
		},
		{
			name: "oom kill",
			log:  "container killed due to out of memory",
			wantCat: CatOOM,
		},
		{
			name: "test failure with junit",
			log:  "##[error]Process completed with exit code 1.",
			suites: parsers.Suites{{Name: "x", Total: 1, Failures: 1, Cases: []parsers.TestCase{{Name: "boom", Status: parsers.StatusFailed, Message: "kaboom"}}}},
			wantCat: CatCodeOrTest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan, err := parsers.ScanLog(strings.NewReader(tc.log), patterns)
			if err != nil {
				t.Fatal(err)
			}
			d := Classify(Input{LogScan: scan, Suites: tc.suites, StepName: "x", Command: "make test"})
			if d.Category != tc.wantCat {
				t.Fatalf("category=%s want %s\nevidence=%v", d.Category, tc.wantCat, d.Evidence)
			}
			joined := strings.Join(d.Evidence, " ")
			for _, frag := range tc.mustContain {
				if !strings.Contains(joined, frag) {
					t.Fatalf("evidence missing %q: %v", frag, d.Evidence)
				}
			}
		})
	}
}
