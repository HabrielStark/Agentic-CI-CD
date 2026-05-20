package parsers

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// LogPatterns holds compiled regexes used by the rule-based diagnoser.
type LogPatterns struct {
	NetworkErr     *regexp.Regexp
	DNSErr         *regexp.Regexp
	TLSErr         *regexp.Regexp
	TimeoutErr     *regexp.Regexp
	OOM            *regexp.Regexp
	Permission     *regexp.Regexp
	NPMResolve     *regexp.Regexp
	PipResolve     *regexp.Regexp
	GoModResolve   *regexp.Regexp
	MavenResolve   *regexp.Regexp
	GradleResolve  *regexp.Regexp
	ChecksumErr    *regexp.Regexp
	MissingEnv     *regexp.Regexp
	MissingFile    *regexp.Regexp
	YAMLExpr       *regexp.Regexp
	ExitCodeRe     *regexp.Regexp
	StepFailure    *regexp.Regexp
	PythonTraceback *regexp.Regexp
	NodeStack      *regexp.Regexp
	GoPanic        *regexp.Regexp
	JavaException  *regexp.Regexp
}

// DefaultPatterns returns the compiled rule set used by all packages.
func DefaultPatterns() LogPatterns {
	return LogPatterns{
		NetworkErr: regexp.MustCompile(`(?i)\b(?:ECONNRESET|ECONNREFUSED|EAI_AGAIN|ETIMEDOUT|EHOSTUNREACH|ENETUNREACH|connection (?:reset|refused|timed? out)|temporary failure in name resolution|network is unreachable)\b`),
		DNSErr:     regexp.MustCompile(`(?i)\b(?:getaddrinfo|EAI_AGAIN|name or service not known|could not resolve host|dns lookup failed|no address associated)\b`),
		TLSErr:     regexp.MustCompile(`(?i)\b(?:certificate (?:has expired|is not valid|verify failed)|x509: certificate|tls handshake|self[- ]signed certificate|unable to verify the first certificate)\b`),
		TimeoutErr: regexp.MustCompile(`(?i)\b(?:timed? out after|deadline exceeded|context deadline|the operation has timed out|operation timeout|exceeded the maximum execution time)\b`),
		OOM:        regexp.MustCompile(`(?i)\b(?:out of memory|killed.*signal: killed|java.lang.OutOfMemoryError|fatal error: runtime: out of memory|cannot allocate memory|MemoryError)\b`),
		Permission: regexp.MustCompile(`(?i)\b(?:permission denied|EACCES|operation not permitted|access is denied|forbidden \(403\))\b`),
		NPMResolve: regexp.MustCompile(`(?i)(?:npm ERR! code (?:E404|ETARGET|ERESOLVE|EUNSUPPORTEDPROTOCOL|EBADENGINE|ENOTFOUND)|peer dep missing|cannot resolve dependency|404 not found.*npm|version not found in registry)`),
		PipResolve: regexp.MustCompile(`(?i)(?:could not find a version|no matching distribution|error: cannot install|distribution not found|hash mismatch|resolutionimpossible|packagenotfounderror)`),
		GoModResolve: regexp.MustCompile(`(?i)(?:go: missing go.sum entry|missing go\.sum entry for module|go: github.com.*: cannot find|module .* not found|verify\.sum|checksum mismatch)`),
		MavenResolve: regexp.MustCompile(`(?i)(?:Could not resolve dependencies|Could not find artifact|Failure to find|maven-resolver|return code is: 401|peer not authenticated)`),
		GradleResolve: regexp.MustCompile(`(?i)(?:Could not resolve|FAILURE: Build failed|Could not GET|Could not download|gradle dependency)`),
		ChecksumErr:  regexp.MustCompile(`(?i)\b(?:integrity check failed|sha(?:1|256|512) mismatch|checksum verification failed|checksum mismatch|hash sum mismatch|corrupt(?:ed)? archive)\b`),
		MissingEnv:   regexp.MustCompile(`(?i)(?:environment variable .* (?:not set|missing|required)|missing (?:required )?env(?:ironment)? variable|secret .* not found|secrets\.\w+ not found)`),
		MissingFile:  regexp.MustCompile(`(?i)(?:no such file or directory|cannot find the file|enoent: no such file|file not found:)`),
		YAMLExpr:     regexp.MustCompile(`(?i)(?:Unrecognized named-value|Invalid step expression|Invalid workflow file|expression is invalid)`),
		ExitCodeRe:   regexp.MustCompile(`(?i)Process completed with exit code (\d+)|Error: Process completed with exit code (\d+)`),
		StepFailure:  regexp.MustCompile(`^##\[error\](.*)`),
		PythonTraceback: regexp.MustCompile(`^Traceback \(most recent call last\):`),
		NodeStack:    regexp.MustCompile(`(?m)^\s+at .+ \(.+:\d+:\d+\)$`),
		GoPanic:      regexp.MustCompile(`^panic: `),
		JavaException: regexp.MustCompile(`(?:Exception in thread|Caused by:|java\.[\w.]+Exception)`),
	}
}

// LogScan is the result of scanning a log file for diagnostic anchors.
type LogScan struct {
	ExitCode    int
	StepError   string
	NetworkHits []string
	DNSHits     []string
	TLSHits     []string
	TimeoutHits []string
	OOMHits     []string
	PermHits    []string
	DepHits     []string // npm/pip/go/maven/gradle resolution markers
	ChecksumHits []string
	MissingEnv  []string
	MissingFile []string
	YAMLExpr    []string
	StackTop    string
	ErrorClass  string
	StackBlocks []string
	RawTail     []string // last 200 non-empty lines
}

