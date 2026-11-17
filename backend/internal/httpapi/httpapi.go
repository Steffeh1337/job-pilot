// Package httpapi serves the read-only JSON API the dashboard uses, and the
// built dashboard itself when a web directory is configured.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/scheduler"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// Refresher runs an on-demand poll of the sources. *scheduler.Scheduler
// implements it.
type Refresher interface {
	StartRefresh() scheduler.RefreshStatus
	RefreshStatus() scheduler.RefreshStatus
}

type API struct {
	Store *store.Store
	Log   *slog.Logger
	Now   func() time.Time
	// Intervals maps each enabled source to its minimum poll interval, so the
	// dashboard can show when a source is next due.
	Intervals map[string]time.Duration
	// WebDir, if it exists, is served as a single-page app at "/".
	WebDir string
	// Refresher backs POST /api/refresh. Nil when no poller is running.
	Refresher Refresher
	// Matcher backs the CV profile and fit scores. Nil disables them.
	Matcher Matcher
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/funnel", a.funnel)
	mux.HandleFunc("GET /api/matches", a.matches)
	mux.HandleFunc("GET /api/jobs", a.jobs)
	mux.HandleFunc("GET /api/jobs/{id}", a.job)
	mux.HandleFunc("PUT /api/jobs/{id}/application", a.putApplication)
	mux.HandleFunc("DELETE /api/jobs/{id}/application", a.deleteApplication)
	mux.HandleFunc("PUT /api/jobs/{id}/viewed", a.putViewed)
	mux.HandleFunc("DELETE /api/jobs/{id}/viewed", a.deleteViewed)
	mux.HandleFunc("GET /api/polls", a.polls)
	mux.HandleFunc("GET /api/refresh", a.refreshStatus)
	mux.HandleFunc("POST /api/refresh", a.refreshStart)
	a.registerMatching(mux)
	if a.WebDir != "" {
		if st, err := os.Stat(filepath.Join(a.WebDir, "index.html")); err == nil && !st.IsDir() {
			mux.Handle("GET /", spa(a.WebDir))
		}
	}
	return mux
}

// spa serves files from dir, falling back to index.html so client-side routes
// survive a page reload.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An unknown API path is an error, not a page.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if r.URL.Path != "/" {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *API) funnel(w http.ResponseWriter, r *http.Request) {
	days := intParam(r, "days", 7, 1, 3650)
	since := a.now().AddDate(0, 0, -days)
	rows, err := a.Store.Funnel(r.Context(), since)
	if err != nil {
		a.fail(w, err)
		return
	}
	rejects, err := a.Store.RejectReasons(r.Context(), since)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "since": since, "rows": rows, "rejected": rejects})
}

