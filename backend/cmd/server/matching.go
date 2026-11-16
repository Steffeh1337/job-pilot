package main

import (
	"context"
	"log/slog"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/matcher"
	"github.com/Steffeh1337/job-pilot/backend/internal/ollama"
	"github.com/Steffeh1337/job-pilot/backend/internal/scheduler"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// buildMatcher sets up CV matching. An Ollama address that is not on this
// machine is refused here, so the server won't start rather than quietly send
// a CV somewhere else.
func buildMatcher(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) (*matcher.Matcher, error) {
	var llm matcher.LLM // stays nil when Ollama is off: matching then uses keywords only
	if cfg.Ollama.Enabled {
		client, err := ollama.New(ollama.Options{
			URL: cfg.Ollama.URL, AllowRemote: cfg.Ollama.AllowRemote, Timeout: cfg.Ollama.Timeout,
		})
		if err != nil {
			return nil, err
		}
		llm = client
		if cfg.Ollama.AllowRemote {
			log.Warn("ollama.allow_remote is on: your CV text is sent to " + cfg.Ollama.URL)
		}
	}

	mc := matcher.DefaultConfig()
	mc.EmbedModel = cfg.Ollama.EmbedModel
	mc.ChatModel = cfg.Ollama.ChatModel
	mc.EmbedPrefix = cfg.Ollama.EmbedPrefixOrDefault()
	mc.Fit = fit.Config{
		SkillWeight: cfg.Fit.SkillWeight, SemanticWeight: cfg.Fit.SemanticWeight,
		SemanticLow: cfg.Fit.SemanticLow, SemanticHigh: cfg.Fit.SemanticHigh,
	}
	return &matcher.Matcher{Store: st, LLM: llm, Vocab: skills.Default(), Cfg: mc, Log: log, Ctx: ctx}, nil
}

// indexNewJobs makes the scheduler index jobs as soon as a round finds new ones.
func indexNewJobs(m *matcher.Matcher) func([]scheduler.PollResult) {
	return func(results []scheduler.PollResult) {
		for _, r := range results {
			if r.NewJobs > 0 {
				m.RequestSync()
				return
			}
		}
	}
}
