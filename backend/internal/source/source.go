// Package source holds one adapter per job board or ATS. Each adapter turns
// the board's public API response into domain.Job values.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// Source fetches the currently open jobs from one board.
type Source interface {
	Name() string
	// Interval is the minimum time between polls. Most boards refresh hourly
	// or slower, so polling them every 15 minutes only wastes their bandwidth.
	Interval() time.Duration
	// Fetch may return jobs together with an error when only part of the
	// source failed (for example one company board out of fifty).
	Fetch(ctx context.Context) ([]*domain.Job, error)
}

type base struct {
	name     string
	interval time.Duration
	client   *Client
}

func (b base) Name() string            { return b.name }
func (b base) Interval() time.Duration { return b.interval }

// Client is a small HTTP client shared by all adapters.
type Client struct {
	HTTP *http.Client
	UA   string
}

func NewClient(contact string) *Client {
	ua := "job-pilot/0.1 (personal job search)"
	if contact != "" {
		ua += " " + contact
	}
	return &Client{HTTP: &http.Client{Timeout: 45 * time.Second}, UA: ua}
}

const maxBody = 64 << 20

func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept", "application/json, application/xml;q=0.9, */*;q=0.5")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return body, nil
}

func (c *Client) GetJSON(ctx context.Context, url string, v any) error {
	body, err := c.Get(ctx, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

// Build returns the enabled sources from cfg.
func Build(cfg config.Config, c *Client) []Source {
	all := []Source{
		NewRemoteOK(c),
		NewRemotive(c),
		NewHimalayas(c),
		NewWeWorkRemotely(c),
		NewJobicy(c),
		NewArbeitnow(c),
		NewHackerNews(c),
		NewGreenhouse(c, cfg.Boards.Greenhouse),
		NewLever(c, cfg.Boards.Lever),
		NewAshby(c, cfg.Boards.Ashby),
	}
	var out []Source
	for _, s := range all {
		sc, ok := cfg.Sources[s.Name()]
		if !ok || !sc.Enabled {
			continue
		}
		if sc.Interval > 0 {
			s = withInterval{s, sc.Interval}
		}
		out = append(out, s)
	}
	return out
}

type withInterval struct {
	Source
	d time.Duration
}

func (w withInterval) Interval() time.Duration { return w.d }

// --- helpers shared by adapters ---

var (
	blockRE = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|h[1-6]|tr)\b[^>]*>`)
	tagRE   = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRE = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)
	nlRE    = regexp.MustCompile(`\s*\n\s*`)
)

const maxDescription = 20000

// StripHTML converts an HTML fragment (possibly entity-escaped, as Greenhouse
// and WWR deliver it) to plain text with line breaks kept at block elements.
func StripHTML(s string) string {
	if strings.Contains(s, "&lt;") {
		s = html.UnescapeString(s)
	}
	s = blockRE.ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spaceRE.ReplaceAllString(s, " ")
	s = nlRE.ReplaceAllString(s, "\n")
	s = strings.TrimSpace(s)
	if len(s) > maxDescription {
		s = s[:maxDescription]
	}
	return s
}

// splitList splits "EMEA, LATAM; Canada" into trimmed, non-empty parts.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var (
	remoteWordRE = regexp.MustCompile(`(?i)\bremote\b`)
	alnumRE      = regexp.MustCompile(`[\p{L}\p{N}]`)
)

// locationRegions turns clean location strings from an ATS into structured
// restrictions. A bare "Remote" says nothing about where, so it is dropped,
// and a job with only bare "Remote" locations has no restriction recorded.
// Anything else ("San Francisco", "Remote - US") must be allowed explicitly.
func locationRegions(locs ...string) []string {
	var out []string
	for _, l := range locs {
		if alnumRE.MatchString(remoteWordRE.ReplaceAllString(l, "")) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func parseTime(s string, layouts ...string) time.Time {
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// flexString decodes a JSON string or number into a string.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = flexString(v)
		return nil
	}
	*f = flexString(s)
	return nil
}

// flexInt decodes a JSON number or numeric string; anything else becomes 0.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(n)
	return nil
}

// fetchBoards runs fn for each company token and keeps going when one fails.
func fetchBoards(ctx context.Context, tokens []string, fn func(ctx context.Context, token string) ([]*domain.Job, error)) ([]*domain.Job, error) {
	var (
		jobs []*domain.Job
		errs []error
	)
	for _, t := range tokens {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		js, err := fn(ctx, t)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t, err))
			continue
		}
		jobs = append(jobs, js...)
	}
	return jobs, errors.Join(errs...)
}
