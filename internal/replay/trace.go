package replay

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// TraceMode controls FR-032 syscall/network tracing.
type TraceMode string

// Trace mode constants.
const (
	TraceOff     TraceMode = ""
	TraceStrace  TraceMode = "strace"   // strace -f
	TraceLtrace  TraceMode = "ltrace"   // ltrace -f
)

// installTracePrefix returns the command prefix that should be prepended
// to the failed-step inside the container for the requested mode. It also
// reports whether the binary needs to be installed in the image.
//
// strace -f -e trace=network,file -o /work/replay/strace.log -- <cmd>
// ltrace -f -o /work/replay/ltrace.log -- <cmd>
func installTracePrefix(mode TraceMode) (cmd string, install string, err error) {
	switch mode {
	case TraceOff:
		return "", "", nil
	case TraceStrace:
		return `strace -f -e trace=%network,%file -o /work/replay/strace.log --`,
			"apt-get install -y --no-install-recommends strace || apk add --no-cache strace || true",
			nil
	case TraceLtrace:
		return `ltrace -f -o /work/replay/ltrace.log --`,
			"apt-get install -y --no-install-recommends ltrace || apk add --no-cache ltrace || true",
			nil
	}
	return "", "", fmt.Errorf("unknown trace mode %q", mode)
}

// applyTrace mutates the failed-step.sh body so the failed command runs
// under strace/ltrace. It is idempotent.
func applyTrace(body string, mode TraceMode) (string, error) {
	prefix, install, err := installTracePrefix(mode)
	if err != nil {
		return body, err
	}
	if prefix == "" {
		return body, nil
	}
	if strings.Contains(body, "# RF-TRACE-INJECTED") {
		return body, nil
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines)+3)
	for _, l := range lines {
		out = append(out, l)
		if strings.HasPrefix(l, "set -euo pipefail") {
			out = append(out, "# RF-TRACE-INJECTED")
			out = append(out, install)
		}
	}
	// Find the last non-empty line and prefix it with the tracer.
	for i := len(out) - 1; i >= 0; i-- {
		t := strings.TrimSpace(out[i])
		if t == "" || strings.HasPrefix(t, "#") || t == "exit 1" {
			continue
		}
		if strings.HasPrefix(t, "echo ") {
			continue
		}
		out[i] = prefix + " " + out[i]
		break
	}
	return strings.Join(out, "\n"), nil
}

// CheckTraceAvailable verifies the host has strace/ltrace; surfaces a
// helpful error to callers who pass --trace from the CLI on a host where
// the binary is absent (the in-container install steps still try).
func CheckTraceAvailable(mode TraceMode) error {
	switch mode {
	case TraceStrace:
		if _, err := exec.LookPath("strace"); err != nil {
			return errors.New("strace not found on host (the replay container will try to apt-get/apk install it)")
		}
	case TraceLtrace:
		if _, err := exec.LookPath("ltrace"); err != nil {
			return errors.New("ltrace not found on host (the replay container will try to install it)")
		}
	}
	return nil
}
