package parsers

import (
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

// JUnitXML parses a JUnit XML stream. It accepts both a single <testsuite>
// root and a <testsuites> wrapper.
func JUnitXML(r io.Reader) (Suites, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty junit xml")
	}
	// Try <testsuites> wrapper first.
	var ts xmlTestSuites
	if err := xml.Unmarshal(data, &ts); err == nil && (len(ts.Suites) > 0 || ts.Tests > 0) {
		return convertSuites(ts.Suites), nil
	}
	// Fall back to single <testsuite>.
	var single xmlTestSuite
	if err := xml.Unmarshal(data, &single); err != nil {
		return nil, errorf("parse junit xml: %w", err)
	}
	return convertSuites([]xmlTestSuite{single}), nil
}

type xmlTestSuites struct {
	XMLName xml.Name        `xml:"testsuites"`
	Tests   int             `xml:"tests,attr"`
	Suites  []xmlTestSuite  `xml:"testsuite"`
}

type xmlTestSuite struct {
	XMLName  xml.Name      `xml:"testsuite"`
	Name     string        `xml:"name,attr"`
	Tests    int           `xml:"tests,attr"`
	Failures int           `xml:"failures,attr"`
	Errors   int           `xml:"errors,attr"`
	Skipped  int           `xml:"skipped,attr"`
	Time     string        `xml:"time,attr"`
	File     string        `xml:"file,attr"`
	Cases    []xmlTestCase `xml:"testcase"`
}

type xmlTestCase struct {
	XMLName   xml.Name      `xml:"testcase"`
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	File      string        `xml:"file,attr"`
	Line      int           `xml:"line,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *xmlFailure   `xml:"failure"`
	Error     *xmlFailure   `xml:"error"`
	Skipped   *xmlSkipped   `xml:"skipped"`
	SystemOut string        `xml:"system-out"`
	SystemErr string        `xml:"system-err"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type xmlSkipped struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func convertSuites(in []xmlTestSuite) Suites {
	var out Suites
	for _, x := range in {
		s := TestSuite{
			Name:     x.Name,
			Total:    x.Tests,
			Failures: x.Failures,
			Errors:   x.Errors,
			Skipped:  x.Skipped,
			Duration: parseDur(x.Time),
		}
		// recompute counts because not all producers fill the attrs
		var f, e, sk int
		for _, c := range x.Cases {
			tc := TestCase{
				Name:      c.Name,
				Classname: c.Classname,
				File:      c.File,
				Line:      c.Line,
				Duration:  parseDur(c.Time),
				System:    strings.TrimSpace(c.SystemOut),
				ErrSystem: strings.TrimSpace(c.SystemErr),
				Status:    StatusPassed,
			}
			switch {
			case c.Error != nil:
				tc.Status = StatusErrored
				tc.Message = c.Error.Message
				tc.StackTrace = strings.TrimSpace(c.Error.Body)
				e++
			case c.Failure != nil:
				tc.Status = StatusFailed
				tc.Message = c.Failure.Message
				tc.StackTrace = strings.TrimSpace(c.Failure.Body)
				f++
			case c.Skipped != nil:
				tc.Status = StatusSkipped
				tc.Message = c.Skipped.Message
				sk++
			}
			s.Cases = append(s.Cases, tc)
		}
		if s.Total == 0 {
			s.Total = len(x.Cases)
		}
		if s.Failures == 0 {
			s.Failures = f
		}
		if s.Errors == 0 {
			s.Errors = e
		}
		if s.Skipped == 0 {
			s.Skipped = sk
		}
		out = append(out, s)
	}
	return out
}

func parseDur(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}
