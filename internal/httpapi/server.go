// Package httpapi exposes the review workflow over HTTP.
package httpapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"tga/internal/app"
	"tga/internal/engine"
)

// Server holds dependencies.
type Server struct {
	App      *app.App
	Web      embed.FS
	CasePack embed.FS
	Now      func() time.Time
}

// Routes builds the mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("POST /api/projects/{id}/import", s.importLayer)
	mux.HandleFunc("POST /api/projects/{id}/params", s.reviseParams)
	mux.HandleFunc("POST /api/projects/{id}/decide", s.decide)
	mux.HandleFunc("POST /api/projects/{id}/undo", s.undo)
	mux.HandleFunc("GET /api/projects/{id}/state", s.state)
	mux.HandleFunc("GET /api/projects/{id}/events", s.events)
	mux.HandleFunc("GET /api/projects/{id}/artifacts/{aid}", s.artifact)
	mux.HandleFunc("GET /api/projects/{id}/chart", s.chart)
	mux.HandleFunc("GET /api/projects/{id}/export", s.exportArchive)
	mux.HandleFunc("POST /api/import-archive", s.importArchive)
	mux.HandleFunc("GET /api/casepack", s.listCasepack)
	mux.HandleFunc("GET /api/casepack/{name...}", s.getCasepack)
	mux.HandleFunc("POST /api/projects/{id}/casepack/{which}", s.loadCasepack)

	webRoot, _ := fs.Sub(s.Web, "web")
	mux.Handle("GET /", http.FileServer(http.FS(webRoot)))
	return logRequests(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "tga-review", "algorithm": engine.AlgorithmVersion})
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

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
