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
	descriptionDeny            *regexp.Regexp
	titleInclude, titleExclude *regexp.Regexp
	seniorityExclude           *regexp.Regexp
	b2b, b2bIgnore, b2bTitle   *regexp.Regexp
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
		{&f.descriptionDeny, "description_deny", c.DescriptionDeny},
		{&f.titleInclude, "title_include", c.TitleInclude},
		{&f.titleExclude, "title_exclude", c.TitleExclude},
		{&f.seniorityExclude, "seniority_exclude", c.SeniorityExclude},
		{&f.b2b, "b2b", c.B2B},
		{&f.b2bIgnore, "b2b_ignore", c.B2BIgnore},
		{&f.b2bTitle, "b2b_title", c.B2BTitle},
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

	j.RegionVerified = false
	if !j.Remote {
		j.RejectReason = "not remote"
		return
	}
	ok, verified := f.regionOK(j)
	if !ok {
		j.RejectReason = "region"
		return
	}
	j.Stage, j.RegionVerified = domain.StageRegion, verified

	if !f.titleInclude.MatchString(j.Title) || f.titleExclude.MatchString(j.Title) {
		j.RejectReason = "role"
		return
	}
	j.Stage = domain.StageRole

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
//
// verified is true only when an allowed region was positively found; a job
// that merely wasn't ruled out is ok but unverified, and the dashboard
// flags it so it gets a human look.
func (f *Filter) regionOK(j *domain.Job) (ok, verified bool) {
	if len(j.Regions) > 0 {
		ok = f.regionAllow.MatchString(strings.Join(j.Regions, " | "))
		return ok, ok
	}
	if f.regionAllow.MatchString(j.Location) {
		return true, true
	}
	if f.regionDeny.MatchString(j.Location) {
		return false, false
	}
	// No location signal (or only a bare "Remote"): look for restriction
	// phrases in the description.
	loc := f.descriptionDeny.FindStringIndex(j.Description)
	if loc == nil {
		return true, false
	}
	// The phrase names a disallowed place, but may continue with an allowed
	// one: "located in the US or Europe". Only the text after the phrase is
	// checked, because the phrase itself can contain "anywhere" ("anywhere in
	// the US"), which is restrictive here.
	tail := j.Description[loc[1]:]
	if len(tail) > 40 {
		tail = tail[:40]
	}
	return f.regionAllow.MatchString(tail), false
}

// employmentTypeB2B are fragments of a source's own employment-type field
// that mean contract-style work ("Contract", "Freelance", "Temporary",
// "Fixed term").
var employmentTypeB2B = []string{"contract", "freelance", "temporar", "interim", "fixed"}

// isB2B reports whether the job is contract, freelance, independent or
// short-term work. The source's own employment type is trusted first; then the
// title and description are searched after boilerplate such as "B2B SaaS" and
// "smart contract" has been blanked out.
func (f *Filter) isB2B(j *domain.Job) bool {
	t := strings.ToLower(j.EmploymentType)
	for _, frag := range employmentTypeB2B {
		if strings.Contains(t, frag) {
			return true
		}
	}
	if f.b2bTitle.MatchString(f.b2bIgnore.ReplaceAllString(j.Title, " ")) {
		return true
	}
	return f.b2b.MatchString(f.b2bIgnore.ReplaceAllString(j.Description, " "))
}
