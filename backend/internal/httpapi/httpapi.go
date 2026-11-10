// Package httpapi serves the read-only JSON API the dashboard will use.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

type API struct {
	Store *store.Store
	Log   *slog.Logger
	Now   func() time.Time
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/funnel", a.funnel)
	mux.HandleFunc("GET /api/matches", a.matches)
	mux.HandleFunc("GET /api/polls", a.polls)
	return mux
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *API) funnel(w http.ResponseWriter, r *http.Request) {
	days := intParam(r, "days", 7, 1, 365)
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
	writeJSON(w, http.StatusOK, map[string]any{"since": since, "rows": rows, "rejected": rejects})
}

type matchView struct {
	ID        int64     `json:"id"`
	Source    string    `json:"source"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Company   string    `json:"company"`
	Location  string    `json:"location"`
	Regions   []string  `json:"regions,omitempty"`
	B2B       bool      `json:"b2b"`
	SalaryMin int       `json:"salary_min,omitempty"`
	SalaryMax int       `json:"salary_max,omitempty"`
	Currency  string    `json:"currency,omitempty"`
	PostedAt  time.Time `json:"posted_at"`
	FirstSeen time.Time `json:"first_seen_at"`
}

func (a *API) matches(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.Matches(r.Context(), intParam(r, "limit", 50, 1, 500))
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]matchView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, matchView{
			ID: j.ID, Source: j.Source, URL: j.URL, Title: j.Title, Company: j.Company,
			Location: j.Location, Regions: j.Regions, B2B: j.B2B,
			SalaryMin: j.SalaryMin, SalaryMax: j.SalaryMax, Currency: j.Currency,
			PostedAt: j.PostedAt, FirstSeen: j.FirstSeenAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) polls(w http.ResponseWriter, r *http.Request) {
	polls, err := a.Store.RecentPolls(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	if polls == nil {
		polls = []store.Poll{}
	}
	writeJSON(w, http.StatusOK, polls)
}

func (a *API) fail(w http.ResponseWriter, err error) {
	a.Log.Error("request failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func intParam(r *http.Request, name string, def, min, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n < min {
		return def
	}
	return min2(n, max)
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
