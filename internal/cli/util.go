package cli

import (
	"os"
	"path/filepath"
)

// writeWithMode is a small helper used by writers to ensure the parent dir
// exists before writing.
func writeWithMode(p string, body []byte, mode uint32) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, os.FileMode(mode))
}
