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

func TestNewFilterRejectsBadRegex(t *testing.T) {
	c := config.Default().Filter
	c.Stack = "("
	if _, err := NewFilter(c); err == nil {
		t.Fatal("expected an error for an invalid regex")
	}
}
