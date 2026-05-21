package replay

import (
	"strings"
	"testing"
)

func TestApplyTrace_Off(t *testing.T) {
	body := "#!/usr/bin/env bash\nset -euo pipefail\necho hi\nfalse\n"
	got, err := applyTrace(body, TraceOff)
	if err != nil {
		t.Fatal(err)
	}
	if got != body {
		t.Fatalf("trace=off should be no-op")
	}
}

func TestApplyTrace_Strace(t *testing.T) {
	body := "#!/usr/bin/env bash\nset -euo pipefail\necho hi\nfalse\n"
	got, err := applyTrace(body, TraceStrace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "RF-TRACE-INJECTED") {
		t.Fatalf("missing marker:\n%s", got)
	}
	if !strings.Contains(got, "strace -f") {
		t.Fatalf("missing strace prefix:\n%s", got)
	}
	// idempotence
	again, _ := applyTrace(got, TraceStrace)
	if again != got {
		t.Fatalf("not idempotent")
	}
}

func TestApplyTrace_Ltrace(t *testing.T) {
	body := "#!/usr/bin/env bash\nset -euo pipefail\nfalse\n"
	got, err := applyTrace(body, TraceLtrace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ltrace -f") {
		t.Fatalf("missing ltrace prefix: %s", got)
	}
}

func TestApplyTrace_Unknown(t *testing.T) {
	if _, err := applyTrace("body", TraceMode("weird")); err == nil {
		t.Fatal("expected error")
	}
}
