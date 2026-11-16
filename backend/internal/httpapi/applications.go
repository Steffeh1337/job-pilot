package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

const maxNotesRunes = 2000

// applicationView is what the person told us about applying to a job.
type applicationView struct {
	Status    string    `json:"status"`
	AppliedOn string    `json:"applied_on"` // YYYY-MM-DD
	Notes     string    `json:"notes"`
	UpdatedAt time.Time `json:"updated_at"`
}

func viewApplication(a store.Application) *applicationView {
	return &applicationView{Status: a.Status, AppliedOn: a.AppliedOn, Notes: a.Notes, UpdatedAt: a.UpdatedAt}
}

// withApplications attaches application records to job views.
func (a *API) withApplications(r *http.Request, vs []jobView) ([]jobView, error) {
	if len(vs) == 0 {
		return vs, nil
	}
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = v.ID
	}
	apps, err := a.Store.ApplicationsFor(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	for i := range vs {
		if app, ok := apps[vs[i].ID]; ok {
			vs[i].Application = viewApplication(app)
		}
	}
	return vs, nil
}

func jobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad id"})
		return 0, false
	}
	return id, true
}

// putApplication records that the person applied to a job, or updates what they
// noted about it. The body is {"status", "applied_on", "notes"}; a missing
// status means "applied" and a missing date means today.
func (a *API) putApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	var body struct {
		Status    string `json:"status"`
		AppliedOn string `json:"applied_on"`
		Notes     string `json:"notes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body must be JSON"})
		return
	}
	bad := func(msg string) { writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg}) }

	status := strings.ToLower(strings.TrimSpace(body.Status))
	if status == "" {
		status = "applied"
	}
	if !slices.Contains(store.ApplicationStatuses, status) {
		bad("status must be one of " + strings.Join(store.ApplicationStatuses, ", "))
		return
	}
	today := a.now().Format("2006-01-02")
	day := strings.TrimSpace(body.AppliedOn)
	if day == "" {
		day = today
	}
	when, err := time.Parse("2006-01-02", day)
	if err != nil {
		bad("applied_on must be a date like 2026-11-20")
		return
	}
	if limit := a.now().AddDate(0, 0, 1).Format("2006-01-02"); when.Format("2006-01-02") > limit {
		bad("applied_on can't be in the future")
		return
	}
	notes := strings.TrimSpace(body.Notes)
	if utf8.RuneCountInString(notes) > maxNotesRunes {
		bad("notes are longer than 2000 characters")
		return
	}

	job, err := a.Store.Job(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	rec := store.Application{JobID: id, Status: status, AppliedOn: when.Format("2006-01-02"), Notes: notes, UpdatedAt: a.now()}
	if err := a.Store.SaveApplication(r.Context(), rec); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewApplication(rec))
}

// deleteApplication forgets an application. It is fine if there isn't one.
func (a *API) deleteApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeleteApplication(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
