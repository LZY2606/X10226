// Package webapi exposes the review workflow over HTTP using only the standard
// library. Every JSON view value carries a stable id so UI elements can link
// back to source inputs, computation steps and human decisions.
package webapi

import (
	"encoding/json"
	"net/http"

	"tgareview/internal/cases"
	"tgareview/internal/clock"
	"tgareview/internal/project"
	"tgareview/internal/store"
)

type API struct {
	svc *project.Service
}

func New(st *store.Store, clk clock.Clock) *API {
	return &API{svc: project.New(st, clk)}
}

func (a *API) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", a.listProjects)
	mux.HandleFunc("POST /api/projects", a.createProject)
	mux.HandleFunc("GET /api/projects/{id}", a.getProject)
	mux.HandleFunc("DELETE /api/projects/{id}", a.deleteProject)
	mux.HandleFunc("POST /api/projects/{id}/import", a.importBundle)
	mux.HandleFunc("POST /api/projects/{id}/params", a.reviseParams)
	mux.HandleFunc("POST /api/projects/{id}/decide", a.decide)
	mux.HandleFunc("POST /api/projects/{id}/undo", a.undo)
	mux.HandleFunc("GET /api/projects/{id}/view", a.view)
	mux.HandleFunc("GET /api/projects/{id}/view/{seq}", a.viewAt)
	mux.HandleFunc("GET /api/projects/{id}/events", a.events)
	mux.HandleFunc("GET /api/projects/{id}/export", a.exportProject)
	mux.HandleFunc("POST /api/import-export", a.importExport)
	mux.HandleFunc("GET /api/case/baseline", a.caseBaseline)
	mux.HandleFunc("GET /api/case/replay-plan", a.caseReplay)
	mux.HandleFunc("GET /api/case/mutation/{mid}", a.caseMutation)
	mux.HandleFunc("POST /api/case/load", a.caseLoad)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := a.svc.ListProjects(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"projects": ps})
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	id, err := a.svc.CreateProject(r.Context(), req.ID, req.Name, req.Description)
	if err != nil {
		code := 409
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"project_id": id})
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": r.PathValue("id")})
}

var _ = cases.Root
