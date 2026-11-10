package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath   string        `yaml:"db_path"`
	HTTPAddr string        `yaml:"http_addr"`
	Tick     time.Duration `yaml:"tick"`
	// Contact is appended to the User-Agent so API owners can reach you.
	Contact string `yaml:"contact"`

	Sources map[string]SourceConfig `yaml:"sources"`
	Boards  Boards                  `yaml:"boards"`
	Filter  Filter                  `yaml:"filter"`
}

type SourceConfig struct {
	Enabled bool `yaml:"enabled"`
	// Interval overrides the source's default minimum time between polls.
	Interval time.Duration `yaml:"interval"`
}

// Boards lists company job boards on the three public ATS APIs.
type Boards struct {
	Greenhouse []string `yaml:"greenhouse"`
	Lever      []string `yaml:"lever"`
	Ashby      []string `yaml:"ashby"`
}

// Filter holds the regular expressions behind each funnel stage.
type Filter struct {
	// RegionAllow matches location text that includes the candidate's region.
	RegionAllow string `yaml:"region_allow"`
	// RegionDeny matches location text that clearly excludes it; only used for
	// free-text locations where nothing in RegionAllow matched.
	RegionDeny       string `yaml:"region_deny"`
	TitleInclude     string `yaml:"title_include"`
	TitleExclude     string `yaml:"title_exclude"`
	Stack            string `yaml:"stack"`
	SeniorityExclude string `yaml:"seniority_exclude"`
	B2B              string `yaml:"b2b"`
}

func Default() Config {
	on := SourceConfig{Enabled: true}
	return Config{
		DBPath:   "data/jobpilot.db",
		HTTPAddr: "127.0.0.1:8080",
		Tick:     15 * time.Minute,
		Sources: map[string]SourceConfig{
			"remoteok":       on,
			"remotive":       on,
			"himalayas":      on,
			"weworkremotely": on,
			"jobicy":         on,
			"arbeitnow":      on,
			"hackernews":     on,
			"greenhouse":     on,
			"lever":          on,
			"ashby":          on,
		},
		Filter: Filter{
			RegionAllow: `(?i)\b(worldwide|world wide|anywhere|global|international|europe|european|eu|emea|cee|romania|any time ?zone)\b`,
			// (?i:...) is scoped on purpose: US/UK/NA and Go must stay case-sensitive.
			RegionDeny:       `(?i:\b(usa|united states|u\.s\.|canada|united kingdom|australia|new zealand|india|brazil|mexico|latam|latin america|apac|asia|africa|north america|americas|germany|france|spain|italy|netherlands|poland|singapore|japan)\b)|\b(US|UK|NA)\b`,
			TitleInclude:     `(?i)\b(engineer|developer|programmer|software|backend|back-end|full[- ]?stack|architect|sre|devops|platform)\b`,
			TitleExclude:     `(?i)\b(sales|account (executive|manager)|marketing|recruit\w*|designer|support|customer|hr|legal|finance|accountant|writer|content|analyst|qa|quality|test|mobile|ios|android|embedded|wordpress|php|ruby|java|\.net|c#|c\+\+|salesforce|sap|engineering manager|product manager|project manager|data scientist|machine learning|ml)\b`,
			Stack:            `(?i:\b(golang|typescript|react|node\.?js|next\.?js)\b)|\bGo\b`,
			SeniorityExclude: `(?i)\b(junior|jr\.?|intern(ship)?|graduate|entry[- ]level|trainee|apprentice|mid[- ]level|middle|associate)\b`,
			B2B:              `(?i)\b(b2b|independent contractors?|freelanc\w*|self[- ]employed|1099|contract (role|position|basis|work|engagement)|contract-to-hire|contractor (role|position|basis|engagement|agreement))\b`,
		},
	}
}

// Load reads path over the defaults. A missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
