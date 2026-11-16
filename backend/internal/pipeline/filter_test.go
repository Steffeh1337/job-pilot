package pipeline

import (
	"testing"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

func testFilter(t *testing.T) *Filter {
	t.Helper()
	f, err := NewFilter(config.Default().Filter)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		job        domain.Job
		wantStage  domain.Stage
		wantReason string
		wantB2B    bool
	}{
		{
			name:      "worldwide remote senior Go role is a match",
			job:       domain.Job{Title: "Senior Backend Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "We build services in Go and TypeScript."},
			wantStage: domain.StageSenior,
		},
		{
			name:       "on-site role is dropped before region",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: false, Description: "Go"},
			wantStage:  domain.StageUnique,
			wantReason: "not remote",
		},
		{
			name:       "US-only structured restriction",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"USA", "Canada"}},
			wantStage:  domain.StageUnique,
			wantReason: "region",
		},
		{
			name:       "structured restriction must name an allowed region",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Egypt"}},
			wantStage:  domain.StageUnique,
			wantReason: "region",
		},
		{
			name:      "EMEA among several regions is allowed",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"EMEA", "LATAM", "Canada", "USA"}, Description: "Go"},
			wantStage: domain.StageSenior,
		},
		{
			name:      "bare Remote location is unknown, so it passes",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Remote", Description: "Go"},
			wantStage: domain.StageSenior,
		},
		{
			name:       "free-text US location is denied",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Remote, United States"},
			wantStage:  domain.StageUnique,
			wantReason: "region",
		},
		{
			name:       "REMOTE (US) header is denied",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Acme | Senior Go Engineer | REMOTE (US)"},
			wantStage:  domain.StageUnique,
			wantReason: "region",
		},
		{
			name:      "lowercase 'us' is an ordinary word, not the United States",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Remote, come join us", Description: "Go"},
			wantStage: domain.StageSenior,
		},
		{
			name:      "free-text location naming Europe is allowed even with a deny word",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Remote - Europe or US", Description: "Go"},
			wantStage: domain.StageSenior,
		},
		{
			name:       "non-engineering title",
			job:        domain.Job{Title: "Account Executive, Go-to-Market", Remote: true, Regions: []string{"Worldwide"}, Description: "Go"},
			wantStage:  domain.StageRegion,
			wantReason: "role",
		},
		{
			name:       "excluded language in title",
			job:        domain.Job{Title: "Senior Java Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "Go"},
			wantStage:  domain.StageRegion,
			wantReason: "role",
		},
		{
			name:       "lowercase 'go' in prose is not the language",
			job:        domain.Job{Title: "Senior Software Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "We want to go to market quickly."},
			wantStage:  domain.StageRole,
			wantReason: "stack",
		},
		{
			name:      "stack found in tags",
			job:       domain.Job{Title: "Senior Software Engineer", Remote: true, Regions: []string{"Worldwide"}, Tags: []string{"golang"}},
			wantStage: domain.StageSenior,
		},
		{
			name:       "junior title is dropped last",
			job:        domain.Job{Title: "Junior Go Developer", Remote: true, Regions: []string{"Worldwide"}, Description: "Go"},
			wantStage:  domain.StageStack,
			wantReason: "seniority",
		},
		{
			name:      "unmarked level passes",
			job:       domain.Job{Title: "Backend Engineer (Go)", Remote: true, Regions: []string{"Worldwide"}},
			wantStage: domain.StageSenior,
		},
		{
			name:       "duplicate keeps stage zero",
			job:        domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, DupOf: 7},
			wantStage:  domain.StageDuplicate,
			wantReason: "duplicate",
		},
		{
			name:      "B2B from employment type",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, EmploymentType: "Contract"},
			wantStage: domain.StageSenior, wantB2B: true,
		},
		{
			name:      "B2B from description",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "Open to B2B or independent contractors."},
			wantStage: domain.StageSenior, wantB2B: true,
		},
		{
			name:      "description restricts to Latin America (no structured location)",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Remote role based in Latin America. Must overlap 6 hours."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "description requires US residence",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "You must be located in the US to apply."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "description requires US work authorisation",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Remote", Description: "Candidates must be authorized to work in the United States."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "description says US only",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Open to US only candidates."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "description says remote within Germany",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Mostly remote (within Germany), with monthly meetups."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "a list of countries followed by ONLY (real posting)",
			job:       domain.Job{Title: "Senior Software Engineer", Remote: true, Description: "REMOTE: United States, Canada, or South America ONLY Experience: 7+ years. Go."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "REMOTE with a colon and a country",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Location: REMOTE: Canada. Go."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "REMOTE colon that also names Europe is kept",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "REMOTE: US or Europe. Go and Kubernetes."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "South America only",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Open to South America only. Go."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "REMOTE colon with a non-country is not a restriction",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "REMOTE: flexible hours, async culture. Go."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "anywhere in the US is a restriction, not worldwide",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "This is a remote position that can be located anywhere in the US or Canada."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "phrase continuing with an allowed region is kept",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "You must be located in the US or Europe. Go."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "phrase continuing with another disallowed region is dropped",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "You must be located in Canada or the US. Go."},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "description offering Europe or the US is allowed",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Remote in Europe or the US. Go and TypeScript."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "company being UK-based does not restrict candidates",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "We are a UK-based company hiring worldwide. Go."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "ordinary use of 'us' is not a restriction",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Description: "Join us. Work with us in a remote team. Go."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "explicit allowed location wins over description phrases",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Location: "Anywhere in the World", Description: "Our US only legacy team is being retired. Go."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "structured regions ignore the description",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "Go. Some roles are US only."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "equal-opportunity boilerplate about contractors is not B2B",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "We value all employees and contractors equally."},
			wantStage: domain.StageSenior,
		},
		{
			name:      "ATS city location needs an explicit allow",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"San Francisco"}, Description: "Go"},
			wantStage: domain.StageUnique, wantReason: "region",
		},
		{
			name:      "contractual boilerplate is not B2B",
			job:       domain.Job{Title: "Senior Go Engineer", Remote: true, Regions: []string{"Worldwide"}, Description: "Subject to the terms of your contract of employment."},
			wantStage: domain.StageSenior,
		},
	}
	f := testFilter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := tt.job
			f.Classify(&j)
			if j.Stage != tt.wantStage {
				t.Errorf("stage = %v, want %v (reason %q)", j.Stage, tt.wantStage, j.RejectReason)
			}
			if j.RejectReason != tt.wantReason {
				t.Errorf("reason = %q, want %q", j.RejectReason, tt.wantReason)
			}
			if j.B2B != tt.wantB2B {
				t.Errorf("b2b = %v, want %v", j.B2B, tt.wantB2B)
			}
		})
	}
}

