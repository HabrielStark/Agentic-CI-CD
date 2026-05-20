package diagnose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reproforge/reproforge/internal/parsers"
)

// TestDiagnose_AgainstFixtures runs the rule-based classifier against the
// curated fixtures under /fixtures and asserts the expected category. This is
// our integration smoke test for the full rule set.
func TestDiagnose_AgainstFixtures(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		dir      string
		junit    string
		wantCat  string
		wantTest string // optional: must appear in d.Tests
	}{
		{"python-deterministic", "junit-results.xml", CatCodeOrTest, "test_add_is_correct"},
		{"node-flaky", "", CatCodeOrTest, ""},
		{"go-network", "", CatNetwork, ""},
		{"missing-secret", "", CatMissingSecret, ""},
	}
	patterns := parsers.DefaultPatterns()

	for _, c := range cases {
		c := c
		t.Run(c.dir, func(t *testing.T) {
			logBytes, err := os.ReadFile(filepath.Join(root, "fixtures", c.dir, "job.log"))
			if err != nil {
				t.Fatal(err)
			}
			scan, err := parsers.ScanLog(strings.NewReader(string(logBytes)), patterns)
			if err != nil {
				t.Fatal(err)
			}
			var suites parsers.Suites
			if c.junit != "" {
				b, err := os.ReadFile(filepath.Join(root, "fixtures", c.dir, c.junit))
				if err != nil {
					t.Fatal(err)
				}
				suites, err = parsers.JUnitXML(strings.NewReader(string(b)))
				if err != nil {
					t.Fatal(err)
				}
			}
			d := Classify(Input{LogScan: scan, Suites: suites, StepName: "Run tests", OS: "ubuntu-24.04", Provider: "github_actions"})
			if d.Category != c.wantCat {
				t.Fatalf("[%s] category=%s want %s\nevidence=%v", c.dir, d.Category, c.wantCat, d.Evidence)
			}
			if c.wantTest != "" {
				found := false
				for _, n := range d.Tests {
					if strings.Contains(n, c.wantTest) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("[%s] test %q not in %v", c.dir, c.wantTest, d.Tests)
				}
			}
		})
	}
}

// repoRoot walks up from the test file location until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not locate repo root from %s", file)
	return ""
}
