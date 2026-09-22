package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/example/tga-evidence/internal/app"
)

func (server *Server) loadCase(w http.ResponseWriter, request *http.Request) {
	pack := casepack.Builtin()
	id, err := server.service.CreateProject(request.Context(), pack.Title, pack.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, err = server.service.CommitSources(request.Context(), id, toCommitRequest(pack.InitialImport))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, _ := server.service.LoadProject(request.Context(), id)
	writeJSON(w, http.StatusCreated, map[string]any{"project_id": id, "snapshot": app.SnapshotFromProject(project)})
}

func (server *Server) replayStep(w http.ResponseWriter, request *http.Request) {
	var body struct {
		ProjectID string `json:"project_id"`
		Step      string `json:"step"`
	}
	if err := decode(request, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	pack := casepack.Builtin()
	var response any
	switch body.Step {
	case "initial_mass_revision":
		events, err := server.service.CommitSources(request.Context(), body.ProjectID, toCommitRequest(pack.Replay.InitialMassRevision))
		if err != nil {
			fail(w, err)
			return
		}
		response = events
	case "balance_drift_revision":
		events, err := server.service.CommitSources(request.Context(), body.ProjectID, toCommitRequest(pack.Replay.DriftRevision))
		if err != nil {
			fail(w, err)
			return
		}
		response = events
	case "local_raw_revision":
		events, err := server.service.CommitSources(request.Context(), body.ProjectID, toCommitRequest(pack.Replay.LocalRawRevision))
		if err != nil {
			fail(w, err)
			return
		}
		response = events
	case "import_half_failure":
		events, err := server.service.CommitSources(request.Context(), body.ProjectID, toCommitRequest(pack.Replay.HalfFailedImport))
		response = map[string]any{"events": events, "error": errorString(err)}
	case "parameter_adjustment":
		events, err := server.service.AdjustParameters(request.Context(), body.ProjectID, app.ParameterAdjustment{Reason: asString(pack.Replay.ParameterAdjustment["reason"]), Actor: asString(pack.Replay.ParameterAdjustment["actor"]), Parameters: asMap(pack.Replay.ParameterAdjustment["parameters"])})
		if err != nil {
			fail(w, err)
			return
		}
		response = events
	default:
		writeError(w, http.StatusBadRequest, errUnknownStep(body.Step))
		return
	}
	project, _ := server.service.LoadProject(request.Context(), body.ProjectID)
	writeJSON(w, http.StatusOK, map[string]any{"result": response, "snapshot": app.SnapshotFromProject(project)})
}

func toCommitRequest(raw map[string]any) app.CommitRequest {
	data, _ := json.Marshal(raw)
	var request app.CommitRequest
	_ = json.Unmarshal(data, &request)
	return request
}

func fail(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
	}
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func asString(value any) string      { text, _ := value.(string); return text }
func asMap(value any) map[string]any { typed, _ := value.(map[string]any); return typed }
