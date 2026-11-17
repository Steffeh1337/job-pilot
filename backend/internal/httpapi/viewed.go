package httpapi

import (
	"context"
	"net/http"
)

// withViewed marks which job views have been opened.
func (a *API) withViewed(r *http.Request, vs []jobView) ([]jobView, error) {
	if len(vs) == 0 {
		return vs, nil
	}
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = v.ID
	}
	viewed, err := a.Store.ViewedAmong(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	for i := range vs {
		vs[i].Viewed = viewed[vs[i].ID]
	}
	return vs, nil
}

// unviewedFirst moves jobs that have been opened to the end, keeping the order
// within each group. It is how a ranking by fit still puts viewed jobs last.
func (a *API) unviewedFirst(ctx context.Context, ids []int64) ([]int64, error) {
	viewed, err := a.Store.ViewedAmong(ctx, ids)
	if err != nil || len(viewed) == 0 {
		return ids, err
	}
	out := make([]int64, 0, len(ids))
	var seen []int64
	for _, id := range ids {
		if viewed[id] {
			seen = append(seen, id)
		} else {
			out = append(out, id)
		}
	}
	return append(out, seen...), nil
}

// putViewed records that the person opened a job.
func (a *API) putViewed(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
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
	if err := a.Store.MarkViewed(r.Context(), id, a.now()); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteViewed marks a job unread again. It is fine if it wasn't viewed.
func (a *API) deleteViewed(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	if err := a.Store.MarkUnread(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
