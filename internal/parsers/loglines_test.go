package parsers

import (
	"strings"
	"testing"
)

func TestScanLog_Categories(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want func(s LogScan) bool
	}{
		{
			"network",
			"curl: (7) connection refused",
			func(s LogScan) bool { return len(s.NetworkHits) > 0 },
		},
		{
			"dns",
			"could not resolve host: example.com",
			func(s LogScan) bool { return len(s.DNSHits) > 0 },
		},
		{
			"tls",
			"x509: certificate has expired",
			func(s LogScan) bool { return len(s.TLSHits) > 0 },
		},
		{
			"timeout",
			"the operation has timed out\n",
			func(s LogScan) bool { return len(s.TimeoutHits) > 0 },
		},
		{
			"oom",
			"java.lang.OutOfMemoryError: Java heap space",
			func(s LogScan) bool { return len(s.OOMHits) > 0 },
		},
		{
			"perm",
			"chmod: changing permissions: Permission denied",
			func(s LogScan) bool { return len(s.PermHits) > 0 },
		},
		{
			"npm",
			"npm ERR! code E404 — version not found in registry\n",
			func(s LogScan) bool { return len(s.DepHits) > 0 },
		},
		{
			"pip",
			"ERROR: Could not find a version that satisfies the requirement leftpad",
			func(s LogScan) bool { return len(s.DepHits) > 0 },
		},
		{
			"checksum",
			"sha256 mismatch on package wheel",
			func(s LogScan) bool { return len(s.ChecksumHits) > 0 },
		},
		{
			"missing env",
			"Error: secret API_TOKEN not found",
			func(s LogScan) bool { return len(s.MissingEnv) > 0 },
		},
		{
			"yaml expr",
			"Error: Unrecognized named-value: 'matrix.invalid'",
			func(s LogScan) bool { return len(s.YAMLExpr) > 0 },
		},
		{
			"missing file",
			"cp: cannot stat 'foo': No such file or directory",
			func(s LogScan) bool { return len(s.MissingFile) > 0 },
		},
		{
			"exit code",
			"##[error]Process completed with exit code 137.\n",
			func(s LogScan) bool { return s.ExitCode == 137 && s.StepError != "" },
		},
	}
	patterns := DefaultPatterns()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ScanLog(strings.NewReader(tc.log), patterns)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.want(s) {
				t.Fatalf("unexpected scan result: %+v", s)
			}
		})
	}
}

func TestScanLog_PythonTraceback(t *testing.T) {
	in := `
Traceback (most recent call last):
  File "/runner/_work/r/r/tests/test_api.py", line 42, in test_login
    assert resp.status_code == 200
AssertionError: 500 != 200
`
	s, err := ScanLog(strings.NewReader(in), DefaultPatterns())
	if err != nil {
		t.Fatal(err)
	}
	if s.ErrorClass != "AssertionError" {
		t.Fatalf("expected AssertionError, got %q", s.ErrorClass)
	}
	if !strings.Contains(s.StackTop, "test_login") {
		t.Fatalf("stack top missing: %q", s.StackTop)
	}
	if len(s.StackBlocks) == 0 {
		t.Fatal("stack blocks not captured")
	}
}

func TestScanLog_GoPanic(t *testing.T) {
	in := "panic: runtime error: index out of range [5]"
	s, _ := ScanLog(strings.NewReader(in), DefaultPatterns())
	if s.ErrorClass != "panic" {
		t.Fatalf("expected panic, got %q", s.ErrorClass)
	}
}
