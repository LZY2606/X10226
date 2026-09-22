package webapi

import (
	"encoding/json"
	"io"
	"net/http"

	"tgareview/internal/bundle"
	"tgareview/internal/engine"
	"tgareview/internal/project"
)

func (a *API) importBundle(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	b, err := bundle.Parse(body)
	if err != nil {
		writeJSON(w, 400, err.Error())
		return
	}
	res, err := a.svc.Import(r.Context(), pid, b)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	code := 201
	if res.Status == "partial" {
		code = 202
	}
	writeJSON(w, code, res)
}

func (a *API) reviseParams(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	var p engine.Params
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, 400, "invalid params: "+err.Error())
		return
	}
	rev, art, err := a.svc.ReviseParams(r.Context(), pid, p, "manual parameter revision")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"params_rev_id": rev, "artifact_id": art})
}

func (a *API) decide(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	var req struct {
		ArtifactID string `json:"artifact_id"`
		TargetID   string `json:"target_id"`
		Kind       string `json:"kind"`
		Value      string `json:"value"`
		Rationale  string `json:"rationale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Kind != "accept" && req.Kind != "reject" && req.Kind != "parameter" {
		writeErr(w, 400, "kind must be accept|reject|parameter")
		return
	}
	d, err := a.svc.Decide(r.Context(), pid, req.ArtifactID, req.TargetID, req.Kind, req.Value, req.Rationale)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, d)
}

func (a *API) undo(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	var req struct {
		DecisionID string `json:"decision_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	seq, err := a.svc.UndoDecision(r.Context(), pid, req.DecisionID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"undone": req.DecisionID, "seq": seq})
}

func (a *API) exportProject(w http.ResponseWriter, r *http.Request) {
	b, err := a.svc.Export(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tga-export.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(b)
}

func (a *API) importExport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	b, err := project.ParseExport(body)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.svc.Restore(r.Context(), b); err != nil {
		if err == project.ErrExists {
			writeErr(w, 409, err.Error())
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"restored": b.Project.ID, "events": len(b.Events), "revisions": len(b.Revisions), "artifacts": len(b.Artifacts)})
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	st, err := a.svc.CurrentState(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// Return event list from DB for the timeline (seq/type/at/payload kept).
	evs, err := a.svc.EventList(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"max_seq": st.Seq, "events": evs})
}
