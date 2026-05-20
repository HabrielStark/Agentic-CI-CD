package logx

import (
	"bytes"
	"strings"
	"testing"
)

func TestLevelsAndOrdering(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, LevelInfo, false)
	l.Debug("hidden")
	l.Info("hello", "k", 1)
	l.Warn("warn")
	l.Error("err")
	got := out.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("debug should be filtered: %s", got)
	}
	for _, want := range []string{"INFO hello k=1", "WARN warn", "ERROR err"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestJSONOutput(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, LevelDebug, true)
	l = l.With("sticky", "value")
	l.Info("hello")
	body := out.String()
	if !strings.Contains(body, `"msg":"hello"`) || !strings.Contains(body, `"sticky":"value"`) {
		t.Fatalf("JSON format wrong: %s", body)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug": LevelDebug, "DEBUG": LevelDebug,
		"info": LevelInfo, "warn": LevelWarn, "warning": LevelWarn,
		"error": LevelError, "": LevelInfo, "weird": LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Fatalf("ParseLevel(%q)=%d want %d", in, got, want)
		}
	}
}

func TestSetLevelToggle(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, LevelError, false)
	if l.Level() != LevelError {
		t.Fatal("level not stored")
	}
	l.SetLevel(LevelDebug)
	l.Debug("seen now")
	if !strings.Contains(out.String(), "seen now") {
		t.Fatalf("debug should appear after SetLevel: %s", out.String())
	}
}