// ScanLog reads a log stream and returns indicative findings.
func ScanLog(r io.Reader, p LogPatterns) (LogScan, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var scan LogScan
	tail := make([]string, 0, 256)
	inPyTraceback := false
	pyBlock := []string{}
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		if t == "" {
			if inPyTraceback && len(pyBlock) > 0 {
				scan.StackBlocks = append(scan.StackBlocks, strings.Join(pyBlock, "\n"))
				pyBlock = pyBlock[:0]
				inPyTraceback = false
			}
			continue
		}
		// keep tail
		if len(tail) >= cap(tail) {
			tail = append(tail[1:], line)
		} else {
			tail = append(tail, line)
		}

		if m := p.ExitCodeRe.FindStringSubmatch(line); m != nil {
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					if v, err := atoi(m[i]); err == nil {
						scan.ExitCode = v
					}
				}
			}
		}
		if m := p.StepFailure.FindStringSubmatch(line); m != nil && scan.StepError == "" {
			scan.StepError = strings.TrimSpace(m[1])
		}
		if p.NetworkErr.MatchString(line) {
			scan.NetworkHits = appendCap(scan.NetworkHits, line, 16)
		}
		if p.DNSErr.MatchString(line) {
			scan.DNSHits = appendCap(scan.DNSHits, line, 16)
		}
		if p.TLSErr.MatchString(line) {
			scan.TLSHits = appendCap(scan.TLSHits, line, 16)
		}
		if p.TimeoutErr.MatchString(line) {
			scan.TimeoutHits = appendCap(scan.TimeoutHits, line, 16)
		}
		if p.OOM.MatchString(line) {
			scan.OOMHits = appendCap(scan.OOMHits, line, 16)
		}
		if p.Permission.MatchString(line) {
			scan.PermHits = appendCap(scan.PermHits, line, 16)
		}
		if p.NPMResolve.MatchString(line) ||
			p.PipResolve.MatchString(line) ||
			p.GoModResolve.MatchString(line) ||
			p.MavenResolve.MatchString(line) ||
			p.GradleResolve.MatchString(line) {
			scan.DepHits = appendCap(scan.DepHits, line, 16)
		}
		if p.ChecksumErr.MatchString(line) {
			scan.ChecksumHits = appendCap(scan.ChecksumHits, line, 16)
		}
		if p.MissingEnv.MatchString(line) {
			scan.MissingEnv = appendCap(scan.MissingEnv, line, 16)
		}
		if p.MissingFile.MatchString(line) {
			scan.MissingFile = appendCap(scan.MissingFile, line, 16)
		}
		if p.YAMLExpr.MatchString(line) {
			scan.YAMLExpr = appendCap(scan.YAMLExpr, line, 16)
		}
		if p.PythonTraceback.MatchString(line) {
			inPyTraceback = true
			pyBlock = append(pyBlock[:0], line)
			continue
		}
		if inPyTraceback {
			pyBlock = append(pyBlock, line)
			// detect end via final exception line
			if regexp.MustCompile(`^[A-Z][\w\.]+(?:Error|Exception)(?::|$)`).MatchString(t) {
				scan.StackBlocks = append(scan.StackBlocks, strings.Join(pyBlock, "\n"))
				if scan.ErrorClass == "" {
					if i := strings.IndexByte(t, ':'); i > 0 {
						scan.ErrorClass = t[:i]
					} else {
						scan.ErrorClass = t
					}
				}
				if scan.StackTop == "" {
					scan.StackTop = topPyFrame(pyBlock)
				}
				pyBlock = pyBlock[:0]
				inPyTraceback = false
			}
			continue
		}
		if p.GoPanic.MatchString(line) {
			scan.ErrorClass = "panic"
		}
		if p.JavaException.MatchString(line) && scan.ErrorClass == "" {
			scan.ErrorClass = "JavaException"
		}
		if p.NodeStack.MatchString(line) && scan.StackTop == "" {
			scan.StackTop = strings.TrimSpace(line)
		}
		// Generic JS / Vitest / Jest assertion error detection.
		if strings.Contains(line, "AssertionError:") ||
			strings.Contains(line, "TypeError:") ||
			strings.Contains(line, "ReferenceError:") {
			if scan.ErrorClass == "" {
				if i := strings.Index(line, ":"); i > 0 {
					if j := strings.LastIndex(line[:i], " "); j >= 0 {
						scan.ErrorClass = strings.TrimSpace(line[j+1 : i])
					} else {
						scan.ErrorClass = strings.TrimSpace(line[:i])
					}
				}
			}
		}
		// FAIL  test/...  (Vitest/Jest style)
		if (strings.HasPrefix(t, "FAIL ") || strings.HasPrefix(t, "FAIL\t")) && scan.StackTop == "" {
			scan.StackTop = t
		}
	}
	if err := sc.Err(); err != nil {
		return scan, err
	}
	scan.RawTail = tail
	return scan, nil
}

// topPyFrame extracts the deepest "File ..., line N, in name" frame.
func topPyFrame(block []string) string {
	re := regexp.MustCompile(`File "([^"]+)", line (\d+), in (\S+)`)
	var last string
	for _, l := range block {
		if m := re.FindStringSubmatch(l); m != nil {
			last = m[1] + ":" + m[2] + " " + m[3]
		}
	}
	return last
}

func appendCap(slice []string, v string, cap int) []string {
	if len(slice) >= cap {
		return slice
	}
	return append(slice, v)
}

func atoi(s string) (int, error) {
	n := 0
	for i, c := range s {
		if c < '0' || c > '9' {
			return 0, errorf("not a number at %d: %q", i, s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
