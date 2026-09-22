// Package web serves the review workspace: a single synchronized page plus a
// small JSON API. Every value exposed in the view model carries its stable
// uid, so source -> derivation -> decision traces are links rather than
// recomputed labels.
package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"tga-lineage/internal/model"
	"tga-lineage/internal/project"
)

type Server struct {
	svc     *project.Service
	dbPath  string
	handler http.Handler
}

func NewServer(svc *project.Service, dbPath string) *Server {
	s := &Server{svc: svc, dbPath: dbPath}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/app.js", s.appJS)
	mux.HandleFunc("GET /static/style.css", s.css)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{pid}", s.getProject)
	mux.HandleFunc("POST /api/projects/{pid}/import", s.importData)
	mux.HandleFunc("POST /api/projects/{pid}/params", s.reviseParams)
	mux.HandleFunc("POST /api/projects/{pid}/decide", s.decide)
	mux.HandleFunc("POST /api/projects/{pid}/retract", s.retract)
	mux.HandleFunc("POST /api/projects/{pid}/resolve", s.resolve)
	mux.HandleFunc("POST /api/projects/{pid}/note", s.note)
	mux.HandleFunc("POST /api/projects/{pid}/counter/run", s.runCounter)
	mux.HandleFunc("POST /api/projects/{pid}/counter/verdict", s.verdictCounter)
	mux.HandleFunc("GET /api/projects/{pid}/export", s.exportAll)
	mux.HandleFunc("POST /api/import-archive", s.importArchive)
	mux.HandleFunc("GET /api/casepack", s.casepack)
	mux.HandleFunc("POST /api/casepack/demo", s.demoProject)
	mux.HandleFunc("GET /", s.index)
	s.handler = mux
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.svc.ListRows(r.Context())
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, rows)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Description string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Name == "" {
		req.Name = "TGA review"
	}
	st, err := s.svc.CreateProject(r.Context(), req.Name, req.Description)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, st.Info)
}

func (s *Server) loadState(ctx context.Context, pid string, r *http.Request) (*project.State, int64, bool) {
	seq, _ := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64)
	if seq > 0 {
		st, err := s.svc.StateAt(ctx, pid, seq)
		if err != nil {
			return nil, 0, false
		}
		return st, seq, true
	}
	st, err := s.svc.State(ctx, pid)
	if err != nil {
		return nil, 0, false
	}
	return st, st.Seq, true
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	st, _, ok := s.loadState(r.Context(), pid, r)
	if !ok {
		fail(w, 404, "project not found")
		return
	}
	view := BuildView(st)
	writeJSON(w, view)
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	data, name, err := readUpload(r)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	res, err := s.svc.Import(r.Context(), pid, name, data)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"reports": res.Reports, "project": BuildView(res.State)})
}

func (s *Server) reviseParams(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	var req ParamsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	reason := "manual parameter adjustment"
	if req.Reason != nil {
		reason = *req.Reason
	}
	st, err := s.svc.ApplyParams(r.Context(), pid, func(p model.ParamSet) model.ParamSet {
		return s.applyParamsToState(p, req)
	}, reason)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, BuildView(st))
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	var req struct {
		Target    string         `json:"target"`
		Verdict   string         `json:"verdict"`
		Note      string         `json:"note"`
		ParamsUID string         `json:"params_uid"`
		Params    *ParamsRequest `json:"params,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Verdict == model.DecisionAdjusted && req.Params != nil {
		st, err := s.svc.ApplyParams(r.Context(), pid, *req.Params)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if cur := st.Params; cur != nil {
			req.ParamsUID = cur.UID
		}
	}
	st, d, err := s.svc.Decide(r.Context(), pid, req.Target, req.Verdict, req.Note, req.ParamsUID)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"decision": d, "project": BuildView(st)})
}

func (s *Server) retract(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	var req struct{ DecisionUID, Note string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	st, err := s.svc.Retract(r.Context(), pid, req.DecisionUID, req.Note)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, BuildView(st))
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	var req struct{ Target, Keep, Note string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	st, err := s.svc.ResolveConflict(r.Context(), pid, req.Target, req.Keep, req.Note)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, BuildView(st))
}

func (s *Server) note(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	var req struct{ Text string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	st, err := s.svc.Note(r.Context(), pid, req.Text)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, BuildView(st))
}

func readUpload(r *http.Request) (data []byte, name string, err error) {
	if ct := r.Header.Get("Content-Type"); len(ct) >= 19 && ct[:19] == "multipart/form-data" {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, "", err
		}
		f, h, ferr := r.FormFile("file")
		if ferr != nil {
			return nil, "", ferr
		}
		defer f.Close()
		data, err = io.ReadAll(f)
		return data, h.Filename, err
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	return body, "upload.csv", err
}
