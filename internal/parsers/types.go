// Package parsers contains parsers for CI logs and standard test report
// formats. Each parser returns a normalised TestSuite/TestCase representation.
package parsers

import (
	"fmt"
	"strings"
	"time"
)

// TestStatus represents a normalised test result status.
type TestStatus string

// Test statuses.
const (
	StatusPassed  TestStatus = "passed"
	StatusFailed  TestStatus = "failed"
	StatusSkipped TestStatus = "skipped"
	StatusErrored TestStatus = "errored"
)

// TestCase is a single test case.
type TestCase struct {
	Name        string        `json:"name"`
	Classname   string        `json:"classname,omitempty"`
	File        string        `json:"file,omitempty"`
	Line        int           `json:"line,omitempty"`
	Status      TestStatus    `json:"status"`
	Duration    time.Duration `json:"durationNs"`
	Message     string        `json:"message,omitempty"`
	StackTrace  string        `json:"stackTrace,omitempty"`
	System      string        `json:"systemOut,omitempty"`
	ErrSystem   string        `json:"systemErr,omitempty"`
}

// FullName returns a stable identifier for a test case.
func (tc *TestCase) FullName() string {
	if tc.Classname != "" {
		return tc.Classname + "::" + tc.Name
	}
	return tc.Name
}

// TestSuite groups test cases.
type TestSuite struct {
	Name      string        `json:"name"`
	Cases     []TestCase    `json:"cases"`
	Failures  int           `json:"failures"`
	Errors    int           `json:"errors"`
	Skipped   int           `json:"skipped"`
	Total     int           `json:"total"`
	Duration  time.Duration `json:"durationNs"`
}

// Suites is a slice of suites. It exposes convenience accessors used by callers.
type Suites []TestSuite

// Failed returns all failed/errored test cases across suites.
func (s Suites) Failed() []TestCase {
	var out []TestCase
	for _, su := range s {
		for _, tc := range su.Cases {
			if tc.Status == StatusFailed || tc.Status == StatusErrored {
				out = append(out, tc)
			}
		}
	}
	return out
}

// Stats returns aggregated counts.
func (s Suites) Stats() (total, failed, errored, skipped int) {
	for _, su := range s {
		total += su.Total
		failed += su.Failures
		errored += su.Errors
		skipped += su.Skipped
	}
	return
}

// FormatName returns a human friendly suite name from a file path.
func FormatName(path string) string {
	if path == "" {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// errorf returns a formatted error.
func errorf(f string, a ...any) error { return fmt.Errorf(f, a...) }
