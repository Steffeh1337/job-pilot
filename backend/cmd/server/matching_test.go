package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/scheduler"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestBuildMatcherRefusesARemoteOllama(t *testing.T) {
	cfg := config.Default()
	cfg.Ollama.URL = "http://ollama.example.com:11434"
	if _, err := buildMatcher(context.Background(), cfg, testStore(t), quiet()); err == nil || !strings.Contains(err.Error(), "not on this machine") {
		t.Fatalf("err = %v; the server must refuse to start rather than send a CV elsewhere", err)
	}
	cfg.Ollama.AllowRemote = true
	if _, err := buildMatcher(context.Background(), cfg, testStore(t), quiet()); err != nil {
		t.Errorf("allow_remote should permit it: %v", err)
	}
}

func TestBuildMatcherWithOllamaOff(t *testing.T) {
	cfg := config.Default()
	cfg.Ollama.Enabled = false
	m, err := buildMatcher(context.Background(), cfg, testStore(t), quiet())
	if err != nil {
		t.Fatal(err)
	}
	if m.LLM != nil {
		t.Error("with ollama disabled there must be no LLM (a typed nil would look enabled)")
	}
	if st := m.Status(context.Background()); st.Ollama.Enabled {
		t.Errorf("status = %+v", st.Ollama)
	}
}

func TestBuildMatcherCopiesTheSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Ollama.EmbedModel = "bge-m3"
	cfg.Ollama.ChatModel = "qwen2.5:7b"
	cfg.Fit.SkillWeight, cfg.Fit.SemanticWeight, cfg.Fit.SemanticLow, cfg.Fit.SemanticHigh = 2, 1, 0.3, 0.9
	m, err := buildMatcher(context.Background(), cfg, testStore(t), quiet())
	if err != nil {
		t.Fatal(err)
	}
	c := m.Cfg
	if c.EmbedModel != "bge-m3" || c.ChatModel != "qwen2.5:7b" || c.EmbedPrefix != "" {
		t.Errorf("cfg = %+v (bge-m3 takes no prefix)", c)
	}
	if c.Fit.SkillWeight != 2 || c.Fit.SemanticWeight != 1 || c.Fit.SemanticLow != 0.3 || c.Fit.SemanticHigh != 0.9 {
		t.Errorf("fit = %+v", c.Fit)
	}
	cfg.Ollama.EmbedModel = "nomic-embed-text"
	if m, _ := buildMatcher(context.Background(), cfg, testStore(t), quiet()); m.Cfg.EmbedPrefix != "clustering: " {
		t.Errorf("nomic-embed-text should get its prefix, got %q", m.Cfg.EmbedPrefix)
	}
}

func TestIndexNewJobsOnlyWhenThereAreNewJobs(t *testing.T) {
	cfg := config.Default()
	cfg.Ollama.Enabled = false
	st := testStore(t)
	m, _ := buildMatcher(context.Background(), cfg, st, quiet())
	hook := indexNewJobs(m)

	hook([]scheduler.PollResult{{Source: "a", Fetched: 10}, {Source: "b", Skipped: true}})
	if m.Status(context.Background()).Index.Syncing {
		t.Error("a round that found nothing new must not start indexing")
	}
	hook(nil) // must not panic
}
