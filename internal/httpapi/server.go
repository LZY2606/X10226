// Package httpapi serves the review UI and the JSON/SVG API.
package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"tgaaudit/internal/store"
)

// Server wires the store to HTTP handlers.
type Server struct {
	Store    *store.Store
	Web      fs.FS
	BasePath string
}

// New constructs an API server.
func New(st *store.Store, web fs.FS) *Server {
	return &Server{Store: st, Web: web}
}

// Routes returns the configured mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getSnapshot)
	mux.HandleFunc("GET /api/projects/{id}/events/{seq}", s.getSnapshotAt)
	mux.HandleFunc("POST /api/projects/{id}/import", s.importBundle)
	mux.HandleFunc("POST /api/projects/{id}/params", s.reviseParams)
	mux.HandleFunc("POST /api/projects/{id}/decide", s.decide)
	mux.HandleFunc("POST /api/projects/{id}/conflicts/{cid}/resolve", s.resolve)
	mux.HandleFunc("POST /api/projects/{id}/undo", s.undo)
	mux.HandleFunc("GET /api/projects/{id}/export", s.exportProject)
	mux.HandleFunc("POST /api/import-events", s.importEvents)
	mux.HandleFunc("GET /api/projects/{id}/nodes/{nid}", s.getNode)
	mux.HandleFunc("GET /api/projects/{id}/chart/{kind}", s.chart)
	mux.HandleFunc("GET /api/cases", s.listCases)
	mux.HandleFunc("GET /api/cases/{id}", s.getCase)
	if s.Web != nil {
		mux.HandleFunc("GET /", s.serveWeb)
	}
	return logging(mux)
}

func logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(s.Web, name)
	if err != nil {
		data, err = fs.ReadFile(s.Web, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name = "index.html"
	}
	ct := "text/plain; charset=utf-8"
	switch {
	case strings.HasSuffix(name, ".html"):
		ct = "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		ct = "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		ct = "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Write(data)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

func ctxOf(r *http.Request) context.Context { return r.Context() }
