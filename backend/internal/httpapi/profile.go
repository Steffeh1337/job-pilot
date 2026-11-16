package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Steffeh1337/job-pilot/backend/internal/cv"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/matcher"
)

// Matcher is CV matching as the API uses it. *matcher.Matcher implements it.
type Matcher interface {
	Status(ctx context.Context) matcher.Status
	Profile(ctx context.Context) (*matcher.ProfileView, error)
	Upload(ctx context.Context, filename string, data []byte) (*matcher.ProfileView, []string, error)
	Reextract(ctx context.Context) (*matcher.ProfileView, []string, error)
	SuggestHighlights(ctx context.Context) ([]string, []string, error)
	UpdateProfile(ctx context.Context, p matcher.Profile) (*matcher.ProfileView, error)
	DeleteProfile(ctx context.Context) error
	RequestSync()
	Fits(ctx context.Context, ids []int64) (map[int64]fit.Fit, error)
	Rank(ctx context.Context, ids []int64, minFit int) ([]int64, map[int64]fit.Fit, error)
}

func (a *API) registerMatching(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profile", a.withMatcher(a.getProfile))
	mux.HandleFunc("PUT /api/profile", a.withMatcher(a.putProfile))
	mux.HandleFunc("DELETE /api/profile", a.withMatcher(a.deleteProfile))
	mux.HandleFunc("POST /api/profile/cv", a.withMatcher(a.uploadCV))
	mux.HandleFunc("POST /api/profile/reextract", a.withMatcher(a.reextract))
	mux.HandleFunc("POST /api/profile/highlights", a.withMatcher(a.suggestHighlights))
	mux.HandleFunc("GET /api/fit/status", a.withMatcher(a.fitStatus))
	mux.HandleFunc("POST /api/fit/sync", a.withMatcher(a.fitSync))
}

// withMatcher answers 503 when CV matching is not available.
func (a *API) withMatcher(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Matcher == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "CV matching is not available"})
			return
		}
		h(w, r)
	}
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	v, err := a.Matcher.Profile(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// profileResponse is a profile plus anything the user should be told about how
// it was read.
type profileResponse struct {
	Profile  *matcher.ProfileView `json:"profile"`
	Warnings []string             `json:"warnings"`
}

// maxFormOverhead is room in an upload for the multipart framing around the file.
const maxFormOverhead = 1 << 20

// uploadCV reads a CV from a multipart form field named "cv". The whole upload
// is held in memory, never written to a temporary file, and nothing about it
// is logged.
func (a *API) uploadCV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, cv.MaxBytes+maxFormOverhead)
	if err := r.ParseMultipartForm(cv.MaxBytes + maxFormOverhead); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": cv.ErrTooLarge.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send the CV as a multipart form with a file field named cv"})
		return
	}
	file, header, err := r.FormFile("cv")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file was attached: the form field must be named cv"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, cv.MaxBytes+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the uploaded file"})
		return
	}

	view, warnings, err := a.Matcher.Upload(r.Context(), header.Filename, data)
	if err != nil {
		a.profileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{Profile: view, Warnings: nonNilStrings(warnings)})
}

func (a *API) reextract(w http.ResponseWriter, r *http.Request) {
	view, warnings, err := a.Matcher.Reextract(r.Context())
	if err != nil {
		a.profileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{Profile: view, Warnings: nonNilStrings(warnings)})
}

// highlightsResponse is bullets drafted for review. Nothing is saved until the
// person saves the profile.
type highlightsResponse struct {
	Highlights []string `json:"highlights"`
	Warnings   []string `json:"warnings"`
}

func (a *API) suggestHighlights(w http.ResponseWriter, r *http.Request) {
	bullets, warnings, err := a.Matcher.SuggestHighlights(r.Context())
	if err != nil {
		a.profileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, highlightsResponse{Highlights: nonNilStrings(bullets), Warnings: nonNilStrings(warnings)})
}

func (a *API) putProfile(w http.ResponseWriter, r *http.Request) {
	var p matcher.Profile
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body must be a JSON profile"})
		return
	}
	view, err := a.Matcher.UpdateProfile(r.Context(), p)
	if err != nil {
		a.profileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := a.Matcher.DeleteProfile(r.Context()); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) fitStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Matcher.Status(r.Context()))
}

// fitSync starts indexing in the background and returns the current status.
func (a *API) fitSync(w http.ResponseWriter, r *http.Request) {
	a.Matcher.RequestSync()
	status := a.Matcher.Status(r.Context())
	status.Index.Syncing = true
	writeJSON(w, http.StatusAccepted, status)
}

// profileError maps the matcher's errors onto HTTP statuses.
func (a *API) profileError(w http.ResponseWriter, err error) {
	var (
		invalid     *matcher.InvalidCVError
		bad         *matcher.ValidationError
		unavailable *matcher.UnavailableError
	)
	switch {
	case errors.As(err, &unavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case errors.Is(err, cv.ErrTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.As(err, &bad):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, matcher.ErrNoProfile):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		a.fail(w, err)
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
