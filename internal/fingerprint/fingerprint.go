// Package fingerprint computes stable identifiers for failures so that
// recurring problems can be detected across runs without leaking secret
// content. It distils a failure into a normalised tuple
// (top-of-stack frame, error class, package/test name, command).
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

// Input gathers everything we need to build a fingerprint.
type Input struct {
	Command       string
	ExitCode      int
	Step          string
	FailingTests  []string
	ErrorClass    string   // e.g. "AssertionError", "ENOENT", "exit 137"
	StackTopFrame string   // e.g. "tests/test_api.py:42 test_login"
	DepPackages   []string // e.g. ["lodash", "react@18.2.0"]
	Provider      string
	OS            string
}

// Compute returns "sha256:<hex>" given the input. Empty fields are normalised
// so that incidental differences (timestamps, run ids, paths) do not affect
// the hash.
func Compute(in Input) string {
	parts := []string{
		"v1",
		"prov=" + strings.ToLower(strings.TrimSpace(in.Provider)),
		"os=" + strings.ToLower(strings.TrimSpace(in.OS)),
		"step=" + normaliseStep(in.Step),
		"cmd=" + normaliseCommand(in.Command),
		"exit=" + itoa(in.ExitCode),
		"err=" + normaliseError(in.ErrorClass),
		"frame=" + normaliseFrame(in.StackTopFrame),
		"tests=" + normaliseList(in.FailingTests),
		"deps=" + normaliseList(in.DepPackages),
	}
	joined := strings.Join(parts, "\n")
	sum := sha256.Sum256([]byte(joined))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func itoa(i int) string {
	// avoid strconv import dance for compactness; covers full int range.
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	bp := len(b)
	for i > 0 {
		bp--
		b[bp] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		bp--
		b[bp] = '-'
	}
	return string(b[bp:])
}

var (
	hexLong   = regexp.MustCompile(`\b[0-9a-fA-F]{12,}\b`)
	digitsLong = regexp.MustCompile(`\b\d{4,}\b`)
	tmpPath   = regexp.MustCompile(`/tmp/[^\s'"]+|/var/folders/[^\s'"]+|C:\\Users\\[^\s]+\\AppData\\Local\\Temp\\[^\s]+`)
	uuidLike  = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	wsRun     = regexp.MustCompile(`\s+`)
	colonNums = regexp.MustCompile(`:\d+`)
)

func normaliseCommand(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = hexLong.ReplaceAllString(s, "<hex>")
	s = uuidLike.ReplaceAllString(s, "<uuid>")
	s = digitsLong.ReplaceAllString(s, "<num>")
	s = tmpPath.ReplaceAllString(s, "<tmp>")
	s = wsRun.ReplaceAllString(s, " ")
	return strings.ToLower(s)
}

func normaliseStep(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToLower(wsRun.ReplaceAllString(s, " "))
}

func normaliseError(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, " ", "_")
}

// normaliseFrame collapses absolute paths and per-run ids in a frame string.
// e.g. "/runner/_work/foo/foo/tests/test_api.py:42 in test_login" -> "tests/test_api.py:N test_login"
func normaliseFrame(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// strip common CI runner prefixes
	for _, p := range []string{
		"/home/runner/work/", "/runner/_work/", "/Users/runner/work/",
		"D:\\a\\", "/github/workspace/",
	} {
		if i := strings.Index(s, p); i >= 0 {
			s = s[i+len(p):]
			// remove the leading "<repo>/<repo>/" duplicate that Actions creates
			parts := strings.SplitN(s, "/", 3)
			if len(parts) == 3 && parts[0] == parts[1] {
				s = parts[2]
			}
			break
		}
	}
	s = uuidLike.ReplaceAllString(s, "<uuid>")
	s = hexLong.ReplaceAllString(s, "<hex>")
	s = colonNums.ReplaceAllString(s, ":N")
	return strings.ToLower(strings.TrimSpace(s))
}

func normaliseList(items []string) string {
	cp := append([]string(nil), items...)
	for i, v := range cp {
		cp[i] = strings.ToLower(strings.TrimSpace(v))
	}
	sort.Strings(cp)
	return strings.Join(cp, ",")
}
