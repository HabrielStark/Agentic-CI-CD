package flake

import "testing"

func TestNowSeed_Variations(t *testing.T) {
	a := nowSeed(0)
	b := nowSeed(1)
	if a == "" || b == "" {
		t.Fatal("empty seed")
	}
	if a == b {
		// extremely unlikely in the same nanosecond, but acceptable; we just make sure seeds are short numerics
		t.Logf("seeds collided: %s/%s", a, b)
	}
	for _, s := range []string{a, b} {
		for _, c := range s {
			if c < '0' || c > '9' {
				t.Fatalf("seed not numeric: %q", s)
			}
		}
	}
}

func TestSummaryIsFlaky(t *testing.T) {
	s := Summary{Passed: 0, Failed: 5}
	if s.IsFlaky() {
		t.Fatal("should not be flaky (no passes)")
	}
	s = Summary{Passed: 3, Failed: 2}
	if !s.IsFlaky() {
		t.Fatal("should be flaky")
	}
}
