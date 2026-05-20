package parsers

import (
	"strings"
	"testing"
)

const junitWrapper = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="3" failures="1" errors="0">
  <testsuite name="tests.sample" tests="3" failures="1" errors="0" time="1.234">
    <testcase classname="tests.sample" name="test_pass" time="0.10"/>
    <testcase classname="tests.sample" name="test_fail" time="0.20">
      <failure type="AssertionError" message="x != 1">
Traceback (most recent call last):
  File "/runner/_work/repo/repo/tests/test_sample.py", line 12, in test_fail
    assert x == 1
AssertionError: x != 1
      </failure>
    </testcase>
    <testcase classname="tests.sample" name="test_skip">
      <skipped message="not for this os"/>
    </testcase>
  </testsuite>
</testsuites>`

const junitSingle = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="single" tests="1" failures="0" errors="1">
  <testcase classname="single" name="boom">
    <error type="RuntimeError" message="boom!"/>
  </testcase>
</testsuite>`

func TestJUnit_Wrapper(t *testing.T) {
	suites, err := JUnitXML(strings.NewReader(junitWrapper))
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) != 1 {
		t.Fatalf("expected 1 suite, got %d", len(suites))
	}
	s := suites[0]
	if s.Total != 3 || s.Failures != 1 || s.Skipped != 1 {
		t.Fatalf("counts wrong: %+v", s)
	}
	failed := suites.Failed()
	if len(failed) != 1 || failed[0].Name != "test_fail" {
		t.Fatalf("failed list wrong: %+v", failed)
	}
	if !strings.Contains(failed[0].StackTrace, "AssertionError") {
		t.Fatalf("stack not preserved: %s", failed[0].StackTrace)
	}
}

func TestJUnit_SingleSuite(t *testing.T) {
	suites, err := JUnitXML(strings.NewReader(junitSingle))
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) != 1 {
		t.Fatalf("expected 1 suite, got %d", len(suites))
	}
	if suites[0].Errors != 1 {
		t.Fatalf("errors not counted: %+v", suites[0])
	}
}

func TestJUnit_Empty(t *testing.T) {
	_, err := JUnitXML(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestJUnit_Malformed(t *testing.T) {
	_, err := JUnitXML(strings.NewReader("<not></valid>"))
	if err == nil {
		t.Fatal("expected error on garbage")
	}
}
