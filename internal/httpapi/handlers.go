package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"tga-archive/internal/cases"
	"tga-archive/internal/domain"
	"tga-archive/internal/store"
)

func (s *Server) importBundle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	res, err := s.App.SubmitBundle(id, req.Text)
	if err != nil {
		writeErr(w, 400, "import_failed", err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) importLayer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Layer          string `json:"layer"`
		Label          string `json:"label"`
		Text           string `json:"text"`
		ParentSourceID string `json:"parentSourceId"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	p, ev, err := s.App.SubmitLayer(id, req.Layer, req.Label, req.Text, req.ParentSourceID)
	if err != nil {
		writeErr(w, 400, "import_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"payload": p, "event": ev})
}

func (s *Server) reviseParams(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Params domain.Params `json:"params"`
		Reason string        `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	ev, err := s.App.ReviseParams(id, req.Params, req.Reason)
	if err != nil {
		writeErr(w, 400, "revise_failed", err.Error())
		return
	}
	writeJSON(w, 200, ev)
}

func (s *Server) rule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		CandidateKey string             `json:"candidateKey"`
		Decision     string             `json:"decision"`
		Rationale    string             `json:"rationale"`
		Adjusted     map[string]float64 `json:"adjusted"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	ev, err := s.App.Rule(id, req.CandidateKey, req.Decision, req.Rationale, req.Adjusted)
	if err != nil {
		writeErr(w, 400, "rule_failed", err.Error())
		return
	}
	writeJSON(w, 200, ev)
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		CandidateKey string `json:"candidateKey"`
		KeptRuling   string `json:"keptRulingId"`
		Note         string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	ev, err := s.App.ResolveConflict(id, req.CandidateKey, req.KeptRuling, req.Note)
	if err != nil {
		writeErr(w, 400, "resolve_failed", err.Error())
		return
	}
	writeJSON(w, 200, ev)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		RulingID string `json:"rulingId"`
		Reason   string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	ev, err := s.App.UndoRuling(id, req.RulingID, req.Reason)
	if err != nil {
		writeErr(w, 400, "undo_failed", err.Error())
		return
	}
	writeJSON(w, 200, ev)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	evs, err := s.Store.Events(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	writeJSON(w, 200, evs)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	st, err := s.App.CurrentState(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) stateAt(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		writeErr(w, 400, "bad_seq", "seq must be an integer")
		return
	}
	st, err := s.App.StateAt(r.PathValue("id"), seq)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.Export(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	b, err := a.Marshal()
	if err != nil {
		writeErr(w, 500, "marshal_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="tga-archive.json"`)
	_, _ = w.Write(b)
}

func (s *Server) importArchive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeErr(w, 400, "read_failed", err.Error())
		return
	}
	a, err := store.ParseArchive(body)
	if err != nil {
		writeErr(w, 400, "bad_archive", err.Error())
		return
	}
	v, err := s.Store.Import(a)
	if err != nil {
		writeErr(w, 400, "import_archive_failed", err.Error())
		return
	}
	_ = s.App.Refresh(r.Context(), v.ProjectID)
	writeJSON(w, 200, v)
}

func (s *Server) listCases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, cases.List())
}

func (s *Server) getCase(w http.ResponseWriter, r *http.Request) {
	c, err := cases.Load(r.PathValue("cid"))
	if err != nil {
		writeErr(w, 404, "case_not_found", err.Error())
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) instantiateCase(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("cid")
	c, err := cases.Load(cid)
	if err != nil {
		writeErr(w, 404, "case_not_found", err.Error())
		return
	}
	// fixed virtual origin for reproducible replay ids
	info, err := s.App.CreateProject(c.Manifest.Title, cid, c.Manifest.Seed, "instantiated from offline case pack")
	if err != nil {
		writeErr(w, 500, "instantiate_failed", err.Error())
		return
	}
	res, err := s.App.SubmitBundle(info.ID, c.V1Input)
	if err != nil {
		writeJSON(w, 200, map[string]any{"project": info, "initialImport": res, "warning": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"project": info, "initialImport": res})
}

func (s *Server) caseFile(w http.ResponseWriter, r *http.Request) {
	c, err := cases.Load(r.PathValue("cid"))
	if err != nil {
		writeErr(w, 404, "case_not_found", err.Error())
		return
	}
	name := r.PathValue("name")
	for _, f := range c.Manifest.Files {
		if f == name {
			content := c.Files[name]
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(content))
			return
		}
	}
	writeErr(w, 404, "file_not_found", name)
}