// The phrases below are taken from real postings in the first week of data.
func TestIsB2B(t *testing.T) {
	tests := []struct {
		name  string
		title string
		desc  string
		etype string
		want  bool
	}{
		// Contract-style work that must be found.
		{"contract-based roles", "Senior React Developer", "Two ways to work with clients Contract: most roles are contract-based.", "", true},
		{"full-time contractor", "Software Engineer", "Type: Full-time contractor. Start in December.", "", true},
		{"remote contractor line", "AWS Architect", "Final Details Remote Contractor USD Full Time", "", true},
		{"permanent or contract", "Senior Engineer", "Remote or Berlin · Full time · Permanent or contract · Start as soon as possible", "", true},
		{"contractors welcome", "Senior Engineer", "Full-time contractors are also welcome.", "", true},
		{"hacker news contract header", "Senior Full-Stack Engineer | REMOTE (worldwide) | Contract, 4–5 months | $3,000–4,000/month", "", "", true},
		{"six month contract", "Full-Stack Developer", "6-month contract offer for the right candidate", "", true},
		{"spanish contractor line", "DESARROLLADOR FULL STACK", "Contratación: Contractor. Pago: En USD.", "", true},
		{"freelancer thread", "Go developer", "", "freelance", true},
		{"freelance word", "Developer", "This is a contractor/freelancer opportunity.", "", true},
		{"b2b contract", "Engineer", "We offer a B2B contract with monthly invoices.", "", true},
		{"b2b basis", "Engineer", "Work on a B2B basis from Poland.", "", true},
		{"on a b2b", "Engineer", "Hired on a B2B.", "", true},
		{"short-term contract", "Engineer", "A short-term contract to start.", "", true},
		{"temporary role", "Engineer", "This is a temporary role covering leave.", "", true},
		{"project based", "Engineer", "Mostly project-based work.", "", true},
		{"hourly figure", "Engineer", "Paid $60/hr, invoiced monthly.", "", true},
		{"day rate", "Engineer", "Day rate negotiable.", "", true},
		{"independent contractor", "Engineer", "Open to independent contractors.", "", true},
		{"contract in title", "Senior Go Engineer (Contract)", "", "", true},
		{"employment type contract", "Engineer", "", "Contract", true},
		{"employment type temporary", "Engineer", "", "Temporary", true},
		{"employment type fixed term", "Engineer", "", "Fixed term", true},

		// Wording that only looks like it, which must not count.
		{"B2B SaaS company", "Senior Full Stack", "Complex B2B SaaS products. Experience working with distributed teams.", "", false},
		{"B2B technology company", "Principal Architect", "Jumio is a B2B technology company dedicated to eradicating online fraud.", "", false},
		{"B2B products", "Developer", "Build the exciting brands in gaming, consumer tech, and B2B SaaS.", "", false},
		{"B2B stack", "Developer", "External APIs used in our B2B stack.", "", false},
		{"B2B or B2C", "Developer", "We serve B2B and B2C customers.", "", false},
		{"smart contract development", "Developer Experience Engineer", "Deep understanding of EVM and smart contract development.", "", false},
		{"smart contract in title", "Smart Contract Engineer", "Solidity.", "", false},
		{"API contract design", "Principal Engineer", "Experience designing REST APIs, including contract design, versioning, error handling.", "", false},
		{"contract deliverables", "Solutions Engineer", "Technical requirements translated into contract deliverables.", "", false},
		{"independent non-profit", "Principal Engineer", "AIWI is an independent non-profit.", "", false},
		{"employees and contractors", "Engineer", "We value all employees and contractors equally.", "", false},
		{"employees, vendors and contractors", "Engineer", "Applies to employees, vendors, and contractors.", "", false},
		{"contractors and employees", "Engineer", "Policy covers contractors and employees.", "", false},
		{"contract of employment", "Engineer", "Subject to the terms of your contract of employment.", "", false},
		{"Contract colon, full-time employee", "Engineer", "Contract: Full-time. Hiring Process: 45-min chat.", "", false},
		{"short-term goals", "Engineer", "Balance short-term goals with long-term vision.", "", false},
		{"invoicing product", "Engineer", "We build invoicing software for small businesses.", "", false},
		{"management consultancy client", "AI Engineer", "Our client is a leading global management consultancy.", "", false},
		{"plain full-time", "Engineer", "Full-time employment with equity.", "Full-Time", false},
	}
	f := testFilter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &domain.Job{Title: tt.title, Description: tt.desc, EmploymentType: tt.etype}
			if got := f.isB2B(j); got != tt.want {
				t.Errorf("isB2B = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegionVerified(t *testing.T) {
	tests := []struct {
		name string
		job  domain.Job
		want bool
	}{
		{"structured allowed region", domain.Job{Regions: []string{"Worldwide"}}, true},
		{"explicit location", domain.Job{Location: "Anywhere in the World"}, true},
		{"bare Remote is only not-ruled-out", domain.Job{Location: "Remote"}, false},
		{"no location at all", domain.Job{}, false},
		{"rejected job is never verified", domain.Job{Regions: []string{"USA"}}, false},
	}
	f := testFilter(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := tt.job
			j.Title, j.Remote, j.Description = "Senior Go Engineer", true, "Go"
			f.Classify(&j)
			if j.RegionVerified != tt.want {
				t.Errorf("RegionVerified = %v, want %v (stage %v)", j.RegionVerified, tt.want, j.Stage)
			}
		})
	}
}

func TestNewFilterRejectsBadRegex(t *testing.T) {
	c := config.Default().Filter
	c.Stack = "("
	if _, err := NewFilter(c); err == nil {
		t.Fatal("expected an error for an invalid regex")
	}
}
