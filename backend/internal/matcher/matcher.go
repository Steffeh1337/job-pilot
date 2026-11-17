// Package matcher ties CV matching together: it reads a CV into a profile,
// indexes jobs (skills, plus embeddings when a local model is available), and
// scores jobs against the profile. Nothing here talks to anything but the
// local database and, optionally, a local Ollama.
package matcher

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/ollama"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// LLM is the part of Ollama the matcher uses. *ollama.Client implements it.
type LLM interface {
	Version(ctx context.Context) (string, error)
	Models(ctx context.Context) ([]ollama.Model, error)
	Embed(ctx context.Context, model string, inputs []string) ([][]float32, error)
	ChatJSON(ctx context.Context, model, system, user string, schema any, out any) error
	Chat(ctx context.Context, model, system, user string) (string, error)
}

// Config holds the matcher's settings.
type Config struct {
	EmbedModel string
	// ChatModel reads the CV. Empty means: use an installed one.
	ChatModel string
	// EmbedPrefix is put in front of every text before embedding; some models
	// (nomic-embed-text) expect one.
	EmbedPrefix string
	// MinStage is the earliest funnel stage whose jobs are indexed and scored.
	MinStage domain.Stage
	Fit      fit.Config
	// BatchSize is how many jobs are embedded per request.
	BatchSize int
}

// DefaultConfig suits nomic-embed-text.
func DefaultConfig() Config {
	return Config{
		EmbedModel:  "nomic-embed-text",
		EmbedPrefix: "clustering: ",
		MinStage:    domain.StageRegion,
		Fit:         fit.DefaultConfig(),
		BatchSize:   16,
	}
}

type Matcher struct {
	Store *store.Store
	// LLM is nil when no local model is configured; matching then uses
	// keywords only.
	LLM   LLM
	Vocab *skills.Vocabulary
	Cfg   Config
	Log   *slog.Logger
	// Ctx bounds background indexing. Nil means context.Background().
	Ctx context.Context
	Now func() time.Time

	// disableAutoSync stops uploads and edits from starting a background
	// indexing pass. Tests use it to control when indexing happens.
	disableAutoSync bool

	syncMu sync.Mutex // one indexing pass at a time

	mu       sync.Mutex // guards the fields below
	gen      int        // bumped whenever the profile changes
	cache    *fitProfile
	running  bool
	pending  bool
	lastSync time.Time
	lastErr  string
	lastRun  SyncReport
}

func (m *Matcher) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Matcher) ctx() context.Context {
	if m.Ctx != nil {
		return m.Ctx
	}
	return context.Background()
}

// autoSync starts a background pass after the profile changed.
func (m *Matcher) autoSync() {
	if !m.disableAutoSync {
		m.RequestSync()
	}
}

func (m *Matcher) invalidate() {
	m.mu.Lock()
	m.cache = nil
	m.gen++
	m.mu.Unlock()
}

// ErrNoChatModel means Ollama has no model that can read a CV.
var ErrNoChatModel = errors.New("no chat model is installed. Run `ollama pull qwen2.5:7b` (or set ollama.chat_model)")

// preferredChat lists models known to do structured extraction well, best first.
var preferredChat = []string{"qwen2.5:7b", "qwen3:8b", "llama3.1:8b", "qwen2.5:14b", "gemma3:12b", "llama3.2:3b", "qwen2.5:3b", "mistral:7b"}

// chatModel picks the model that reads the CV.
func (m *Matcher) chatModel(ctx context.Context) (string, error) {
	models, err := m.LLM.Models(ctx)
	if err != nil {
		return "", err
	}
	return chooseChatModel(m.Cfg.ChatModel, models)
}

func chooseChatModel(want string, models []ollama.Model) (string, error) {
	if want != "" {
		if hasModel(models, want) {
			return want, nil
		}
		return "", &ollama.ModelMissingError{Model: want}
	}
	for _, p := range preferredChat {
		if hasModel(models, p) {
			return p, nil
		}
	}
	for _, mod := range models {
		if !isEmbeddingModel(mod.Name) {
			return mod.Name, nil
		}
	}
	return "", ErrNoChatModel
}

// hasModel treats "nomic-embed-text" as installed when "nomic-embed-text:latest" is.
func hasModel(models []ollama.Model, want string) bool {
	for _, mod := range models {
		if mod.Name == want || (!strings.Contains(want, ":") && mod.Name == want+":latest") {
			return true
		}
	}
	return false
}

func isEmbeddingModel(name string) bool {
	n := strings.ToLower(name)
	for _, k := range []string{"embed", "bge-", "minilm", "e5-", "gte-", "mxbai"} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

// OllamaStatus describes the local model server.
type OllamaStatus struct {
	Enabled        bool     `json:"enabled"`
	Reachable      bool     `json:"reachable"`
	Version        string   `json:"version,omitempty"`
	Error          string   `json:"error,omitempty"`
	EmbedModel     string   `json:"embed_model"`
	EmbedInstalled bool     `json:"embed_installed"`
	ChatModel      string   `json:"chat_model,omitempty"`
	ChatInstalled  bool     `json:"chat_installed"`
	Installed      []string `json:"installed"`
}

// IndexStatus says how much of the job list is ready for matching.
type IndexStatus struct {
	store.FeatureCounts
	Syncing   bool       `json:"syncing"`
	LastSync  *time.Time `json:"last_sync"`
	LastError string     `json:"last_error,omitempty"`
}

// Status is what the dashboard shows about matching.
type Status struct {
	Ollama     OllamaStatus `json:"ollama"`
	HasProfile bool         `json:"has_profile"`
	Index      IndexStatus  `json:"index"`
}

// Status checks Ollama and the index. It never fails: problems are reported in
// the result.
func (m *Matcher) Status(ctx context.Context) Status {
	st := Status{Ollama: OllamaStatus{Enabled: m.LLM != nil, EmbedModel: m.Cfg.EmbedModel, Installed: []string{}}}

	if m.LLM != nil {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if v, err := m.LLM.Version(cctx); err != nil {
			st.Ollama.Error = err.Error()
		} else {
			st.Ollama.Reachable, st.Ollama.Version = true, v
			if models, err := m.LLM.Models(cctx); err != nil {
				st.Ollama.Error = err.Error()
			} else {
				for _, mod := range models {
					st.Ollama.Installed = append(st.Ollama.Installed, mod.Name)
				}
				st.Ollama.EmbedInstalled = hasModel(models, m.Cfg.EmbedModel)
				if name, err := chooseChatModel(m.Cfg.ChatModel, models); err == nil {
					st.Ollama.ChatModel, st.Ollama.ChatInstalled = name, true
				} else {
					st.Ollama.ChatModel = m.Cfg.ChatModel
				}
			}
		}
	}

	if row, err := m.Store.LoadProfile(ctx); err == nil && row != nil {
		st.HasProfile = true
	}
	counts, _ := m.Store.FeatureCounts(ctx, m.Cfg.MinStage, m.featureVersion(), m.Cfg.EmbedModel)
	m.mu.Lock()
	st.Index = IndexStatus{FeatureCounts: counts, Syncing: m.running, LastError: m.lastErr}
	if !m.lastSync.IsZero() {
		t := m.lastSync
		st.Index.LastSync = &t
	}
	m.mu.Unlock()
	return st
}
