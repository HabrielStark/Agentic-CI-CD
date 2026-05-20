package parsers

import (
	"strings"
	"testing"
)

func TestGoTestJSON(t *testing.T) {
	in := `
{"Time":"2026-05-20T12:00:00Z","Action":"run","Package":"pkg/foo","Test":"TestA"}
{"Time":"2026-05-20T12:00:00Z","Action":"output","Package":"pkg/foo","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Time":"2026-05-20T12:00:00Z","Action":"output","Package":"pkg/foo","Test":"TestA","Output":"--- FAIL: TestA (0.01s)\n"}
{"Time":"2026-05-20T12:00:00Z","Action":"output","Package":"pkg/foo","Test":"TestA","Output":"    foo_test.go:42: boom\n"}
{"Time":"2026-05-20T12:00:00Z","Action":"fail","Package":"pkg/foo","Test":"TestA","Elapsed":0.01}
{"Time":"2026-05-20T12:00:00Z","Action":"run","Package":"pkg/foo","Test":"TestB"}
{"Time":"2026-05-20T12:00:00Z","Action":"pass","Package":"pkg/foo","Test":"TestB","Elapsed":0.05}
{"Time":"2026-05-20T12:00:00Z","Action":"run","Package":"pkg/bar","Test":"TestC"}
{"Time":"2026-05-20T12:00:00Z","Action":"skip","Package":"pkg/bar","Test":"TestC"}
`
	suites, err := GoTestJSON(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) != 2 {
		t.Fatalf("want 2 suites, got %d", len(suites))
	}
	failed := suites.Failed()
	if len(failed) != 1 || failed[0].Name != "TestA" {
		t.Fatalf("failed list wrong: %+v", failed)
	}
	if !strings.Contains(failed[0].StackTrace, "boom") {
		t.Fatalf("stack missing: %q", failed[0].StackTrace)
	}
}

func TestGoTestJSON_Garbage(t *testing.T) {
	_, err := GoTestJSON(strings.NewReader("not json\n"))
	if err != nil {
		t.Fatal("expected to skip garbage gracefully")
	}
}
