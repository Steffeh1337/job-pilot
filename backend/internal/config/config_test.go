package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tick != 15*time.Minute || !cfg.Sources["remoteok"].Enabled {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadOverridesOnlyWhatIsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	yml := `
tick: 5m
sources:
  jobicy: {enabled: false}
  remotive: {enabled: true, interval: 12h}
boards:
  greenhouse: [gitlab]
filter:
  stack: '(?i)\brust\b'
`
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tick != 5*time.Minute || cfg.Sources["jobicy"].Enabled || cfg.Sources["remotive"].Interval != 12*time.Hour {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if !cfg.Sources["remoteok"].Enabled || cfg.DBPath == "" || cfg.Filter.TitleInclude == "" {
		t.Errorf("unset values lost their defaults: %+v", cfg)
	}
	if cfg.Filter.Stack != `(?i)\brust\b` || len(cfg.Boards.Greenhouse) != 1 {
		t.Errorf("filter/boards not loaded: %+v", cfg)
	}
}

func TestLoadRejectsBadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("tick: [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected a parse error")
	}
}
