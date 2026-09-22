package httpapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/example/tga-evidence/internal/app"
	"github.com/example/tga-evidence/internal/casepack"
)

type Server struct {
	service *app.Service
	web     fs.FS
}

func New(service *app.Service, webFS fs.FS) *Server { return &Server{service: service, web: webFS} }

func (server *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.health)
	mux.HandleFunc("GET /api/casepack", server.casepack)
	mux.HandleFunc("POST /api/projects", server.createProject)
	mux.HandleFunc("GET /api/projects/{id}", server.getProject)
	mux.HandleFunc("POST /api/projects/{id}/import", server.commit)
	mux.HandleFunc("POST /api/projects/{id}/decisions", server.decide)
	mux.HandleFunc("POST /api/projects/{id}/decisions/undo", server.undo)
	mux.HandleFunc("POST /api/projects/{id}/parameters", server.parameters)
	mux.HandleFunc("GET /api/projects/{id}/events/{eventID}", server.stateAt)
	mux.HandleFunc("GET /api/projects/{id}/export", server.exportProject)
	mux.HandleFunc("POST /api/import-archive", server.importArchive)
	mux.HandleFunc("POST /api/demo/load-case", server.loadCase)
	mux.HandleFunc("POST /api/demo/replay-step", server.replayStep)
	mux.HandleFunc("GET /", server.ui)
	return logging(mux)
}

func (server *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
func (server *Server) casepack(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(casepack.BuiltinRaw())
}

func (server *Server) createProject(w http.ResponseWriter, request *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(request, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Name == "" {
		body.Name = "失重谱系复核"
	}
	id, err := server.service.CreateProject(request.Context(), body.Name, body.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project_id": id})
}

func (server *Server) getProject(w http.ResponseWriter, request *http.Request) {
	project, err := server.service.LoadProject(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, app.SnapshotFromProject(project))
}

func (server *Server) commit(w http.ResponseWriter, request *http.Request) {
	var commit app.CommitRequest
	if err := decode(request, &commit); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	events, err := server.service.CommitSources(request.Context(), request.PathValue("id"), commit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error(), "events": events})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "events": events})
}

func (server *Server) decide(w http.ResponseWriter, request *http.Request) {
	var decision app.DecisionRequest
	if err := decode(request, &decision); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	event, err := server.service.Decide(request.Context(), request.PathValue("id"), decision)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (server *Server) undo(w http.ResponseWriter, request *http.Request) {
	var body struct {
		DecisionID string `json:"decision_id"`
		Reason     string `json:"reason"`
		Actor      string `json:"actor"`
	}
	if err := decode(request, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	event, err := server.service.UndoDecision(request.Context(), request.PathValue("id"), body.DecisionID, body.Reason, body.Actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (server *Server) parameters(w http.ResponseWriter, request *http.Request) {
	var adjustment app.ParameterAdjustment
	if err := decode(request, &adjustment); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	events, err := server.service.AdjustParameters(request.Context(), request.PathValue("id"), adjustment)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"events": events})
}

func (server *Server) stateAt(w http.ResponseWriter, request *http.Request) {
	project, err := server.service.StateAtEvent(request.Context(), request.PathValue("id"), request.PathValue("eventID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, app.SnapshotFromProject(project))
}

func (server *Server) exportProject(w http.ResponseWriter, request *http.Request) {
	archive, err := server.service.Export(request.Context(), request.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="tga-evidence-archive.json"`)
	writeJSON(w, http.StatusOK, archive)
}

func (server *Server) importArchive(w http.ResponseWriter, request *http.Request) {
	var archive app.Archive
	if err := decode(request, &archive); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, events, err := server.service.ImportArchive(request.Context(), archive, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project_id": id, "event_count": len(events)})
}
