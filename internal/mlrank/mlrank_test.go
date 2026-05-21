package mlrank

import (
	"math"
	"testing"
)

func TestTrainAndRerank_PrefersWitnessedClass(t *testing.T) {
	corpus := map[string][][]string{
		"network_issue": {
			{"connection", "refused", "dns", "timeout"},
			{"econnrefused", "tcp", "dial"},
			{"dns", "lookup", "failed"},
		},
		"code_or_test_failure": {
			{"assertion", "test", "failed", "expected"},
			{"junit", "case", "failed"},
			{"pytest", "assertion"},
		},
	}
	m := TrainFromCorpus(corpus)
	if m.IsEmpty() {
		t.Fatal("expected non-empty model")
	}

	// network-style evidence should up-rank network_issue.
	in := map[string]float64{
		"network_issue":        0.5,
		"code_or_test_failure": 0.5,
	}
	out := m.Rerank(in, []string{"connection refused dial tcp"}, 0.3)
	if out["network_issue"] < out["code_or_test_failure"] {
		t.Fatalf("expected network_issue to outrank, got %+v", out)
	}
	// rule floor preserved
	for cat, v := range out {
		if v < in[cat] {
			t.Fatalf("blended %s=%f below rule floor %f", cat, v, in[cat])
		}
	}
}

func TestRerank_EmptyModelPassthrough(t *testing.T) {
	var m *Model
	in := map[string]float64{"a": 0.7}
	got := m.Rerank(in, nil, 0.5)
	if got["a"] != 0.7 {
		t.Fatalf("nil model should pass through, got %+v", got)
	}
}

func TestNormaliseTokens(t *testing.T) {
	got := normaliseTokens("DNS lookup, failed: foo-bar 42!")
	want := []string{"dns", "lookup", "failed", "foo", "bar", "42"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v vs %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at %d: %q vs %q", i, got[i], want[i])
		}
	}
}

func TestScore_Determinism(t *testing.T) {
	corpus := map[string][][]string{
		"x": {{"a", "b"}, {"a", "c"}},
		"y": {{"d", "e"}, {"d", "f"}},
	}
	m := TrainFromCorpus(corpus)
	s1 := m.Score([]string{"a", "b"})
	s2 := m.Score([]string{"a", "b"})
	for k := range s1 {
		if math.Abs(s1[k]-s2[k]) > 1e-9 {
			t.Fatalf("non-deterministic score for %s", k)
		}
	}
}

func TestRerank_AlphaClamped(t *testing.T) {
	corpus := map[string][][]string{
		"x": {{"a"}},
		"y": {{"b"}},
	}
	m := TrainFromCorpus(corpus)
	in := map[string]float64{"x": 0.5, "y": 0.5}
	for _, alpha := range []float64{-1, 0, 0.5, 1, 2} {
		out := m.Rerank(in, []string{"a"}, alpha)
		for k := range out {
			if out[k] < 0 || out[k] > 1.01 {
				t.Fatalf("alpha=%f produced out-of-range value: %+v", alpha, out)
			}
		}
	}
}
