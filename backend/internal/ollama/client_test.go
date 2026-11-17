package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Options{URL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewOnlyAllowsThisMachine(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:11434", "http://localhost:11434", "http://[::1]:11434", "http://127.0.0.5:9/", "https://localhost"} {
		if _, err := New(Options{URL: u}); err != nil {
			t.Errorf("%s should be allowed: %v", u, err)
		}
	}
	for _, u := range []string{"http://example.com:11434", "http://10.0.0.5:11434", "https://192.168.1.20", "http://ollama.internal", "http://0.0.0.0:11434"} {
		if _, err := New(Options{URL: u}); err == nil || !strings.Contains(err.Error(), "not on this machine") {
			t.Errorf("%s should be refused, got %v", u, err)
		}
	}
	if _, err := New(Options{URL: "http://example.com:11434", AllowRemote: true}); err != nil {
		t.Errorf("allow_remote should permit it: %v", err)
	}
	for _, u := range []string{"", "not a url", "ftp://127.0.0.1", "127.0.0.1:11434"} {
		if _, err := New(Options{URL: u}); err == nil {
			t.Errorf("%q should be rejected as invalid", u)
		}
	}
}

func TestVersionAndModels(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.9.1"}`))
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"nomic-embed-text:latest","size":274000000},{"name":"qwen2.5:7b","size":4700000000}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	if v, err := c.Version(context.Background()); err != nil || v != "0.9.1" {
		t.Errorf("Version = %q, %v", v, err)
	}
	ms, err := c.Models(context.Background())
	if err != nil || len(ms) != 2 || ms[1].Name != "qwen2.5:7b" || ms[0].Size != 274000000 {
		t.Errorf("Models = %+v, %v", ms, err)
	}
}

func TestEmbed(t *testing.T) {
	var got map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"embeddings":[[1,0,0],[0,1,0]]}`))
	})
	vecs, err := c.Embed(context.Background(), "nomic-embed-text", []string{"a", "b"})
	if err != nil || len(vecs) != 2 || vecs[1][1] != 1 {
		t.Fatalf("vecs = %v, err = %v", vecs, err)
	}
	if got["model"] != "nomic-embed-text" || len(got["input"].([]any)) != 2 {
		t.Errorf("request = %v", got)
	}
	if v, err := c.Embed(context.Background(), "m", nil); err != nil || v != nil {
		t.Errorf("empty input should be a no-op, got %v, %v", v, err)
	}
}

func TestEmbedValidatesTheReply(t *testing.T) {
	for name, body := range map[string]string{
		"wrong count":      `{"embeddings":[[1,2]]}`,
		"empty vector":     `{"embeddings":[[],[]]}`,
		"ragged vectors":   `{"embeddings":[[1,2],[1]]}`,
		"not json":         `<html>`,
		"missing embedder": `{}`,
	} {
		c := newTest(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
		if _, err := c.Embed(context.Background(), "m", []string{"a", "b"}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestModelMissing(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model \"nomic-embed-text\" not found, try pulling it first"}`))
	})
	_, err := c.Embed(context.Background(), "nomic-embed-text", []string{"a"})
	if !IsModelMissing(err) {
		t.Fatalf("err = %v, want ModelMissingError", err)
	}
	if !strings.Contains(err.Error(), "ollama pull nomic-embed-text") {
		t.Errorf("the message should say how to fix it: %v", err)
	}
}

func TestUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing is listening any more
	c, _ := New(Options{URL: url, Timeout: 2 * time.Second})
	_, err := c.Version(context.Background())
	if !IsUnavailable(err) {
		t.Fatalf("err = %v, want UnavailableError", err)
	}
	if IsModelMissing(err) {
		t.Error("an unreachable server is not a missing model")
	}
}

func TestOtherStatusErrors(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"out of memory"}`))
	})
	_, err := c.Version(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 500 || se.Message != "out of memory" {
		t.Errorf("err = %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the redirect target must never be contacted")
	}))
	defer elsewhere.Close()
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	})
	if _, err := c.Version(context.Background()); err == nil {
		t.Error("a redirect should be an error")
	}
}

func TestChatJSON(t *testing.T) {
	var req map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&req)
		w.Write([]byte(`{"message":{"role":"assistant","content":"` + "```json\\n{\\\"headline\\\":\\\"Backend engineer\\\",\\\"years\\\":8}\\n```" + `"}}`))
	})
	var out struct {
		Headline string `json:"headline"`
		Years    int    `json:"years"`
	}
	err := c.ChatJSON(context.Background(), "qwen2.5:7b", "system prompt", "the cv", map[string]any{"type": "object"}, &out)
	if err != nil || out.Headline != "Backend engineer" || out.Years != 8 {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if req["stream"] != false || req["model"] != "qwen2.5:7b" {
		t.Errorf("request = %v", req)
	}
	if opts := req["options"].(map[string]any); opts["temperature"] != float64(0) {
		t.Errorf("temperature should be 0, options = %v", opts)
	}
	if msgs := req["messages"].([]any); len(msgs) != 2 || msgs[1].(map[string]any)["content"] != "the cv" {
		t.Errorf("messages = %v", msgs)
	}
	if req["format"] == nil {
		t.Error("the schema was not sent")
	}
}

func TestChatReturnsPlainText(t *testing.T) {
	var req map[string]any
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&req)
		w.Write([]byte(`{"message":{"role":"assistant","content":"  Hello there.\n"}}`))
	})
	got, err := c.Chat(context.Background(), "qwen2.5:7b", "be brief", "hi")
	if err != nil || got != "Hello there." {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, set := req["format"]; set {
		t.Error("a plain answer must not be constrained to a JSON schema")
	}
	if opts := req["options"].(map[string]any); opts["temperature"] == float64(0) {
		t.Errorf("a plain answer should not use temperature 0, options = %v", opts)
	}
}

func TestChatJSONRejectsNonJSON(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"Sure! Here is the profile."}}`))
	})
	var out map[string]any
	if err := c.ChatJSON(context.Background(), "m", "s", "u", map[string]any{}, &out); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestChatJSONRetriesWithoutThink(t *testing.T) {
	calls := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		calls++
		if _, has := req["think"]; has {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"\"llama3.2\" does not support thinking"}`))
			return
		}
		w.Write([]byte(`{"message":{"content":"{\"ok\":true}"}}`))
	})
	var out struct{ OK bool }
	if err := c.ChatJSON(context.Background(), "llama3.2", "s", "u", map[string]any{}, &out); err != nil || !out.OK {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestContextCancellationIsNotUnavailable(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Version(ctx)
	if err == nil || IsUnavailable(err) {
		t.Errorf("err = %v; a timeout is not 'Ollama is down'", err)
	}
}
