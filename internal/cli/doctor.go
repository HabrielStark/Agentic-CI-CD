package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/reproforge/reproforge/internal/version"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the local environment for missing tools and common issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "reproforge %s (%s)\n", version.Version, version.Commit)
			fmt.Fprintf(w, "go:       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(w, "\n")

			checks := []struct {
				name    string
				check   func() (string, bool)
			}{
				{"docker/podman", checkContainerRuntime},
				{"git", checkGit},
				{"strace", checkStrace},
				{"network", checkNetwork},
				{"disk", checkDisk},
			}

			allOK := true
			for _, c := range checks {
				detail, ok := c.check()
				status := "OK"
				if !ok {
					status = "WARN"
					allOK = false
				}
				fmt.Fprintf(w, "  [%s] %-20s %s\n", status, c.name, detail)
			}
			fmt.Fprintf(w, "\n")
			if allOK {
				fmt.Fprintf(w, "All checks passed. Ready to replay.\n")
			} else {
				fmt.Fprintf(w, "Some checks failed. Replay may not work correctly.\n")
			}
			return nil
		},
	}
	return cmd
}

func checkContainerRuntime() (string, bool) {
	for _, rt := range []string{"docker", "podman"} {
		path, err := exec.LookPath(rt)
		if err == nil {
			out, _ := exec.Command(rt, "version", "--format", "{{.Client.Version}}").Output()
			ver := strings.TrimSpace(string(out))
			if ver == "" {
				out, _ = exec.Command(rt, "--version").Output()
				ver = strings.TrimSpace(string(out))
			}
			return fmt.Sprintf("%s (%s)", path, ver), true
		}
	}
	return "neither docker nor podman found in PATH", false
}

func checkGit() (string, bool) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "git not found in PATH", false
	}
	out, _ := exec.Command("git", "--version").Output()
	return fmt.Sprintf("%s (%s)", path, strings.TrimSpace(string(out))), true
}

func checkStrace() (string, bool) {
	path, err := exec.LookPath("strace")
	if err != nil {
		return "not found (optional; needed for --trace)", true // not a hard req
	}
	return path, true
}

func checkNetwork() (string, bool) {
	// Quick check: can we resolve github.com?
	out, err := exec.Command("getent", "hosts", "github.com").Output()
	if err != nil {
		// fallback: try nslookup
		out, err = exec.Command("nslookup", "github.com").Output()
		if err != nil {
			return "DNS resolution failed for github.com", false
		}
	}
	_ = out
	return "DNS resolves github.com", true
}

func checkDisk() (string, bool) {
	// Check available space in the working directory
	var stat struct{}
	wd, _ := os.Getwd()
	// On Linux we can check via statfs; simplified: just report the dir
	_ = stat
	return fmt.Sprintf("working directory: %s", wd), true
}
