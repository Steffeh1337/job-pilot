package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBPath   string `yaml:"db_path"`
	HTTPAddr string `yaml:"http_addr"`
	// WebDir is the built dashboard (web/dist); served at "/" if it exists.
	WebDir string        `yaml:"web_dir"`
	Tick   time.Duration `yaml:"tick"`
	// Contact is appended to the User-Agent so API owners can reach you.
	Contact string `yaml:"contact"`

	Sources map[string]SourceConfig `yaml:"sources"`
	Boards  Boards                  `yaml:"boards"`
	Filter  Filter                  `yaml:"filter"`
	Ollama  Ollama                  `yaml:"ollama"`
	Fit     Fit                     `yaml:"fit"`
}

// Ollama configures the local model server used to read your CV and to
// compare it with jobs. Your CV text is sent to this server and nowhere else,
// so by default only an address on this machine is accepted.
type Ollama struct {
	// Enabled turns the local model on. Without it, CV matching still works
	// from keywords alone.
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	// AllowRemote permits a server on another machine. Your CV would be sent
	// there; leave it off unless that is what you want.
	AllowRemote bool `yaml:"allow_remote"`
	// EmbedModel turns text into vectors for the semantic score.
	EmbedModel string `yaml:"embed_model"`
	// ChatModel reads the CV into a profile. Empty means use whichever
	// suitable model is installed.
	ChatModel string `yaml:"chat_model"`
	// EmbedPrefix goes in front of every text before embedding. Unset uses the
	// model's recommended prefix; set it to "" for none.
	EmbedPrefix *string       `yaml:"embed_prefix"`
	Timeout     time.Duration `yaml:"timeout"`
}

// Fit tunes the match score. Skill and semantic parts are weights relative to
// each other.
type Fit struct {
	SkillWeight    float64 `yaml:"skill_weight"`
	SemanticWeight float64 `yaml:"semantic_weight"`
	// SemanticLow and SemanticHigh map raw embedding similarity onto 0..100%.
	// Where "related" starts depends on the embedding model.
	SemanticLow  float64 `yaml:"semantic_low"`
	SemanticHigh float64 `yaml:"semantic_high"`
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
	RegionDeny string `yaml:"region_deny"`
	// DescriptionDeny matches restriction phrases in the description ("must be
	// located in the US"). It is only consulted for jobs whose source gave no
	// structured location, such as RemoteOK.
	DescriptionDeny  string `yaml:"description_deny"`
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

// countries are the places whose mention in a restriction phrase rules the
// candidate out. US and UK stay case-sensitive so "join us" never matches.
const countries = `(?:(?-i:US|USA|UK)|u\.s\.|united states|canada|latam|latin america|south america|central america|united kingdom|germany|france|spain|italy|netherlands|poland|india|brazil|mexico|australia|apac|north america)`

// countryList is one or more countries joined by commas, slashes, "and" or
// "or": "United States, Canada, or South America".
const countryList = countries + `(?:\s*(?:,|/|&|and|or)\s*` + countries + `)*`

// descriptionDeny matches phrasing that restricts who may apply:
// "US only", "US-based candidates", "must be located in Canada", "authorized
// to work in the United States", "remote role based in Latin America",
// "Remote (within Germany)", "remote work from the US".
var descriptionDeny = strings.ReplaceAll(strings.ReplaceAll(`(?i)\b(?:`+
	`{L}[- ]?(?:only|residents?|citizens?)\b`+
	`|remote\s*[:\-–—]\s*(?:the\s+)?{L}\b`+
	`|{C}[- ]based\s+(?:candidates?|applicants?|employees?|engineers?|developers?|contractors?)\b`+
	`|(?:must|need to|required to|have to|should|can|may)\s+(?:be\s+)?(?:located|based|reside|residing|living|live|work|working)\s+(?:anywhere\s+)?(?:in|from|within)\s+(?:the\s+)?{C}\b`+
	`|(?:role|position)\s+(?:is\s+)?(?:based|located)\s+(?:in|within)\s+(?:the\s+)?{C}\b`+
	`|(?:authori[sz]ed|eligible|legally entitled)\s+to\s+work\s+(?:in|within)\s+(?:the\s+)?{C}\b`+
	`|remote\s*\(\s*(?:within\s+)?(?:the\s+)?{C}\s*\)`+
	`|remote\b[^.\n]{0,30}?\b(?:within|in|from)\s+(?:the\s+)?{C}\b`+
	`)`, "{L}", countryList), "{C}", countries)

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
		WebDir:   "../web/dist",
		Tick:     15 * time.Minute,
		Sources: map[string]SourceConfig{
			"remoteok":       on,
			"remotive":       on,
			"himalayas":      on,
			"weworkremotely": on,
			"jobicy":         on,
			// 0 of 286 jobs passed the region stage in the first measurement:
			// it lists German-market jobs. Set enabled: true to re-measure.
			"arbeitnow":  {Enabled: false},
			"hackernews": on,
			"greenhouse": on,
			"lever":      on,
			"ashby":      on,
		},
		Ollama: Ollama{
			Enabled:    true,
			URL:        "http://127.0.0.1:11434",
			EmbedModel: "nomic-embed-text",
			Timeout:    3 * time.Minute,
		},
		Fit: Fit{SkillWeight: 0.6, SemanticWeight: 0.4, SemanticLow: 0.72, SemanticHigh: 0.82},
		Filter: Filter{
			RegionAllow: `(?i)\b(worldwide|world wide|anywhere|global|international|europe|european|eu|emea|cee|romania|any time ?zone)\b`,
			// (?i:...) is scoped on purpose: US/UK/NA and Go must stay case-sensitive.
			RegionDeny:       `(?i:\b(usa|united states|u\.s\.|canada|united kingdom|australia|new zealand|india|brazil|mexico|latam|latin america|apac|asia|africa|north america|americas|germany|france|spain|italy|netherlands|poland|singapore|japan)\b)|\b(US|UK|NA)\b`,
			DescriptionDeny:  descriptionDeny,
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
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate rejects settings that would give meaningless results.
func (c Config) Validate() error {
	f := c.Fit
	switch {
	case f.SkillWeight < 0 || f.SemanticWeight < 0 || f.SkillWeight+f.SemanticWeight <= 0:
		return errors.New("fit: skill_weight and semantic_weight must not be negative, and one must be above 0")
	case f.SemanticHigh <= f.SemanticLow:
		return errors.New("fit: semantic_high must be greater than semantic_low")
	case c.Ollama.Timeout <= 0:
		return errors.New("ollama: timeout must be positive")
	case c.Ollama.Enabled && strings.TrimSpace(c.Ollama.URL) == "":
		return errors.New("ollama: url is required when ollama is enabled")
	case c.Ollama.Enabled && strings.TrimSpace(c.Ollama.EmbedModel) == "":
		return errors.New("ollama: embed_model is required when ollama is enabled")
	}
	return nil
}

// EmbedPrefix is the text put before every embedded string. When it is not
// set, models that expect a prefix get theirs.
func (o Ollama) EmbedPrefixOrDefault() string {
	if o.EmbedPrefix != nil {
		return *o.EmbedPrefix
	}
	if strings.HasPrefix(o.EmbedModel, "nomic-embed-text") {
		return "clustering: "
	}
	return ""
}
