// Package ollama is a small client for a local Ollama server. It exists to
// keep CV text on this machine, so it refuses any address that is not
// loopback unless remote use is explicitly allowed, never follows redirects,
// and never goes through a proxy.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options configure a Client.
type Options struct {
	// URL is the Ollama server, normally http://127.0.0.1:11434.
	URL string
	// AllowRemote permits a server that is not on this machine. Leave it off:
	// CV text is sent to this server.
	AllowRemote bool
	Timeout     time.Duration
}

type Client struct {
	base *url.URL
	http *http.Client
}

// New checks the address and builds a client.
func New(o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(o.URL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("ollama url %q is not a valid http(s) address", o.URL)
	}
	if !o.AllowRemote && !isLoopback(u.Hostname()) {
		return nil, fmt.Errorf("ollama url %q is not on this machine; your CV would leave it. Use 127.0.0.1 or set allow_remote: true if that is really what you want", o.URL)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{
		base: u,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{Proxy: nil}, // never via an environment proxy
			// Ollama does not redirect, so a redirect means something else is listening.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// UnavailableError means no Ollama server answered.
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string {
	return "Ollama is not reachable (is it running?): " + e.Err.Error()
}
func (e *UnavailableError) Unwrap() error { return e.Err }

// ModelMissingError means the model has not been pulled.
type ModelMissingError struct{ Model string }

func (e *ModelMissingError) Error() string {
	return fmt.Sprintf("model %q is not installed: run `ollama pull %s`", e.Model, e.Model)
}

// StatusError is any other error reply from the server.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return fmt.Sprintf("ollama: %d %s", e.Status, e.Message) }

func IsUnavailable(err error) bool  { var e *UnavailableError; return errors.As(err, &e) }
func IsModelMissing(err error) bool { var e *ModelMissingError; return errors.As(err, &e) }

// Model is an installed model.
type Model struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Version returns the server version.
func (c *Client) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/version", nil, "", &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// Models lists installed models.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Models []Model `json:"models"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/tags", nil, "", &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// Embed returns one vector per input.
func (c *Client) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	req := map[string]any{"model": model, "input": inputs, "truncate": true}
	if err := c.do(ctx, http.MethodPost, "/api/embed", req, model, &out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(out.Embeddings), len(inputs))
	}
	dim := len(out.Embeddings[0])
	for i, v := range out.Embeddings {
		if len(v) == 0 || len(v) != dim {
			return nil, fmt.Errorf("embedding %d has %d dimensions, want %d", i, len(v), dim)
		}
		for _, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return nil, fmt.Errorf("embedding %d contains an invalid number", i)
			}
		}
	}
	return out.Embeddings, nil
}

// ChatJSON asks the model to answer with JSON matching schema and decodes the
// answer into out. Temperature is 0 so the same CV gives the same profile.
func (c *Client) ChatJSON(ctx context.Context, model, system, user string, schema any, out any) error {
	req := map[string]any{
		"model":  model,
		"stream": false,
		"think":  false,
		"format": schema,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"options": map[string]any{"temperature": 0, "num_ctx": 8192},
	}
	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	err := c.do(ctx, http.MethodPost, "/api/chat", req, model, &resp)
	var se *StatusError
	if errors.As(err, &se) && se.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(se.Message), "think") {
		// An older or non-reasoning model that rejects the "think" switch.
		delete(req, "think")
		err = c.do(ctx, http.MethodPost, "/api/chat", req, model, &resp)
	}
	if err != nil {
		return err
	}
	content := stripFences(resp.Message.Content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("the model did not return valid JSON: %w", err)
	}
	return nil
}

// stripFences removes a ```json fence that some models add around JSON.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

const maxResponse = 64 << 20

func (c *Client) do(ctx context.Context, method, path string, body any, model string, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return err
		}
		return &UnavailableError{Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error string `json:"error"`
		}
		text := strings.TrimSpace(string(msg))
		if json.Unmarshal(msg, &e) == nil && e.Error != "" {
			text = e.Error
		}
		if resp.StatusCode == http.StatusNotFound && model != "" && strings.Contains(strings.ToLower(text), "not found") {
			return &ModelMissingError{Model: model}
		}
		return &StatusError{Status: resp.StatusCode, Message: text}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(out); err != nil {
		return fmt.Errorf("could not read the response from ollama: %w", err)
	}
	return nil
}
