package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// GoTestEvent is the shape produced by `go test -json`.
type GoTestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test,omitempty"`
	Output  string    `json:"Output,omitempty"`
	Elapsed float64   `json:"Elapsed,omitempty"`
}

// GoTestJSON parses a stream of `go test -json` events into Suites.
// One suite per package, one case per test.
func GoTestJSON(r io.Reader) (Suites, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)

	type tcKey struct{ pkg, test string }
	type tcAcc struct {
		status   TestStatus
		duration time.Duration
		out      strings.Builder
	}

	pkgs := map[string]*TestSuite{}
	cases := map[tcKey]*tcAcc{}
	order := []tcKey{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev GoTestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Package == "" {
			continue
		}
		if _, ok := pkgs[ev.Package]; !ok {
			pkgs[ev.Package] = &TestSuite{Name: ev.Package}
		}

		switch ev.Action {
		case "run", "start":
			if ev.Test != "" {
				k := tcKey{ev.Package, ev.Test}
				if _, ok := cases[k]; !ok {
					cases[k] = &tcAcc{status: StatusPassed}
					order = append(order, k)
				}
			}
		case "output":
			if ev.Test != "" {
				k := tcKey{ev.Package, ev.Test}
				if c, ok := cases[k]; ok {
					c.out.WriteString(ev.Output)
				}
			}
		case "pass":
			if ev.Test != "" {
				k := tcKey{ev.Package, ev.Test}
				if c, ok := cases[k]; ok {
					c.status = StatusPassed
					c.duration = time.Duration(ev.Elapsed * float64(time.Second))
				}
			}
		case "fail":
			if ev.Test != "" {
				k := tcKey{ev.Package, ev.Test}
				if c, ok := cases[k]; ok {
					c.status = StatusFailed
					c.duration = time.Duration(ev.Elapsed * float64(time.Second))
				}
			}
		case "skip":
			if ev.Test != "" {
				k := tcKey{ev.Package, ev.Test}
				if c, ok := cases[k]; ok {
					c.status = StatusSkipped
					c.duration = time.Duration(ev.Elapsed * float64(time.Second))
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, k := range order {
		acc := cases[k]
		s := pkgs[k.pkg]
		tc := TestCase{
			Name:       k.test,
			Classname:  k.pkg,
			Status:     acc.status,
			Duration:   acc.duration,
			System:     acc.out.String(),
			Message:    firstNonEmpty(acc.out.String()),
			StackTrace: extractGoStack(acc.out.String()),
		}
		s.Cases = append(s.Cases, tc)
		s.Total++
		switch acc.status {
		case StatusFailed:
			s.Failures++
		case StatusErrored:
			s.Errors++
		case StatusSkipped:
			s.Skipped++
		}
	}

	out := make(Suites, 0, len(pkgs))
	for _, s := range pkgs {
		out = append(out, *s)
	}
	return out, nil
}

func firstNonEmpty(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "---") {
			continue
		}
		return t
	}
	return ""
}

// extractGoStack returns the lines following a "goroutine N [..]:" header or
// the typical Go test failure marker "    file.go:NN: message".
func extractGoStack(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "--- FAIL:") {
			// next lines until blank or another --- marker
			for j := i + 1; j < len(lines); j++ {
				lj := lines[j]
				ts := strings.TrimSpace(lj)
				if ts == "" || strings.HasPrefix(ts, "---") || strings.HasPrefix(ts, "FAIL") || strings.HasPrefix(ts, "ok") {
					break
				}
				out = append(out, lj)
			}
			break
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
