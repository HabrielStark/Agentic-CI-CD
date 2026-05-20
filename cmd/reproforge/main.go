// Command reproforge is the CLI for ReproForge CI.
//
// Usage:
//
//	reproforge from-run <github-actions-run-url>
//	reproforge collect --url <run-url>
//	reproforge replay <capsule.tar.zst>
//	reproforge diagnose <capsule.tar.zst>
//	reproforge flake <capsule.tar.zst> --runs 20
//	reproforge report <capsule.tar.zst> --format markdown
//	reproforge patch <capsule.tar.zst> --ai claude --verify
package main

import (
	"fmt"
	"os"

	"github.com/reproforge/reproforge/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
