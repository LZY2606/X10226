package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"tga/internal/app"
	"tga/internal/archive"
	"tga/internal/model"
	"tga/internal/store"
	"tga/internal/svg"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.App.ListProjects(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"projects": ps})
}

type createReq struct {
	Name string `json:"name"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "name is required")
		return
	}
	p, err := s.App.CreateProject(r.Context(), req.Name)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.App.Store.GetProject(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNoProject) {
		writeErr(w, 404, "project not found")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

type importReq struct {
	Layer  string `json:"layer"`
	Raw    string `json:"raw"`
	Source string `json:"source"`
}

func (s *Server) importLayer(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	layer := model.Layer(req.Layer)
	if !layer.Valid() {
		writeErr(w, 400, "layer must be one of mass_samples/blank_curve/furnace_program/gas_valve_events/initial_mass")
		return
	}
	sub, ev, err := s.App.ImportLayer(r.Context(), r.PathValue("id"), app.ImportInput{
		Layer: layer, Raw: req.Raw, Source: req.Source,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"submission": sub, "event_seq": ev.Seq, "status": sub.Status,
		"accepted_rows": sub.AcceptedRows, "total_rows": sub.TotalRows})
}

type paramsReq struct {
	Text string            `json:"text"`
	Map  map[string]string `json:"values"`
}

func (s *Server) reviseParams(w http.ResponseWriter, r *http.Request) {
	var req paramsReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	values := req.Map
	if strings.TrimSpace(req.Text) != "" {
		v, err := app.ParseParamsText(req.Text)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		values = v
	}
	if len(values) == 0 {
		writeErr(w, 400, "no parameters provided")
		return
	}
	ev, err := s.App.ReviseParams(r.Context(), r.PathValue("id"), values)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"event_seq": ev.Seq})
}

type decideReq struct {
	Target string            `json:"target"`
	Action string            `json:"action"`
	Reason string            `json:"reason"`
	Adjust map[string]string `json:"adjust"`
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var req decideReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ev, err := s.App.Decide(r.Context(), r.PathValue("id"), app.DecideInput{
		Target: req.Target, Action: req.Action, Reason: req.Reason, Adjust: req.Adjust,
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"event_seq": ev.Seq})
}

type undoReq struct {
	TargetSeq int64  `json:"target_seq"`
	Reason    string `json:"reason"`
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	var req undoReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetSeq == 0 {
		writeErr(w, 400, "target_seq required")
		return
	}
	ev, err := s.App.Undo(r.Context(), r.PathValue("id"), req.TargetSeq, req.Reason)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"event_seq": ev.Seq})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	seq := int64(0)
	if q := r.URL.Query().Get("seq"); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			writeErr(w, 400, "bad seq")
			return
		}
		seq = v
	}
	st, err := s.replay(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	headEvents, _ := s.App.Store.Events(r.Context(), r.PathValue("id"))
	headSeq := int64(0)
	if len(headEvents) > 0 {
		headSeq = headEvents[len(headEvents)-1].Seq
	}
	writeJSON(w, 200, buildView(st, headSeq))
}

func (s *Server) replay(ctx context.Context, id string, seq int64) (*engine.State, error) {
	if seq > 0 {
		return s.App.ReplayAt(ctx, id, seq)
	}
	return s.App.ReplayHead(ctx, id)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	evs, err := s.App.Store.Events(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(evs))
	for _, ev := range evs {
		var payload any
		_ = json.Unmarshal(ev.Payload, &payload)
		out = append(out, map[string]any{
			"seq": ev.Seq, "id": ev.ID, "type": ev.Type, "prev_id": ev.PrevID,
			"created_at": ev.CreatedAt, "payload": payload,
		})
	}
	writeJSON(w, 200, map[string]any{"events": out})
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	st, err := s.App.ReplayHead(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	a := st.Artifacts[r.PathValue("aid")]
	if a == nil {
		writeErr(w, 404, "artifact not found")
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) chart(w http.ResponseWriter, r *http.Request) {
	seq := int64(0)
	if q := r.URL.Query().Get("seq"); q != "" {
		seq, _ = strconv.ParseInt(q, 10, 64)
	}
	st, err := s.replay(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	headEvents, _ := s.App.Store.Events(r.Context(), r.PathValue("id"))
	headSeq := int64(0)
	if len(headEvents) > 0 {
		headSeq = headEvents[len(headEvents)-1].Seq
	}
	out, err := svg.Render(st.State, headSeq)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	_, _ = io.WriteString(w, out)
}

func (s *Server) exportArchive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tga-archive.zip"`)
	if err := archive.Export(r.Context(), s.App.Store, r.PathValue("id"), w); err != nil {
		writeErr(w, 500, err.Error())
	}
}

func (s *Server) importArchive(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeErr(w, 400, "multipart file required (field: file)")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer f.Close()
	arc, err := archive.Import(r.Context(), s.App.Store, f)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"project": arc.Project, "events": len(arc.Events)})
}
