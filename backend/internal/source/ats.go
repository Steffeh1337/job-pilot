package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// The three adapters below read public company job boards. They fetch each
// configured company in turn and keep going if one board fails.

// ---------- Greenhouse ----------

type Greenhouse struct {
	base
	BaseURL string
	Boards  []string
}

func NewGreenhouse(c *Client, boards []string) *Greenhouse {
	return &Greenhouse{base{"greenhouse", time.Hour, c}, "https://boards-api.greenhouse.io", boards}
}

func (s *Greenhouse) Fetch(ctx context.Context) ([]*domain.Job, error) {
	return fetchBoards(ctx, s.Boards, func(ctx context.Context, token string) ([]*domain.Job, error) {
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/v1/boards/%s/jobs?content=true", s.BaseURL, url.PathEscape(token)))
		if err != nil {
			return nil, err
		}
		return parseGreenhouse(body, token)
	})
}

func parseGreenhouse(body []byte, token string) ([]*domain.Job, error) {
	var raw struct {
		Jobs []struct {
			ID             flexString `json:"id"`
			Title          string     `json:"title"`
			URL            string     `json:"absolute_url"`
			Company        string     `json:"company_name"`
			Content        string     `json:"content"`
			FirstPublished string     `json:"first_published"`
			Location       struct {
				Name string `json:"name"`
			} `json:"location"`
			Departments []struct {
				Name string `json:"name"`
			} `json:"departments"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("greenhouse %s: %w", token, err)
	}
	out := make([]*domain.Job, 0, len(raw.Jobs))
	for _, r := range raw.Jobs {
		company := r.Company
		if company == "" {
			company = token
		}
		var tags []string
		for _, d := range r.Departments {
			tags = append(tags, d.Name)
		}
		out = append(out, &domain.Job{
			Source: "greenhouse", ExternalID: token + ":" + string(r.ID), URL: r.URL,
			Title: r.Title, Company: company, Location: r.Location.Name,
			Regions: locationRegions(r.Location.Name),
			Remote:  strings.Contains(strings.ToLower(r.Location.Name), "remote"),
			Tags:    tags, Description: StripHTML(r.Content),
			PostedAt: parseTime(r.FirstPublished, time.RFC3339),
		})
	}
	return out, nil
}

// ---------- Lever ----------

type Lever struct {
	base
	BaseURL string
	Boards  []string
}

func NewLever(c *Client, boards []string) *Lever {
	return &Lever{base{"lever", time.Hour, c}, "https://api.lever.co", boards}
}

func (s *Lever) Fetch(ctx context.Context) ([]*domain.Job, error) {
	return fetchBoards(ctx, s.Boards, func(ctx context.Context, token string) ([]*domain.Job, error) {
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/v0/postings/%s?mode=json", s.BaseURL, url.PathEscape(token)))
		if err != nil {
			return nil, err
		}
		return parseLever(body, token)
	})
}

func parseLever(body []byte, token string) ([]*domain.Job, error) {
	var raw []struct {
		ID         string `json:"id"`
		Text       string `json:"text"`
		HostedURL  string `json:"hostedUrl"`
		CreatedAt  int64  `json:"createdAt"` // milliseconds
		Workplace  string `json:"workplaceType"`
		Categories struct {
			Commitment   string   `json:"commitment"`
			Location     string   `json:"location"`
			Team         string   `json:"team"`
			AllLocations []string `json:"allLocations"`
		} `json:"categories"`
		OpeningPlain     string `json:"openingPlain"`
		DescriptionPlain string `json:"descriptionBodyPlain"`
		AdditionalPlain  string `json:"additionalPlain"`
		Lists            []struct {
			Text    string `json:"text"`
			Content string `json:"content"`
		} `json:"lists"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("lever %s: %w", token, err)
	}
	out := make([]*domain.Job, 0, len(raw))
	for _, r := range raw {
		locs := r.Categories.AllLocations
		if len(locs) == 0 && r.Categories.Location != "" {
			locs = []string{r.Categories.Location}
		}
		desc := []string{r.OpeningPlain, r.DescriptionPlain}
		for _, l := range r.Lists {
			desc = append(desc, l.Text, StripHTML(l.Content))
		}
		desc = append(desc, r.AdditionalPlain)
		var tags []string
		if r.Categories.Team != "" {
			tags = []string{r.Categories.Team}
		}
		out = append(out, &domain.Job{
			Source: "lever", ExternalID: token + ":" + r.ID, URL: r.HostedURL,
			Title: r.Text, Company: token, Location: strings.Join(locs, ", "),
			Regions: locationRegions(locs...),
			Remote:  strings.EqualFold(r.Workplace, "remote"), EmploymentType: r.Categories.Commitment,
			Tags: tags, Description: truncate(strings.TrimSpace(strings.Join(desc, "\n"))),
			PostedAt: time.UnixMilli(r.CreatedAt),
		})
	}
	return out, nil
}

// ---------- Ashby ----------

type Ashby struct {
	base
	BaseURL string
	Boards  []string
}

func NewAshby(c *Client, boards []string) *Ashby {
	return &Ashby{base{"ashby", time.Hour, c}, "https://api.ashbyhq.com", boards}
}

func (s *Ashby) Fetch(ctx context.Context) ([]*domain.Job, error) {
	return fetchBoards(ctx, s.Boards, func(ctx context.Context, token string) ([]*domain.Job, error) {
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/posting-api/job-board/%s?includeCompensation=true", s.BaseURL, url.PathEscape(token)))
		if err != nil {
			return nil, err
		}
		return parseAshby(body, token)
	})
}

func parseAshby(body []byte, token string) ([]*domain.Job, error) {
	var raw struct {
		Jobs []struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Department     string `json:"department"`
			Team           string `json:"team"`
			EmploymentType string `json:"employmentType"`
			Location       string `json:"location"`
			Secondary      []struct {
				Location string `json:"location"`
				Address  struct {
					Postal struct {
						Country string `json:"addressCountry"`
					} `json:"postalAddress"`
				} `json:"address"`
			} `json:"secondaryLocations"`
			PublishedAt  string `json:"publishedAt"`
			IsListed     *bool  `json:"isListed"`
			IsRemote     bool   `json:"isRemote"`
			Workplace    string `json:"workplaceType"`
			JobURL       string `json:"jobUrl"`
			Description  string `json:"descriptionPlain"`
			Compensation struct {
				Summary []struct {
					Type     string  `json:"compensationType"`
					Currency string  `json:"currencyCode"`
					Min      float64 `json:"minValue"`
					Max      float64 `json:"maxValue"`
				} `json:"summaryComponents"`
			} `json:"compensation"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("ashby %s: %w", token, err)
	}
	out := make([]*domain.Job, 0, len(raw.Jobs))
	for _, r := range raw.Jobs {
		if r.IsListed != nil && !*r.IsListed {
			continue
		}
		// Primary and secondary locations together say where a remote job is open.
		locs := []string{r.Location}
		for _, sec := range r.Secondary {
			locs = append(locs, sec.Location)
			if c := sec.Address.Postal.Country; c != "" && c != sec.Location {
				locs = append(locs, c)
			}
		}
		var tags []string
		for _, t := range []string{r.Department, r.Team} {
			if t != "" {
				tags = append(tags, t)
			}
		}
		j := &domain.Job{
			Source: "ashby", ExternalID: token + ":" + r.ID, URL: r.JobURL,
			Title: r.Title, Company: token, Location: strings.Join(locs, ", "),
			Regions: locationRegions(locs...),
			// isRemote is also true for hybrid roles, so workplaceType wins
			// when it is present.
			Remote: strings.EqualFold(r.Workplace, "remote") || (r.Workplace == "" && r.IsRemote), EmploymentType: r.EmploymentType,
			Tags: tags, Description: truncate(r.Description),
			PostedAt: parseTime(r.PublishedAt, time.RFC3339, "2006-01-02T15:04:05.000-07:00"),
		}
		for _, c := range r.Compensation.Summary {
			if c.Type == "Salary" {
				j.SalaryMin, j.SalaryMax, j.Currency = int(c.Min), int(c.Max), c.Currency
			}
		}
		out = append(out, j)
	}
	return out, nil
}

func truncate(s string) string {
	if len(s) > maxDescription {
		return s[:maxDescription]
	}
	return s
}
