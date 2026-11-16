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
	// B2B matches wording that says the engagement is contract, freelance,
	// independent or short-term work. It runs on the description after
	// B2BIgnore has removed boilerplate that only looks like a signal.
	B2B string `yaml:"b2b"`
	// B2BIgnore matches text to blank out before looking for B2B wording:
	// "B2B SaaS", "smart contract", "employees and contractors", and so on.
	B2BIgnore string `yaml:"b2b_ignore"`
	// B2BTitle matches a title that states the engagement ("(Contract)").
	B2BTitle string `yaml:"b2b_title"`
}

// b2bIgnore blanks out text that mentions contracts or B2B without saying
// anything about how the candidate would be engaged.
const b2bIgnore = `(?i)` +
	`\bsmart[- ]contracts?\b` +
	`|\b(?:api|service|data|interface|rest|openapi|graphql|abi|protobuf|schema)[- ]contracts?\b` +
	`|\bcontracts?[- ](?:design|law|negotiation|management|review|deliverables?|testing|tests?|first|driven|address)\b` +
	`|\b(?:government|public|enterprise|client|customer|vendor|supplier)\s+contracts?\b` +
	`|\b(?:employees?|staff|workers)\s*(?:,|/|and|or|&)\s*(?:\w+\s*,\s*)?(?:and\s+)?contractors?\b` +
	`|\bcontractors?\s*(?:,|/|and|or|&)\s*(?:employees?|staff|vendors?|consultants?|suppliers?|partners?)\b` +
	`|\bcontractual\b` +
	`|\bb2b\s*(?:/|,|and|or|&|-)?\s*(?:b2c|b2b2c|d2c|saas|software|products?|company|companies|technology|tech|marketplace|platform|sales|startup|customers?|clients?|payments?|fintech|applications?|apps?|stack|business(?:es)?|space|solutions?|services?|commerce|e-commerce|ecosystem|brands?|industry|enterprise|environment|world|market|vertical|infrastructure|data|growth)\b` +
	`|\bindependent\s+(?:non-?profit|company|research|organi[sz]ation|think|variable|test)`

// b2bPattern matches wording for contract, freelance, independent and
// short-term engagements. A bare "contract" is deliberately absent: it is
// mostly "smart contract" or "contract design".
const b2bPattern = `(?i)(?:` +
	`\bb2b\s*[/,-]?\s*(?:contract\w*|basis|engagement|agreement|set[- ]?up|arrangement|collaboration|invoic\w*|freelanc\w*|only|or)\b` +
	`|\b(?:on|via|through|under)\s+(?:a\s+)?b2b\b` +
	`|\b(?:independent|freelance)\s+(?:contractors?|consultants?|professionals?|engineers?|developers?|basis|work)\b` +
	`|\bfreelanc\w*` +
	`|\bcontractors?\b` +
	`|\bcontract[- ](?:role|position|basis|work|engagement|opportunity|assignment|job|based|term|only)\b` +
	`|\bcontract[- ]to[- ]hire\b|\bc2h\b` +
	`|\b(?:short|long|fixed)[- ]term\s+(?:contract|role|position|project|engagement|assignment|basis|work|opportunity|employment)\b` +
	`|\b(?:temporary|interim)\s+(?:role|position|basis|contract|assignment|engagement|work)\b` +
	`|\b\d+[- ]?(?:month|mo|week|year)s?\s+contract\b` +
	`|\bcontract\s*,\s*\d` +
	`|\b(?:permanent|full[- ]time|employment)\s+or\s+contract\b|\bcontract\s+or\s+(?:permanent|full[- ]time|employment)\b` +
	`|\(contract\)|\bon\s+a\s+contract\b` +
	`|\bproject[- ]based\b|\bper[- ]project\b|\b(?:hourly|daily|day)\s+rate\b|\b\d+(?:\.\d+)?\s*/\s*(?:hr|hour|day)\b` +
	`|\bself[- ]employed\b|\b1099\b|\bconsulting\s+(?:basis|engagement|contract|assignment)\b` +
	`)`

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
			B2B:              b2bPattern,
			B2BIgnore:        b2bIgnore,
			B2BTitle:         `(?i)\b(?:contract|contractor|freelanc\w*|temporary|interim|b2b)\b`,
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
