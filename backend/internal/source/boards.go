package source

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// ---------- RemoteOK ----------

type RemoteOK struct {
	base
	BaseURL string
}

func NewRemoteOK(c *Client) *RemoteOK {
	return &RemoteOK{base{"remoteok", 30 * time.Minute, c}, "https://remoteok.com"}
}

func (s *RemoteOK) Fetch(ctx context.Context) ([]*domain.Job, error) {
	body, err := s.client.Get(ctx, s.BaseURL+"/api")
	if err != nil {
		return nil, err
	}
	return parseRemoteOK(body)
}

func parseRemoteOK(body []byte) ([]*domain.Job, error) {
	// The first array element is a legal notice, not a job.
	var raw []struct {
		ID          flexString `json:"id"`
		Epoch       int64      `json:"epoch"`
		Company     string     `json:"company"`
		Position    string     `json:"position"`
		Tags        []string   `json:"tags"`
		Location    string     `json:"location"`
		URL         string     `json:"url"`
		Description string     `json:"description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("remoteok: %w", err)
	}
	var out []*domain.Job
	for _, r := range raw {
		if r.ID == "" || r.Position == "" {
			continue
		}
		out = append(out, &domain.Job{
			Source: "remoteok", ExternalID: string(r.ID), URL: r.URL,
			Title: r.Position, Company: r.Company, Location: r.Location, Remote: true,
			Tags: r.Tags, Description: StripHTML(r.Description),
			PostedAt: time.Unix(r.Epoch, 0),
		})
	}
	return out, nil
}

// ---------- Remotive ----------

type Remotive struct {
	base
	BaseURL string
}

// Remotive asks API users to poll sparingly, so the default interval is long.
func NewRemotive(c *Client) *Remotive {
	return &Remotive{base{"remotive", 6 * time.Hour, c}, "https://remotive.com"}
}

func (s *Remotive) Fetch(ctx context.Context) ([]*domain.Job, error) {
	body, err := s.client.Get(ctx, s.BaseURL+"/api/remote-jobs?category=software-dev")
	if err != nil {
		return nil, err
	}
	return parseRemotive(body)
}

func parseRemotive(body []byte) ([]*domain.Job, error) {
	var raw struct {
		Jobs []struct {
			ID          flexString `json:"id"`
			URL         string     `json:"url"`
			Title       string     `json:"title"`
			Company     string     `json:"company_name"`
			Tags        []string   `json:"tags"`
			JobType     string     `json:"job_type"`
			Published   string     `json:"publication_date"`
			Location    string     `json:"candidate_required_location"`
			Salary      string     `json:"salary"`
			Description string     `json:"description"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("remotive: %w", err)
	}
	out := make([]*domain.Job, 0, len(raw.Jobs))
	for _, r := range raw.Jobs {
		out = append(out, &domain.Job{
			Source: "remotive", ExternalID: string(r.ID), URL: r.URL,
			Title: r.Title, Company: r.Company, Remote: true,
			Regions: splitList(r.Location), EmploymentType: r.JobType, Tags: r.Tags,
			Description: StripHTML(r.Description),
			PostedAt:    parseTime(r.Published, "2006-01-02T15:04:05", time.RFC3339),
		})
	}
	return out, nil
}

// ---------- Himalayas ----------

type Himalayas struct {
	base
	BaseURL string
	Pages   int // 20 jobs per page, newest first
}

func NewHimalayas(c *Client) *Himalayas {
	return &Himalayas{base{"himalayas", time.Hour, c}, "https://himalayas.app", 5}
}

func (s *Himalayas) Fetch(ctx context.Context) ([]*domain.Job, error) {
	var all []*domain.Job
	for p := 0; p < s.Pages; p++ {
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/jobs/api?limit=20&offset=%d", s.BaseURL, p*20))
		if err != nil {
			return all, err
		}
		jobs, err := parseHimalayas(body)
		if err != nil {
			return all, err
		}
		if len(jobs) == 0 {
			break
		}
		all = append(all, jobs...)
	}
	return all, nil
}

