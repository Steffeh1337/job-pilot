package matcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/ollama"
)

// fakeOllamaServer speaks the Ollama HTTP protocol, so the matcher is tested
// through the real client rather than a Go stub.
func fakeOllamaServer(t *testing.T) (*httptest.Server, *wireLog) {
	t.Helper()
	log := &wireLog{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.9.0"}`))
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"name":"qwen2.5:7b","size":4700000000},{"name":"nomic-embed-text:latest","size":274000000}]}`))
	})
	mux.HandleFunc("POST /api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		log.add("embed", req.Model, req.Input)
		if req.Model != "nomic-embed-text" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"model \"` + req.Model + `\" not found, try pulling it first"}`))
			return
		}
		vecs := make([][]float32, len(req.Input))
		for i, in := range req.Input {
			vecs[i] = kwVector(in)
		}
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": vecs})
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Format   any    `json:"format"`
			Messages []struct{ Role, Content string }
			Options  map[string]any `json:"options"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		user := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				user = m.Content
			}
		}
		log.add("chat", req.Model, []string{user})
		if req.Stream || req.Format == nil || len(req.Messages) != 2 || req.Options["temperature"] != float64(0) {
			t.Errorf("unexpected chat request: stream=%v format=%v messages=%d options=%v", req.Stream, req.Format != nil, len(req.Messages), req.Options)
		}
		content, _ := json.Marshal(llmProfile{
			Headline: "Senior Backend Engineer", Seniority: "senior", YearsExperience: 8,
			Skills: []string{"Temporal", "Haskell"}, Languages: []string{"English"}, Summary: "Builds distributed backend systems.",
		})
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "message": map[string]string{"role": "assistant", "content": string(content)}, "done": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, log
}

type wireLog struct {
	mu    sync.Mutex
	calls []wireCall
}
type wireCall struct {
	kind, model string
	inputs      []string
}

func (l *wireLog) add(kind, model string, inputs []string) {
	l.mu.Lock()
	l.calls = append(l.calls, wireCall{kind, model, inputs})
	l.mu.Unlock()
}

func (l *wireLog) of(kind string) []wireCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []wireCall
	for _, c := range l.calls {
		if c.kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func TestWorksThroughTheRealOllamaClient(t *testing.T) {
	srv, log := fakeOllamaServer(t)
	client, err := ollama.New(ollama.Options{URL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	m, st := newMatcher(t, client)
	ctx := context.Background()

	// Status through HTTP.
	s := m.Status(ctx)
	if !s.Ollama.Reachable || !s.Ollama.EmbedInstalled || s.Ollama.ChatModel != "qwen2.5:7b" || s.Ollama.Version != "0.9.0" {
		t.Fatalf("status = %+v", s.Ollama)
	}

	// Upload: the model reads the CV.
	view, warnings, err := m.Upload(ctx, "cv.txt", []byte(sampleCV))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err %v, warnings %v", err, warnings)
	}
	if view.ExtractedBy != "llm:qwen2.5:7b" || view.Headline != "Senior Backend Engineer" {
		t.Errorf("view = %+v", view)
	}
	if _, ok := hasSkill(view.Profile, "Temporal"); !ok {
		t.Error("grounded custom skill missing")
	}
	if _, ok := hasSkill(view.Profile, "Haskell"); ok {
		t.Error("an ungrounded skill got through")
	}
	chat := log.of("chat")
	if len(chat) != 1 || !strings.Contains(chat[0].inputs[0], "Led the migration to Go microservices") {
		t.Errorf("the CV text did not reach the model: %+v", chat)
	}

	// Sync: skills, then embeddings.
	a := addJob(t, st, "Backend engineer", "Backend services in Go and Kubernetes.", domain.StageSenior)
	rep, err := m.Sync(ctx)
	if err != nil || !rep.ProfileEmbedded || rep.JobsEmbedded != 1 || rep.EmbedError != "" {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
	fits, _ := m.Fits(ctx, []int64{a})
	if f, ok := fits[a]; !ok || f.Semantic == nil || f.SkillScore == nil {
		t.Errorf("fit = %+v", f)
	}

	// A model that isn't installed is reported with the command that fixes it.
	m.Cfg.EmbedModel = "mxbai-embed-large"
	m.invalidate()
	rep, err = m.Sync(ctx)
	if err != nil || !strings.Contains(rep.EmbedError, "ollama pull mxbai-embed-large") {
		t.Errorf("rep = %+v, err = %v", rep, err)
	}
}

func TestStatusWhenNothingListens(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	client, _ := ollama.New(ollama.Options{URL: url, Timeout: time.Second})
	m, _ := newMatcher(t, client)
	s := m.Status(context.Background())
	if s.Ollama.Reachable || !strings.Contains(s.Ollama.Error, "not reachable") {
		t.Errorf("status = %+v", s.Ollama)
	}
	if view, warnings, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV)); err != nil || view.ExtractedBy != "keywords" || len(warnings) != 1 {
		t.Errorf("upload with nothing listening: %+v, %v, %v", view, warnings, err)
	}
}
