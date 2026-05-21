// Package server implements FR-031: an optional, self-hosted HTTP service
// that lets a team upload, query and download capsules. The server is
// deliberately small and stateless apart from a content-addressed object
// store on disk and a SQLite index. It is OPT-IN — `reproforge serve`
// must be invoked explicitly. The local-only mode (no server) keeps
// working unchanged.
//
// Endpoints (all JSON unless noted):
//
//   POST  /api/v1/capsules           upload a tar.zst capsule (body: raw bytes)
//   GET   /api/v1/capsules/:fp       download the capsule by fingerprint
//   GET   /api/v1/capsules           list capsules (optional ?repo=)
//   GET   /api/v1/diagnoses/:fp      latest diagnosis for a fingerprint
//   GET   /healthz                   200 OK
//
// Auth: bearer token via REPROFORGE_SERVER_TOKEN. If empty, the server is
// read-only and refuses POSTs. Never accepts a capsule larger than 50MB.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/reproforge/reproforge/internal/logx"
)

// Config controls the server.
type Config struct {
	Addr      string // ":8080"
	Storage   string // "./reproforge-server"
	Token     string // bearer token; empty → read-only
	MaxBody   int64  // bytes; 0 → 50MiB default
	Logger    *logx.Logger
}

// Server is the HTTP entry point.
type Server struct {
	cfg     Config
	logger  *logx.Logger
	mu      sync.RWMutex
	indexed map[string]capsuleIndex
}

type capsuleIndex struct {
	Fingerprint string    `json:"fingerprint"`
	Repo        string    `json:"repo"`
	Job         string    `json:"job"`
	Workflow    string    `json:"workflow"`
	Category    string    `json:"category,omitempty"`
	Size        int64     `json:"size"`
	UploadedAt  time.Time `json:"uploadedAt"`
	Path        string    `json:"-"`
}

// New returns a configured Server.
func New(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.Storage == "" {
		cfg.Storage = "./reproforge-server"
	}
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = 50 * 1024 * 1024
	}
	if cfg.Logger == nil {
		cfg.Logger = logx.Default()
	}
	if err := os.MkdirAll(filepath.Join(cfg.Storage, "capsules"), 0o755); err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, logger: cfg.Logger, indexed: map[string]capsuleIndex{}}
	if err := s.indexExisting(); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns an http.Handler ready to be served.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/api/v1/capsules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleList(w, r)
		case http.MethodPost:
			s.handleUpload(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/capsules/", s.handleByFingerprint)
	mux.HandleFunc("/api/v1/diagnoses/", s.handleDiagnosis)
	return mux
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	s.logger.Info("server: listening", "addr", s.cfg.Addr, "storage", s.cfg.Storage, "auth", s.cfg.Token != "")
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// indexExisting reads existing capsule files from disk and rebuilds the index.
func (s *Server) indexExisting() error {
	dir := filepath.Join(s.cfg.Storage, "capsules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tar.zst") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		fp := strings.TrimSuffix(e.Name(), ".tar.zst")
		idx := capsuleIndex{
			Fingerprint: "sha256:" + fp, Size: info.Size(), Path: full, UploadedAt: info.ModTime().UTC(),
		}
		// best-effort: open the capsule to extract repo/job/category
		if c, err := capsule.UnpackFile(full, filepath.Join(s.cfg.Storage, "tmp-"+fp)); err == nil {
			idx.Repo = c.Repo
			idx.Job = c.Job
			idx.Workflow = c.Workflow
			if c.Diagnosis != nil {
				idx.Category = c.Diagnosis.Category
			}
			_ = os.RemoveAll(filepath.Join(s.cfg.Storage, "tmp-"+fp))
		}
		s.indexed[idx.Fingerprint] = idx
	}
	return nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]capsuleIndex, 0, len(s.indexed))
	for _, v := range s.indexed {
		if repo != "" && v.Repo != repo {
			continue
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkWriteAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body too large or read failed: "+err.Error(), http.StatusRequestEntityTooLarge)
		return
	}

	// Verify the capsule before persisting.
	tmp, err := os.MkdirTemp(s.cfg.Storage, "verify-*")
	if err != nil {
		http.Error(w, "tmp dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmp)
	tmpFile := filepath.Join(tmp, "in.tar.zst")
	if err := os.WriteFile(tmpFile, body, 0o600); err != nil {
		http.Error(w, "write tmp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	c, err := capsule.UnpackFile(tmpFile, filepath.Join(tmp, "ext"))
	if err != nil {
		http.Error(w, "invalid capsule: "+err.Error(), http.StatusBadRequest)
		return
	}
	fpHex := strings.TrimPrefix(c.Failure.Fingerprint, "sha256:")
	if fpHex == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}
	dest := filepath.Join(s.cfg.Storage, "capsules", fpHex+".tar.zst")
	// content-addressed: if a different body lands on the same fp, reject.
	if existing, err := os.ReadFile(dest); err == nil {
		if !bytesEqual(existing, body) {
			http.Error(w, "fingerprint collision with different body", http.StatusConflict)
			return
		}
	}
	if err := os.WriteFile(dest, body, 0o600); err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	idx := capsuleIndex{
		Fingerprint: c.Failure.Fingerprint, Repo: c.Repo, Job: c.Job,
		Workflow: c.Workflow, Path: dest, Size: int64(len(body)), UploadedAt: time.Now().UTC(),
	}
	if c.Diagnosis != nil {
		idx.Category = c.Diagnosis.Category
	}
	s.mu.Lock()
	s.indexed[idx.Fingerprint] = idx
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, idx)
}

func (s *Server) handleByFingerprint(w http.ResponseWriter, r *http.Request) {
	fp := strings.TrimPrefix(r.URL.Path, "/api/v1/capsules/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(fp, "sha256:") {
		fp = "sha256:" + fp
	}
	s.mu.RLock()
	idx, ok := s.indexed[fp]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("X-ReproForge-Repo", idx.Repo)
	w.Header().Set("X-ReproForge-Fingerprint", idx.Fingerprint)
	body, err := os.ReadFile(idx.Path)
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func (s *Server) handleDiagnosis(w http.ResponseWriter, r *http.Request) {
	fp := strings.TrimPrefix(r.URL.Path, "/api/v1/diagnoses/")
	if fp == "" {
		http.Error(w, "missing fingerprint", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(fp, "sha256:") {
		fp = "sha256:" + fp
	}
	s.mu.RLock()
	idx, ok := s.indexed[fp]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tmp, err := os.MkdirTemp(s.cfg.Storage, "dx-*")
	if err != nil {
		http.Error(w, "tmp: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmp)
	c, err := capsule.UnpackFile(idx.Path, tmp)
	if err != nil {
		http.Error(w, "extract: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if c.Diagnosis == nil {
		writeJSON(w, http.StatusOK, map[string]any{"category": "unknown", "confidence": 0})
		return
	}
	writeJSON(w, http.StatusOK, c.Diagnosis)
}

func (s *Server) checkWriteAuth(r *http.Request) bool {
	if s.cfg.Token == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	want := "Bearer " + s.cfg.Token
	return auth == want
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	hashA := sha256.Sum256(a)
	hashB := sha256.Sum256(b)
	return hex.EncodeToString(hashA[:]) == hex.EncodeToString(hashB[:])
}

// Errors surfaced to callers.
var (
	ErrTooLarge = errors.New("server: capsule exceeds max body size")
	_           = fmt.Sprintf
)
