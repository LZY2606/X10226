// Package server exposes the review workflow over HTTP. Every result is
// addressable by stable id and traceable through provenance, decisions and the
// event ledger.
package server

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"tga-review/internal/app"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	svc *app.Service
	mux *http.ServeMux
}

func New(svc *app.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.index)
	s.mux.HandleFunc("GET /api/projects", s.listProjects)
	s.mux.HandleFunc("POST /api/projects", s.createProject)
	s.mux.HandleFunc("GET /api/projects/{id}/state", s.state)
	s.mux.HandleFunc("POST /api/projects/{id}/import", s.importData)
	s.mux.HandleFunc("POST /api/projects/{id}/params", s.reviseParams)
	s.mux.HandleFunc("POST /api/projects/{id}/run", s.runPipeline)
	s.mux.HandleFunc("POST /api/projects/{id}/decide", s.decide)
	s.mux.HandleFunc("POST /api/projects/{id}/resolve", s.resolve)
	s.mux.HandleFunc("POST /api/projects/{id}/undo", s.undo)
	s.mux.HandleFunc("GET /api/projects/{id}/export", s.exportBundle)
	s.mux.HandleFunc("POST /api/import-bundle", s.importBundle)
}

func writeJSON(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, map[string]string{"error": err.Error()}, code)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.svc.Store.DB().QueryContext(r.Context(),
		`SELECT id,name,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		fail(w, 500, err)
		return
	}
	defer rows.Close()
	type P struct{ ID, Name, CreatedAt string }
	var out []P
	for rows.Next() {
		var p P
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			fail(w, 500, err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, out, 200)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		fail(w, 400, errBad("name required"))
		return
	}
	id, err := s.svc.CreateProject(r.Context(), req.Name)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]string{"project_id": id, "name": req.Name}, 201)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	asOf, _ := strconv.ParseInt(r.URL.Query().Get("as_of_seq"), 10, 64)
	st, err := s.svc.Store.Replay(r.Context(), id, asOf)
	if err != nil {
		fail(w, 404, err)
		return
	}
	layers, err := s.svc.Store.Layers(r.Context(), id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	batches, err := s.svc.Store.Batches(r.Context(), id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{
		"state": st, "layers": layers, "batches": batches,
	}, 200)
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Source string `json:"source"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	if req.Source == "" {
		req.Source = "ui-upload"
	}
	res, err := s.svc.Import(r.Context(), id, req.Source, []byte(req.Text))
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, res, 201)
}

func (s *Server) reviseParams(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Reviewer string         `json:"reviewer"`
		Reason   string         `json:"reason"`
		Params   paramsPayload  `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	evID, err := s.svc.ReviseParams(r.Context(), id, orDefault(req.Reviewer, "reviewer"), req.Reason, req.Params.toDomain())
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"event_id": evID}, 201)
}

func (s *Server) runPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct{ Reviewer string `json:"reviewer"` }
	_ = json.NewDecoder(r.Body).Decode(&req)
	set, err := s.svc.RunPipeline(r.Context(), id, orDefault(req.Reviewer, "reviewer"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, set, 201)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req decideReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	d := req.toDomain()
	evID, err := s.svc.Decide(r.Context(), id, orDefault(req.Reviewer, "reviewer"), d)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"event_id": evID, "conflict_of": ""}, 201)
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Reviewer                       string `json:"reviewer"`
		ObjectID, First, Second, Winner, Resolution string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	evID, err := s.svc.ResolveConflict(r.Context(), id, orDefault(req.Reviewer, "reviewer"),
		req.ObjectID, req.First, req.Second, req.Winner, req.Resolution)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"event_id": evID}, 201)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Reviewer string `json:"reviewer"`
		EventID  string `json:"event_id"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	evID, err := s.svc.Undo(r.Context(), id, orDefault(req.Reviewer, "reviewer"), req.EventID, req.Reason)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"event_id": evID}, 201)
}

func (s *Server) exportBundle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, err := s.svc.Export(r.Context(), id)
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="tga-bundle.json"`)
	writeJSON(w, b, 200)
}

func (s *Server) importBundle(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	id, err := s.svc.ImportBundle(r.Context(), raw)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"project_id": id}, 201)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

type errStr string

func (e errStr) Error() string { return string(e) }
func errBad(m string) error    { return errStr(m) }
