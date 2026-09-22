// Package httpapi exposes the review workflow over HTTP. The server renders
// synchronized SVG panels server-side; a tiny vanilla-JS layer adds the
// expandable calculation chains, rulings, history time-travel and import/export.
package httpapi

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"tga-archive/internal/app"
	"tga-archive/internal/cases"
	"tga-archive/internal/domain"
	"tga-archive/internal/store"
	"tga-archive/internal/svg"
)

type Server struct {
	App   *app.App
	Store *store.Store
	Tmpl  *template.Template
}

func New(a *app.App) *Server {
	tmpl := template.Must(template.New("page").Parse(pageHTML))
	return &Server{App: a, Store: a.Store, Tmpl: tmpl}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/", s.static)
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("POST /api/projects/{id}/import-bundle", s.importBundle)
	mux.HandleFunc("POST /api/projects/{id}/import-layer", s.importLayer)
	mux.HandleFunc("POST /api/projects/{id}/params", s.reviseParams)
	mux.HandleFunc("POST /api/projects/{id}/rulings", s.rule)
	mux.HandleFunc("POST /api/projects/{id}/resolve", s.resolve)
	mux.HandleFunc("POST /api/projects/{id}/undo", s.undo)
	mux.HandleFunc("GET /api/projects/{id}/events", s.events)
	mux.HandleFunc("GET /api/projects/{id}/state", s.state)
	mux.HandleFunc("GET /api/projects/{id}/state/{seq}", s.stateAt)
	mux.HandleFunc("GET /api/projects/{id}/export", s.export)
	mux.HandleFunc("POST /api/import-archive", s.importArchive)
	mux.HandleFunc("GET /api/cases", s.listCases)
	mux.HandleFunc("GET /api/cases/{cid}", s.getCase)
	mux.HandleFunc("POST /api/cases/{cid}/instantiate", s.instantiateCase)
	mux.HandleFunc("GET /api/cases/{cid}/file/{name}", s.caseFile)
	return logging(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, code2, msg string) {
	writeJSON(w, code, map[string]any{"error": code2, "message": msg})
}

func logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))).ServeHTTP(w, r)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	projects, _ := s.Store.Projects()
	cs := cases.List()
	data := struct {
		Projects []store.ProjectInfo
		Cases    []cases.Manifest
	}{projects, cs}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.Tmpl.Execute(w, data)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		CaseID string `json:"caseId"`
		Seed   int64  `json:"seed"`
		Note   string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "bad_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "missing_name", "project name is required")
		return
	}
	info, err := s.App.CreateProject(req.Name, req.CaseID, req.Seed, req.Note)
	if err != nil {
		writeErr(w, 500, "create_failed", err.Error())
		return
	}
	writeJSON(w, 201, info)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.Projects()
	if err != nil {
		writeErr(w, 500, "db_error", err.Error())
		return
	}
	writeJSON(w, 200, ps)
}

type pageData struct {
	Project     store.ProjectInfo
	State       *app.State
	Panels      map[string]template.HTML
	Events      []domain.Envelope
	StaleCount  int
	TotalNodes  int
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var st *app.State
	var err error
	if at := r.URL.Query().Get("at"); at != "" {
		seq, perr := strconv.ParseInt(at, 10, 64)
		if perr != nil {
			writeErr(w, 400, "bad_at", "at must be an event seq")
			return
		}
		st, err = s.App.StateAt(id, seq)
	} else {
		st, err = s.App.CurrentState(id)
	}
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, 200, st)
		return
	}
	evs, _ := s.Store.Events(id)
	total, stale, _ := s.Store.NodeCount(id)
	data := pageData{
		Project: st.Project, State: st, Events: evs, StaleCount: stale, TotalNodes: total,
		Panels: map[string]template.HTML{
			"tgdtg":     template.HTML(svg.TGDTG(st)),
			"furnace":   template.HTML(svg.Furnace(st)),
			"atmosphere": template.HTML(svg.Atmosphere(st)),
			"stages":    template.HTML(svg.Stages(st)),
		},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = reviewTmpl(st).Execute(w, data)
}

func reviewTmpl(_ *app.State) *template.Template {
	return template.Must(template.New("review").Funcs(template.FuncMap{
		"fmtMeasure": fmtMeasure,
		"fmtPct":     fmtPct,
		"temC": func(k float64) float64 { return k - 273.15 },
		"statusClass": func(st string) string {
			switch st {
			case "ruled_accepted":
				return "ok"
			case "ruled_rejected":
				return "err"
			default:
				return "warn"
			}
		},
		"diagClass": func(sev string) string {
			switch sev {
			case "error":
				return "err"
			case "warning":
				return "warn"
			default:
				return ""
			}
		},
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}).Parse(reviewHTML))
}

func fmtMeasure(m domain.Measure) string {
	if !m.Valid {
		return m.Raw + " (unconverted)"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", m.Value), "0"), ".") + " " + m.Unit
}

func fmtPct(m domain.Measure) string {
	if !m.Valid {
		return "unknown %"
	}
	return fmt.Sprintf("%.2f %%", m.Value)
}

func decode(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

var _ = strconv.Itoa
