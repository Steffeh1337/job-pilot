package domain

import (
	"regexp"
	"strings"
	"time"
)

// Stage is the furthest funnel step a job has passed. A job that is rejected
// stays at the last stage it cleared, so counting jobs with Stage >= n gives
// the funnel numbers directly.
type Stage int

const (
	StageDuplicate Stage = iota // same company+title already seen from another source
	StageUnique                 // first time this role was seen
	StageRegion                 // remote and open to someone in the configured region
	StageRole                   // title looks like an engineering role we want
	StageStack                  // mentions the target stack
	StageSenior                 // title is not junior/mid level; this is a "match"
)

var stageNames = [...]string{"duplicate", "unique", "region", "role", "stack", "senior"}

func (s Stage) String() string {
	if s < 0 || int(s) >= len(stageNames) {
		return "unknown"
	}
	return stageNames[s]
}

// Job is a normalised listing from any source.
type Job struct {
	ID             int64
	Source         string
	ExternalID     string
	URL            string
	Title          string
	Company        string
	Location       string   // free text, e.g. "Remote, United States"
	Regions        []string // structured restrictions when the source provides them
	Remote         bool
	EmploymentType string
	Tags           []string
	Description    string // plain text
	SalaryMin      int
	SalaryMax      int
	Currency       string
	PostedAt       time.Time
	FirstSeenAt    time.Time
	LastSeenAt     time.Time

	// Set by the pipeline.
	Fingerprint  string
	DupOf        int64
	Stage        Stage
	B2B          bool
	RejectReason string
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normalise(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), " "), " ")
}

// Fingerprint identifies the same role posted on several boards.
func Fingerprint(company, title string) string {
	return normalise(company) + "|" + normalise(title)
}
