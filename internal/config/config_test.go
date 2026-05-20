package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultWhenMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider == "" {
		t.Fatal("expected default provider")
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".reproforge", "config.yaml")
	cfg := Default()
	cfg.AI.Provider = "claude"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.AI.Provider != "claude" {
		t.Fatalf("not roundtripped: %+v", got.AI)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(p, []byte("not: [valid: yaml: at all"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected parse error")
	}
}
