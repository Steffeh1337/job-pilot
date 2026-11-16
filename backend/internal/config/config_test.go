package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestOllamaDefaultsStayOnThisMachine(t *testing.T) {
	c := Default()
	if !c.Ollama.Enabled || c.Ollama.URL != "http://127.0.0.1:11434" || c.Ollama.AllowRemote {
		t.Errorf("ollama defaults = %+v", c.Ollama)
	}
	if c.Ollama.EmbedModel != "nomic-embed-text" || c.Ollama.ChatModel != "" || c.Ollama.Timeout <= 0 {
		t.Errorf("ollama defaults = %+v", c.Ollama)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("the defaults must be valid: %v", err)
	}
}

func TestOllamaAndFitOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	yml := `
ollama:
  enabled: true
  url: http://localhost:11434
  embed_model: mxbai-embed-large
  chat_model: qwen2.5:7b
  embed_prefix: ""
  timeout: 90s
fit:
  skill_weight: 1
  semantic_weight: 0
  semantic_low: 0.3
  semantic_high: 0.9
`
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ollama.EmbedModel != "mxbai-embed-large" || c.Ollama.ChatModel != "qwen2.5:7b" || c.Ollama.Timeout != 90*time.Second {
		t.Errorf("ollama = %+v", c.Ollama)
	}
	if c.Fit.SkillWeight != 1 || c.Fit.SemanticWeight != 0 || c.Fit.SemanticLow != 0.3 {
		t.Errorf("fit = %+v", c.Fit)
	}
	if c.Ollama.EmbedPrefix == nil || c.Ollama.EmbedPrefixOrDefault() != "" {
		t.Error(`an explicit empty prefix must mean "no prefix", not "use the default"`)
	}
}

func TestEmbedPrefixDefaults(t *testing.T) {
	for model, want := range map[string]string{
		"nomic-embed-text": "clustering: ", "nomic-embed-text:latest": "clustering: ", "bge-m3": "", "mxbai-embed-large": "",
	} {
		if got := (Ollama{EmbedModel: model}).EmbedPrefixOrDefault(); got != want {
			t.Errorf("prefix for %s = %q, want %q", model, got, want)
		}
	}
	custom := "query: "
	if got := (Ollama{EmbedModel: "nomic-embed-text", EmbedPrefix: &custom}).EmbedPrefixOrDefault(); got != "query: " {
		t.Errorf("an explicit prefix must win, got %q", got)
	}
}

func TestValidateRejectsNonsense(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"negative weight":      func(c *Config) { c.Fit.SkillWeight = -1 },
		"no weights":           func(c *Config) { c.Fit.SkillWeight, c.Fit.SemanticWeight = 0, 0 },
		"inverted calibration": func(c *Config) { c.Fit.SemanticLow, c.Fit.SemanticHigh = 0.8, 0.4 },
		"zero timeout":         func(c *Config) { c.Ollama.Timeout = 0 },
		"no url":               func(c *Config) { c.Ollama.URL = " " },
		"no embed model":       func(c *Config) { c.Ollama.EmbedModel = "" },
	} {
		c := Default()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// A disabled Ollama doesn't need a URL or model.
	c := Default()
	c.Ollama = Ollama{Enabled: false, Timeout: time.Minute}
	if err := c.Validate(); err != nil {
		t.Errorf("disabled ollama should validate: %v", err)
	}
}

func TestLoadReportsInvalidFit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(path, []byte("fit:\n  semantic_low: 0.9\n  semantic_high: 0.5\n"), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "semantic_high") {
		t.Errorf("err = %v", err)
	}
}

// The example config is documentation people copy, so it must always load.
func TestExampleConfigLoadsAndMatchesTheDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
	def := Default()
	if cfg.Ollama.URL != def.Ollama.URL || cfg.Ollama.EmbedModel != def.Ollama.EmbedModel || cfg.Ollama.Timeout != def.Ollama.Timeout {
		t.Errorf("ollama in the example = %+v, defaults = %+v", cfg.Ollama, def.Ollama)
	}
	if cfg.Ollama.AllowRemote {
		t.Error("the example must never turn allow_remote on")
	}
	if cfg.Fit != def.Fit {
		t.Errorf("fit in the example = %+v, defaults = %+v", cfg.Fit, def.Fit)
	}
	if len(cfg.Boards.Greenhouse) == 0 {
		t.Error("the example should list some company boards")
	}
}
