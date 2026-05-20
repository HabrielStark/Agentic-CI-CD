package capsule

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CapsuleFileName is the canonical filename for the manifest inside the bundle.
const CapsuleFileName = "capsule.json"

// ChecksumsFileName is the canonical filename for the checksums manifest.
const ChecksumsFileName = "checksums.txt"

// PackOptions configures Pack.
type PackOptions struct {
	// SourceDir is a directory whose contents become the capsule body.
	// If empty, only Manifest and any AddFiles entries are written.
	SourceDir string
	// Manifest is the resulting capsule manifest. Required.
	Manifest *Capsule
	// Output is the destination tar.zst writer.
	Output io.Writer
	// AddFiles is an optional set of additional in-memory files keyed by relative path.
	AddFiles map[string][]byte
	// CompressionLevel is the zstd level (1..22). Defaults to zstd default if zero.
	CompressionLevel int
}

// Pack writes a tar.zst capsule to opts.Output. The manifest's Logs/Artifacts
// SHA256 fields are populated when a SourceDir is provided. checksums.txt is
// always emitted with sha256 entries for every payload file.
func Pack(opts PackOptions) error {
	if opts.Output == nil {
		return errors.New("capsule.Pack: Output is nil")
	}
	if opts.Manifest == nil {
		return errors.New("capsule.Pack: Manifest is nil")
	}

	// Collect files first to compute sha256 for the manifest.
	type packed struct {
		path string // path inside tar
		data []byte
	}
	var files []packed

	if opts.SourceDir != "" {
		err := filepath.WalkDir(opts.SourceDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(opts.SourceDir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			// Always exclude the manifest from sources to avoid duplication.
			if rel == CapsuleFileName {
				return nil
			}
			files = append(files, packed{path: rel, data: b})
			return nil
		})
		if err != nil {
			return err
		}
	}
	// Add explicit files (these win over source files at the same path).
	for k, v := range opts.AddFiles {
		k = filepath.ToSlash(k)
		// drop pre-existing entry with same path
		newFiles := files[:0]
		for _, f := range files {
			if f.path != k {
				newFiles = append(newFiles, f)
			}
		}
		files = append(newFiles, packed{path: k, data: append([]byte(nil), v...)})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	// Update manifest content hashes.
	pathHash := map[string]string{}
	pathSize := map[string]int{}
	for _, f := range files {
		s := sha256.Sum256(f.data)
		pathHash[f.path] = hex.EncodeToString(s[:])
		pathSize[f.path] = len(f.data)
	}
	for i := range opts.Manifest.Logs {
		if h, ok := pathHash[opts.Manifest.Logs[i].Path]; ok {
			opts.Manifest.Logs[i].SHA256 = h
			opts.Manifest.Logs[i].Size = int64(pathSize[opts.Manifest.Logs[i].Path])
		}
	}
	for i := range opts.Manifest.Artifacts {
		if h, ok := pathHash[opts.Manifest.Artifacts[i].Path]; ok {
			opts.Manifest.Artifacts[i].SHA256 = h
			opts.Manifest.Artifacts[i].Size = int64(pathSize[opts.Manifest.Artifacts[i].Path])
		}
	}

	if err := opts.Manifest.Validate(); err != nil {
		return fmt.Errorf("manifest invalid: %w", err)
	}

	// Build checksums.txt
	var cb strings.Builder
	for _, f := range files {
		fmt.Fprintf(&cb, "%s  %s\n", pathHash[f.path], f.path)
	}

	manifestBytes, err := json.MarshalIndent(opts.Manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')

	// Compose final list: capsule.json, files..., checksums.txt
	final := []packed{{path: CapsuleFileName, data: manifestBytes}}
	final = append(final, files...)
	final = append(final, packed{path: ChecksumsFileName, data: []byte(cb.String())})

	// zstd level
	var encoderOpts []zstd.EOption
	if opts.CompressionLevel > 0 {
		encoderOpts = append(encoderOpts, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(opts.CompressionLevel)))
	}

	enc, err := zstd.NewWriter(opts.Output, encoderOpts...)
	if err != nil {
		return err
	}
	defer enc.Close()

	tw := tar.NewWriter(enc)
	for _, f := range final {
		if err := tw.WriteHeader(&tar.Header{
			Name:     f.path,
			Mode:     0o644,
			Size:     int64(len(f.data)),
			Typeflag: tar.TypeReg,
			ModTime:  opts.Manifest.CreatedAt,
		}); err != nil {
			return err
		}
		if _, err := tw.Write(f.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return nil
}

// PackFile is a convenience wrapper that writes the capsule to dst on disk.
func PackFile(dst string, opts PackOptions) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	opts.Output = bw
	if err := Pack(opts); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := bw.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Unpack extracts a tar.zst capsule into destDir and returns the parsed
// manifest. It validates checksums and the manifest, returning an error if any
// integrity check fails.
func Unpack(src io.Reader, destDir string) (*Capsule, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(src)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	tr := tar.NewReader(dec)
	checksums := map[string]string{}
	hashes := map[string]string{}
	var manifestBytes []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		clean := path.Clean(hdr.Name)
		if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return nil, fmt.Errorf("refusing unsafe path: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeReg:
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			s := sha256.Sum256(data)
			hashes[clean] = hex.EncodeToString(s[:])
			if clean == CapsuleFileName {
				manifestBytes = data
			} else if clean == ChecksumsFileName {
				for _, line := range strings.Split(string(data), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.SplitN(line, "  ", 2)
					if len(parts) != 2 {
						continue
					}
					checksums[parts[1]] = parts[0]
				}
			}
			out := filepath.Join(destDir, clean)
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return nil, err
			}
		case tar.TypeDir:
			out := filepath.Join(destDir, clean)
			if err := os.MkdirAll(out, 0o755); err != nil {
				return nil, err
			}
		default:
			// skip symlinks/devices
		}
	}

	if manifestBytes == nil {
		return nil, errors.New("capsule.json missing")
	}
	c, err := Decode(manifestBytes)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("manifest validation: %w", err)
	}
	if len(checksums) == 0 {
		return c, nil
	}
	for p, expect := range checksums {
		got, ok := hashes[p]
		if !ok {
			return c, fmt.Errorf("file declared in checksums.txt missing: %s", p)
		}
		if got != expect {
			return c, fmt.Errorf("checksum mismatch for %s: want %s got %s", p, expect, got)
		}
	}
	// ensure manifest log/artifact hashes match
	for _, l := range c.Logs {
		got, ok := hashes[l.Path]
		if !ok {
			return c, fmt.Errorf("log %s missing in capsule", l.Path)
		}
		if l.SHA256 != "" && got != l.SHA256 {
			return c, fmt.Errorf("manifest log hash mismatch for %s", l.Path)
		}
	}
	for _, a := range c.Artifacts {
		got, ok := hashes[a.Path]
		if !ok {
			return c, fmt.Errorf("artifact %s missing in capsule", a.Path)
		}
		if a.SHA256 != "" && got != a.SHA256 {
			return c, fmt.Errorf("manifest artifact hash mismatch for %s", a.Path)
		}
	}
	return c, nil
}

// UnpackFile reads src on disk and unpacks into destDir.
func UnpackFile(src, destDir string) (*Capsule, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Unpack(bufio.NewReader(f), destDir)
}
