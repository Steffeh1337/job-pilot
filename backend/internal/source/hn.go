package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// HackerNews reads the monthly "Who is hiring?" thread and the monthly
// "Freelancer? Seeking freelancer?" thread through the Algolia HN API.
type HackerNews struct {
	base
	BaseURL string // Algolia API root
}

func NewHackerNews(c *Client) *HackerNews {
	return &HackerNews{base{"hackernews", 6 * time.Hour, c}, "https://hn.algolia.com/api/v1"}
}

type hnThread struct {
	query  string
	prefix string // title prefix identifying the right thread
	// onlyHeader keeps just comments whose first line starts with it; the
	// freelancer thread mixes people offering work with people seeking it.
	onlyHeader string
	employment string
}

var hnThreads = []hnThread{
	{query: "Ask HN: Who is hiring?", prefix: "Ask HN: Who is hiring?"},
	{query: "Ask HN: Freelancer? Seeking freelancer?", prefix: "Ask HN: Freelancer? Seeking freelancer?",
		onlyHeader: "SEEKING FREELANCER", employment: "freelance"},
}

func (s *HackerNews) Fetch(ctx context.Context) ([]*domain.Job, error) {
	var (
		all  []*domain.Job
		errs []error
	)
	for _, t := range hnThreads {
		id, err := s.latestThread(ctx, t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/items/%s", s.BaseURL, id))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		jobs, err := parseHNThread(body, t)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, jobs...)
	}
	return all, errors.Join(errs...)
}

func (s *HackerNews) latestThread(ctx context.Context, t hnThread) (string, error) {
	u := fmt.Sprintf("%s/search_by_date?query=%s&tags=story,author_whoishiring&hitsPerPage=5",
		s.BaseURL, url.QueryEscape(`"`+t.query+`"`))
	var res struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	if err := s.client.GetJSON(ctx, u, &res); err != nil {
		return "", err
	}
	for _, h := range res.Hits {
		if strings.HasPrefix(h.Title, t.prefix) {
			return h.ObjectID, nil
		}
	}
	return "", fmt.Errorf("hackernews: no %q thread found", t.prefix)
}

var hnRemoteRE = regexp.MustCompile(`(?i)\bremote\b`)

func parseHNThread(body []byte, t hnThread) ([]*domain.Job, error) {
	var thread struct {
		ID       flexString `json:"id"`
		Children []struct {
			ID        flexString `json:"id"`
			CreatedAt string     `json:"created_at"`
			Text      string     `json:"text"`
		} `json:"children"`
	}
	if err := json.Unmarshal(body, &thread); err != nil {
		return nil, fmt.Errorf("hackernews: %w", err)
	}
	var out []*domain.Job
	for _, c := range thread.Children {
		text := StripHTML(c.Text)
		if text == "" {
			continue // deleted or dead
		}
		header, _, _ := strings.Cut(text, "\n")
		if t.onlyHeader != "" && !strings.HasPrefix(strings.ToUpper(header), t.onlyHeader) {
			continue
		}
		// Headers are free-form but usually "Company | Role | Location | ...".
		segs := strings.Split(header, "|")
		for i := range segs {
			segs[i] = strings.TrimSpace(segs[i])
		}
		company, title := segs[0], header
		if len(segs) > 1 {
			title = strings.Join(segs[1:], " | ")
		}
		if len(title) > 200 {
			title = title[:200]
		}
		out = append(out, &domain.Job{
			Source: "hackernews", ExternalID: string(c.ID),
			URL:   "https://news.ycombinator.com/item?id=" + string(c.ID),
			Title: title, Company: company, Location: header,
			Remote: hnRemoteRE.MatchString(header), EmploymentType: t.employment,
			Description: text,
			PostedAt:    parseTime(c.CreatedAt, time.RFC3339),
		})
	}
	return out, nil
}