func parseHimalayas(body []byte) ([]*domain.Job, error) {
	var raw struct {
		Jobs []struct {
			Title          string   `json:"title"`
			Company        string   `json:"companyName"`
			EmploymentType string   `json:"employmentType"`
			MinSalary      flexInt  `json:"minSalary"`
			MaxSalary      flexInt  `json:"maxSalary"`
			Currency       string   `json:"currency"`
			Seniority      []string `json:"seniority"`
			Restrictions   []string `json:"locationRestrictions"`
			Categories     []string `json:"categories"`
			Description    string   `json:"description"`
			Excerpt        string   `json:"excerpt"`
			PubDate        int64    `json:"pubDate"`
			GUID           string   `json:"guid"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("himalayas: %w", err)
	}
	out := make([]*domain.Job, 0, len(raw.Jobs))
	for _, r := range raw.Jobs {
		desc := r.Description
		if desc == "" {
			desc = r.Excerpt
		}
		out = append(out, &domain.Job{
			Source: "himalayas", ExternalID: r.GUID, URL: r.GUID,
			Title: r.Title, Company: r.Company, Remote: true,
			// An empty restriction list means the job is open worldwide.
			Regions: r.Restrictions, EmploymentType: r.EmploymentType,
			Tags:        append(append([]string{}, r.Categories...), r.Seniority...),
			Description: StripHTML(desc),
			SalaryMin:   int(r.MinSalary), SalaryMax: int(r.MaxSalary), Currency: r.Currency,
			PostedAt: time.Unix(r.PubDate, 0),
		})
	}
	return out, nil
}

// ---------- We Work Remotely (RSS) ----------

type WeWorkRemotely struct {
	base
	BaseURL string
	Feeds   []string
}

func NewWeWorkRemotely(c *Client) *WeWorkRemotely {
	return &WeWorkRemotely{base{"weworkremotely", time.Hour, c}, "https://weworkremotely.com",
		[]string{"remote-full-stack-programming-jobs", "remote-back-end-programming-jobs"}}
}

func (s *WeWorkRemotely) Fetch(ctx context.Context) ([]*domain.Job, error) {
	var (
		all  []*domain.Job
		errs []error
	)
	for _, f := range s.Feeds {
		body, err := s.client.Get(ctx, fmt.Sprintf("%s/categories/%s.rss", s.BaseURL, f))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		jobs, err := parseWWR(body)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, jobs...)
	}
	return all, errors.Join(errs...)
}

func parseWWR(body []byte) ([]*domain.Job, error) {
	// Naming the root element makes an HTML block page an error instead of
	// an empty feed that looks like "no new jobs".
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Items   []struct {
			Title       string `xml:"title"`
			Region      string `xml:"region"`
			Country     string `xml:"country"`
			Skills      string `xml:"skills"`
			Type        string `xml:"type"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        string `xml:"guid"`
			Link        string `xml:"link"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("weworkremotely: %w", err)
	}
	out := make([]*domain.Job, 0, len(feed.Items))
	for _, it := range feed.Items {
		// Titles look like "Company: Role".
		company, title, found := strings.Cut(it.Title, ": ")
		if !found {
			company, title = "", it.Title
		}
		var regions []string
		for _, r := range []string{it.Region, it.Country} {
			if r = strings.TrimSpace(r); r != "" {
				regions = append(regions, r)
			}
		}
		out = append(out, &domain.Job{
			Source: "weworkremotely", ExternalID: it.GUID, URL: it.Link,
			Title: strings.TrimSpace(title), Company: strings.TrimSpace(company), Remote: true,
			Regions: regions, EmploymentType: it.Type, Tags: splitList(it.Skills),
			Description: StripHTML(it.Description),
			PostedAt:    parseTime(it.PubDate, time.RFC1123Z, time.RFC1123),
		})
	}
	return out, nil
}

// ---------- Jobicy ----------

type Jobicy struct {
	base
	BaseURL string
}

func NewJobicy(c *Client) *Jobicy {
	return &Jobicy{base{"jobicy", time.Hour, c}, "https://jobicy.com"}
}

func (s *Jobicy) Fetch(ctx context.Context) ([]*domain.Job, error) {
	body, err := s.client.Get(ctx, s.BaseURL+"/api/v2/remote-jobs?count=100&industry=dev")
	if err != nil {
		return nil, err
	}
	return parseJobicy(body)
}

func parseJobicy(body []byte) ([]*domain.Job, error) {
	var raw struct {
		Jobs []struct {
			ID          flexString `json:"id"`
			URL         string     `json:"url"`
			Title       string     `json:"jobTitle"`
			Company     string     `json:"companyName"`
			Industry    []string   `json:"jobIndustry"`
			Type        []string   `json:"jobType"`
			Geo         string     `json:"jobGeo"`
			Level       string     `json:"jobLevel"`
			Excerpt     string     `json:"jobExcerpt"`
			Description string     `json:"jobDescription"`
			PubDate     string     `json:"pubDate"`
			SalaryMin   flexInt    `json:"salaryMin"`
			SalaryMax   flexInt    `json:"salaryMax"`
			Currency    string     `json:"salaryCurrency"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("jobicy: %w", err)
	}
	out := make([]*domain.Job, 0, len(raw.Jobs))
	for _, r := range raw.Jobs {
		desc := r.Description
		if desc == "" {
			desc = r.Excerpt
		}
		tags := append([]string{}, r.Industry...)
		if r.Level != "" {
			tags = append(tags, r.Level)
		}
		out = append(out, &domain.Job{
			Source: "jobicy", ExternalID: string(r.ID), URL: r.URL,
			Title: r.Title, Company: r.Company, Remote: true,
			Regions: splitList(r.Geo), EmploymentType: strings.Join(r.Type, ", "), Tags: tags,
			Description: StripHTML(desc),
			SalaryMin:   int(r.SalaryMin), SalaryMax: int(r.SalaryMax), Currency: r.Currency,
			PostedAt: parseTime(r.PubDate, time.RFC3339),
		})
	}
	return out, nil
}

// ---------- Arbeitnow ----------

type Arbeitnow struct {
	base
	BaseURL string
}

func NewArbeitnow(c *Client) *Arbeitnow {
	return &Arbeitnow{base{"arbeitnow", time.Hour, c}, "https://www.arbeitnow.com"}
}

// Fetch reads the newest page (300 jobs, newest first), which covers an hour
// of postings with plenty of room.
func (s *Arbeitnow) Fetch(ctx context.Context) ([]*domain.Job, error) {
	body, err := s.client.Get(ctx, s.BaseURL+"/api/job-board-api")
	if err != nil {
		return nil, err
	}
	return parseArbeitnow(body)
}

func parseArbeitnow(body []byte) ([]*domain.Job, error) {
	var raw struct {
		Data []struct {
			Slug        string   `json:"slug"`
			Company     string   `json:"company_name"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Remote      bool     `json:"remote"`
			URL         string   `json:"url"`
			Tags        []string `json:"tags"`
			JobTypes    []string `json:"job_types"`
			Location    string   `json:"location"`
			CreatedAt   int64    `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("arbeitnow: %w", err)
	}
	out := make([]*domain.Job, 0, len(raw.Data))
	for _, r := range raw.Data {
		var regions []string
		if r.Location != "" {
			// A city here is the employer's base; it has to be allowed explicitly.
			regions = []string{r.Location}
		}
		out = append(out, &domain.Job{
			Source: "arbeitnow", ExternalID: r.Slug, URL: r.URL,
			Title: r.Title, Company: r.Company, Remote: r.Remote,
			Regions: regions, EmploymentType: strings.Join(r.JobTypes, ", "), Tags: r.Tags,
			Description: StripHTML(r.Description),
			PostedAt:    time.Unix(r.CreatedAt, 0),
		})
	}
	return out, nil
}
