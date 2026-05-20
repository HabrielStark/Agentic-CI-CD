package replay

import (
	"testing"
)

func TestParseHumanBytes(t *testing.T) {
	cases := map[string]int64{
		"1.0kB": 1000,
		"1MiB":  1 << 20,
		"2GiB":  2 * (1 << 30),
		"100B":  100,
		"":       0,
		"--":     0,
		"42":     42,
	}
	for in, want := range cases {
		if got := parseHumanBytes(in); got != want {
			t.Fatalf("parseHumanBytes(%q)=%d want %d", in, got, want)
		}
	}
}

func TestParsePct(t *testing.T) {
	cases := map[string]float64{
		"23.45%": 23.45,
		"0%":     0,
		"--":     0,
		"":       0,
	}
	for in, want := range cases {
		if got := parsePct(in); got != want {
			t.Fatalf("parsePct(%q)=%f want %f", in, got, want)
		}
	}
}

func TestParsePairKB(t *testing.T) {
	rx, tx := parsePairKB("1.2kB / 2.5kB")
	if rx == 0 || tx == 0 {
		t.Fatalf("unexpected: rx=%f tx=%f", rx, tx)
	}
}
