package webapi

import (
	"net/http"

	"tgareview/internal/bundle"
	"tgareview/internal/cases"
)

func (a *API) caseBaseline(w http.ResponseWriter, r *http.Request) {
	b, err := cases.ReadFile("tga_case_v1/baseline.json")
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func (a *API) caseReplay(w http.ResponseWriter, r *http.Request) {
	b, err := cases.ReadFile("tga_case_v1/replay_plan.json")
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func (a *API) caseMutation(w http.ResponseWriter, r *http.Request) {
	mid := r.PathValue("mid")
	b, err := cases.ReadFile("mutations/" + mid + ".json")
	if err != nil {
		writeErr(w, 404, "unknown mutation: "+mid)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// caseLoad performs a one-click deterministic demo load: it creates the
// project and imports the six-layer offline baseline using only embedded files.
func (a *API) caseLoad(w http.ResponseWriter, r *http.Request) {
	raw, err := cases.ReadFile("tga_case_v1/baseline.json")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	b, err := bundle.Parse(raw)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	pid, err := a.svc.CreateProject(r.Context(), b.ProjectID, b.Name, b.Description)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	res, err := a.svc.Import(r.Context(), pid, b)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"project_id": pid, "import": res})
}
