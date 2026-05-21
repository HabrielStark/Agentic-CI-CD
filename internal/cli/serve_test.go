package cli

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/reproforge/reproforge/internal/server"
)

// TestServeCmd_HelpAndConfigOnly exercises the cobra wiring without
// actually opening a port (which would conflict with parallel tests).
// We verify --help works and that server.New is callable with the same
// flags via a direct invocation.
func TestServeCmd_HelpAndConfigOnly(t *testing.T) {
	out, _, err := runCLI("serve", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--addr", "--storage", "--token", "--max-mb"} {
		if !contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}

	// Construct a server with the same defaults; ensure it starts and
	// responds to /healthz, then shuts down.
	dir := t.TempDir()
	s, err := server.New(server.Config{Storage: filepath.Join(dir, "store")})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: s.Handler()}
	ln, err := newListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	r, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 200 {
		t.Fatalf("healthz: %d", r.StatusCode)
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
