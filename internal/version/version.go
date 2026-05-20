// Package version exposes build metadata for the reproforge CLI.
package version

// Version is overridden at build time via -ldflags="-X .../version.Version=..."
var (
	// Version is the semantic version of the binary.
	Version = "0.1.0"
	// Commit is the git commit SHA the binary was built from.
	Commit = "dev"
	// Date is the RFC3339 build date.
	Date = "unknown"
	// CapsuleSchema is the supported capsule schema version.
	CapsuleSchema = "reproforge.capsule/v1"
)
