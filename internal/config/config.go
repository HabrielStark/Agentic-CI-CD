// Package config loads the .reproforge/config.yaml file used by FR-001.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Default file name relative to the repository root.
const DefaultPath = ".reproforge/config.yaml"

// Config is the on-disk format.
type Config struct {
	Provider   string         `yaml:"provider" json:"provider"`
	Runtime    string         `yaml:"runtime" json:"runtime"`
	OutputDir  string         `yaml:"outputDir" json:"outputDir"`
	Redaction  RedactionCfg   `yaml:"redaction" json:"redaction"`
	Replay     ReplayCfg      `yaml:"replay" json:"replay"`
	Reporting  ReportingCfg   `yaml:"reporting" json:"reporting"`
	AI         AICfg          `yaml:"ai" json:"ai"`
	GitHub     GitHubCfg      `yaml:"github" json:"github"`
}

// RedactionCfg controls user-extensible redaction.
type RedactionCfg struct {
	Patterns []string `yaml:"patterns" json:"patterns"`
	Denylist []string `yaml:"denylist" json:"denylist"`
}

// ReplayCfg controls replay defaults.
type ReplayCfg struct {
	Image    string  `yaml:"image" json:"image"`
	Memory   string  `yaml:"memory" json:"memory"`
	CPUs     float64 `yaml:"cpus" json:"cpus"`
	Network  string  `yaml:"network" json:"network"`
	TimeoutSec int   `yaml:"timeoutSec" json:"timeoutSec"`
	EnvAllowlist []string `yaml:"envAllowlist" json:"envAllowlist"`
}

// ReportingCfg controls report output formats.
type ReportingCfg struct {
	Markdown bool `yaml:"markdown" json:"markdown"`
	JSON     bool `yaml:"json" json:"json"`
	SARIF    bool `yaml:"sarif" json:"sarif"`
}

// AICfg controls optional AI assistance.
type AICfg struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Verify   bool   `yaml:"verify" json:"verify"`
}

// GitHubCfg holds GitHub API hints.
type GitHubCfg struct {
	APIBase string `yaml:"apiBase" json:"apiBase"`
}

// Default returns sensible defaults.
func Default() Config {
	return Config{
		Provider:  "github_actions",
		Runtime:   "auto",
		OutputDir: "./reproforge-out",
		Redaction: RedactionCfg{},
		Replay: ReplayCfg{
			Image:   "",
			Memory:  "4g",
			CPUs:    2.0,
			Network: "configurable",
			TimeoutSec: 1800,
		},
		Reporting: ReportingCfg{Markdown: true, JSON: true, SARIF: false},
		AI:        AICfg{Provider: "none", Verify: true},
		GitHub:    GitHubCfg{APIBase: "https://api.github.com"},
	}
}

// Load reads a config from path. If path is empty, the default location is used.
func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultPath
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Provider == "" {
		cfg.Provider = "github_actions"
	}
	if cfg.Runtime == "" {
		cfg.Runtime = "auto"
	}
	return cfg, nil
}

// Save writes a config (creating parent dirs).
func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
