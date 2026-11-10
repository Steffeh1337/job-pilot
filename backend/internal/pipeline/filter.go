package pipeline

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// Filter classifies jobs into funnel stages. It is pure: no I/O, no state.
type Filter struct {
	regionAllow, regionDeny    *regexp.Regexp
	titleInclude, titleExclude *regexp.Regexp
	stack, seniorityExclude    *regexp.Regexp
	b2b                        *regexp.Regexp
}

func NewFilter(c config.Filter) (*Filter, error) {
	f := &Filter{}
	for _, p := range []struct {
		dst  **regexp.Regexp
		name string
		expr string
	}{
		{&f.regionAllow, "region_allow", c.RegionAllow},
		{&f.regionDeny, "region_deny", c.RegionDeny},
		{&f.titleInclude, "title_include", c.TitleInclude},
		{&f.titleExclude, "title_exclude", c.TitleExclude},
		{&f.stack, "stack", c.Stack},
		{&f.seniorityExclude, "seniority_exclude", c.SeniorityExclude},
		{&f.b2b, "b2b", c.B2B},
	} {
		re, err := regexp.Compile(p.expr)
		if err != nil {
			return nil, fmt.Errorf("filter.%s: %w", p.name, err)
		}
		*p.dst = re
	}
	return f, nil
}

// Classify sets Stage, B2B and RejectReason. A job already marked as a
// duplicate keeps StageDuplicate; callers set that before calling.
func (f *Filter) Classify(j *domain.Job) {
	j.B2B = f.isB2B(j)
	j.RejectReason = ""
	if j.DupOf != 0 {
		j.Stage = domain.StageDuplicate
		j.RejectReason = "duplicate"
		return
	}
	j.Stage = domain.StageUnique

	switch {
	case !j.Remote:
		j.RejectReason = "not remote"
	case !f.regionOK(j):
		j.RejectReason = "region"
	default:
		j.Stage = domain.StageRegion
	}
	if j.Stage != domain.StageRegion {
		return
	}

	if !f.titleInclude.MatchString(j.Title) || f.titleExclude.MatchString(j.Title) {
		j.RejectReason = "role"
		return
	}
	j.Stage = domain.StageRole

	if !f.stack.MatchString(j.Title + "\n" + strings.Join(j.Tags, " ") + "\n" + j.Description) {
		j.RejectReason = "stack"
		return
	}
	j.Stage = domain.StageStack

	if f.seniorityExclude.MatchString(j.Title) {
		j.RejectReason = "seniority"
		return
	}
	j.Stage = domain.StageSenior
}

// regionOK decides whether the candidate could take the job.
//
// With structured restrictions (a list from the source) the job must name an
// allowed region explicitly. With only free text such as "Remote", the job
// passes unless the text names somewhere else, because a bare "Remote" says
// nothing either way and the measurement should show how common that is.
func (f *Filter) regionOK(j *domain.Job) bool {
	if len(j.Regions) > 0 {
		return f.regionAllow.MatchString(strings.Join(j.Regions, " | "))
	}
	if j.Location == "" || f.regionAllow.MatchString(j.Location) {
		return true
	}
	return !f.regionDeny.MatchString(j.Location)
}

func (f *Filter) isB2B(j *domain.Job) bool {
	t := strings.ToLower(j.EmploymentType)
	if strings.Contains(t, "contract") || strings.Contains(t, "freelance") {
		return true
	}
	return f.b2b.MatchString(j.Title + "\n" + j.Description)
}
