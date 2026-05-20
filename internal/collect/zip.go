package collect

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"path"
	"strings"
)

// openZip extracts a zip archive into a map of clean-path -> bytes.
func openZip(b []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(f.Name)
		if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		out[clean] = body
	}
	return out, nil
}

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