type jobView struct {
	ID             int64    `json:"id"`
	Source         string   `json:"source"`
	URL            string   `json:"url"`
	Title          string   `json:"title"`
	Company        string   `json:"company"`
	Location       string   `json:"location"`
	Regions        []string `json:"regions,omitempty"`
	Remote         bool     `json:"remote"`
	EmploymentType string   `json:"employment_type,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	B2B            bool     `json:"b2b"`
	// RegionVerified is false when the source gave no location, so the job
	// was only "not ruled out" for someone in Romania.
	RegionVerified bool      `json:"region_verified"`
	Stage          string    `json:"stage"`
	RejectReason   string    `json:"reject_reason,omitempty"`
	SalaryMin      int       `json:"salary_min,omitempty"`
	SalaryMax      int       `json:"salary_max,omitempty"`
	Currency       string    `json:"currency,omitempty"`
	PostedAt       time.Time `json:"posted_at"`
	FirstSeen      time.Time `json:"first_seen_at"`
	Description    string    `json:"description,omitempty"` // detail endpoint only
	// Fit is how well the job matches the uploaded CV; absent without one, or
	// when the job can't be scored.
	Fit *fit.Fit `json:"fit,omitempty"`
	// Application is set once the person has recorded applying to the job.
	Application *applicationView `json:"application,omitempty"`
	// Viewed is true once the job has been opened, until it is marked unread.
	Viewed bool `json:"viewed"`
}

func view(j *domain.Job, withDescription bool) jobView {
	v := jobView{
		ID: j.ID, Source: j.Source, URL: j.URL, Title: j.Title, Company: j.Company,
		Location: j.Location, Regions: j.Regions, Remote: j.Remote, EmploymentType: j.EmploymentType,
		Tags: j.Tags, B2B: j.B2B, RegionVerified: j.RegionVerified, Stage: j.Stage.String(),
		RejectReason: j.RejectReason, SalaryMin: j.SalaryMin, SalaryMax: j.SalaryMax, Currency: j.Currency,
		PostedAt: j.PostedAt, FirstSeen: j.FirstSeenAt,
	}
	if withDescription {
		v.Description = j.Description
	}
	return v
}

func views(jobs []*domain.Job) []jobView {
	out := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, view(j, false))
	}
	return out
}

// matches is the original, simple list of matching jobs.
func (a *API) matches(w http.ResponseWriter, r *http.Request) {
	jobs, _, err := a.Store.Search(r.Context(), store.Search{Status: store.StatusMatch, Limit: intParam(r, "limit", 50, 1, 500)})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views(jobs))
}

// jobs searches stored jobs. Query parameters: status (match|rejected|all,
// default match), source, reason, b2b=1, unverified=1, applied (1 = only jobs
// applied to, 0 = hide them), q, days, sort (new|fit),
// min_fit, limit, offset. sort=fit and min_fit need an uploaded CV; without
// one they change nothing.
func (a *API) jobs(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := store.Search{
		Status:     store.Status(qs.Get("status")),
		Source:     qs.Get("source"),
		Reason:     qs.Get("reason"),
		B2BOnly:    qs.Get("b2b") == "1",
		Unverified: qs.Get("unverified") == "1",
		Applied:    appliedFilter(qs.Get("applied")),
		Query:      strings.TrimSpace(qs.Get("q")),
		Limit:      intParam(r, "limit", 50, 1, 200),
		Offset:     intParam(r, "offset", 0, 0, 1_000_000),
	}
	switch q.Status {
	case "", store.StatusMatch, store.StatusRejected, store.StatusAll:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be match, rejected or all"})
		return
	}
	if v := qs.Get("applied"); v != "" && q.Applied == store.AppliedAny {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "applied must be 1 (only applied) or 0 (hide applied)"})
		return
	}
	sortBy := qs.Get("sort")
	if sortBy != "" && sortBy != "new" && sortBy != "fit" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sort must be new or fit"})
		return
	}
	minFit := intParam(r, "min_fit", 0, 0, 100)
	if days := intParam(r, "days", 0, 0, 3650); days > 0 {
		q.Since = a.now().AddDate(0, 0, -days)
	}

	ctx := r.Context()
	var (
		jobs  []*domain.Job
		total int
		fits  map[int64]fit.Fit
		err   error
	)
	if a.Matcher != nil && (sortBy == "fit" || minFit > 0) {
		// Fit isn't a database column, so rank every match, then fetch one page.
		var ids, ranked []int64
		if ids, err = a.Store.SearchIDs(ctx, q); err == nil {
			ranked, fits, err = a.Matcher.Rank(ctx, ids, minFit)
		}
		if err == nil && q.Applied != store.AppliedOnly {
			ranked, err = a.unviewedFirst(ctx, ranked)
		}
		if err == nil {
			total = len(ranked)
			lo := min(q.Offset, len(ranked))
			hi := min(lo+q.Limit, len(ranked))
			jobs, err = a.Store.JobsByIDs(ctx, ranked[lo:hi])
		}
	} else {
		jobs, total, err = a.Store.Search(ctx, q)
		if err == nil && a.Matcher != nil {
			ids := make([]int64, len(jobs))
			for i, j := range jobs {
				ids[i] = j.ID
			}
			fits, err = a.Matcher.Fits(ctx, ids)
		}
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	vs, err := a.withApplications(r, withFits(views(jobs), fits))
	if err == nil {
		vs, err = a.withViewed(r, vs)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "jobs": vs})
}

// appliedFilter reads ?applied=1 (only applied) and ?applied=0 (hide applied).
func appliedFilter(v string) store.AppliedFilter {
	switch v {
	case "1":
		return store.AppliedOnly
	case "0":
		return store.AppliedHide
	}
	return store.AppliedAny
}

// withFits attaches fit scores to job views.
func withFits(vs []jobView, fits map[int64]fit.Fit) []jobView {
	for i := range vs {
		if f, ok := fits[vs[i].ID]; ok {
			vs[i].Fit = &f
		}
	}
	return vs
}

func (a *API) job(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad id"})
		return
	}
	j, err := a.Store.Job(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if j == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	v := view(j, true)
	apps, err := a.Store.ApplicationsFor(r.Context(), []int64{id})
	if err != nil {
		a.fail(w, err)
		return
	}
	if app, ok := apps[id]; ok {
		v.Application = viewApplication(app)
	}
	viewed, err := a.Store.ViewedAmong(r.Context(), []int64{id})
	if err != nil {
		a.fail(w, err)
		return
	}
	v.Viewed = viewed[id]
	if a.Matcher != nil {
		fits, err := a.Matcher.Fits(r.Context(), []int64{id})
		if err != nil {
			a.fail(w, err)
			return
		}
		if f, ok := fits[id]; ok {
			v.Fit = &f
		}
	}
	writeJSON(w, http.StatusOK, v)
}

type pollView struct {
	Source          string     `json:"source"`
	StartedAt       *time.Time `json:"started_at"` // null until the first poll
	DurationMS      int64      `json:"duration_ms"`
	Fetched         int        `json:"fetched"`
	NewJobs         int        `json:"new_jobs"`
	Error           string     `json:"error,omitempty"`
	IntervalSeconds int        `json:"interval_seconds"`
}

// polls returns the latest poll of every enabled source, including sources
// that have not run yet.
func (a *API) polls(w http.ResponseWriter, r *http.Request) {
	recent, err := a.Store.RecentPolls(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	bySource := map[string]pollView{}
	for _, p := range recent {
		started := p.StartedAt
		bySource[p.Source] = pollView{
			Source: p.Source, StartedAt: &started, DurationMS: p.Duration.Milliseconds(),
			Fetched: p.Fetched, NewJobs: p.NewJobs, Error: p.Error,
		}
	}
	for name, iv := range a.Intervals {
		v := bySource[name]
		v.Source, v.IntervalSeconds = name, int(iv.Seconds())
		bySource[name] = v
	}
	out := make([]pollView, 0, len(bySource))
	for _, v := range bySource {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	writeJSON(w, http.StatusOK, out)
}

// refreshStart begins polling every source in the background and returns at
// once; clients then watch GET /api/refresh. Starting while one is already
// running just reports the running one, so double clicks are harmless.
func (a *API) refreshStart(w http.ResponseWriter, r *http.Request) {
	if a.Refresher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the poller is not running"})
		return
	}
	writeJSON(w, http.StatusAccepted, a.Refresher.StartRefresh())
}

func (a *API) refreshStatus(w http.ResponseWriter, r *http.Request) {
	if a.Refresher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the poller is not running"})
		return
	}
	writeJSON(w, http.StatusOK, a.Refresher.RefreshStatus())
}

func (a *API) fail(w http.ResponseWriter, err error) {
	a.Log.Error("request failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

// intParam reads an integer query parameter, returning def when it is
// missing or invalid and clamping it to [min, max].
func intParam(r *http.Request, name string, def, min, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
