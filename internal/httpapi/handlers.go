package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"tgaaudit/internal/cases"
	"tgaaudit/internal/domain"
	"tgaaudit/internal/store"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"projects": ps})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Seed int64  `json:"seed"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "TGA review"
	}
	if req.Seed == 0 {
		req.Seed = cases.Seed
	}
	id, err := s.Store.CreateProject(r.Context(), req.Name, req.Seed, req.Note)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	snap, err := s.Store.Snapshot(r.Context(), id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 201, snap)
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.Store.Snapshot(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) getSnapshotAt(w http.ResponseWriter, r *http.Request) {
	seq := atoiPath(r.PathValue("seq"))
	if seq <= 0 {
		writeJSON(w, 400, errStr("seq must be positive"))
		return
	}
	snap, err := s.Store.Snapshot(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.Node(r.Context(), r.PathValue("id"), r.PathValue("nid"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, n)
}

// importBundle accepts either a domain Bundle JSON or {"sections": {...}}.
func (s *Server) importBundle(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	inputs, checksum, ok := decodeBundleInputs(w, body)
	if !ok {
		return
	}
	res, err := s.Store.Import(r.Context(), projectID, checksum, inputs)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	code := 200
	if res.Attempt.Partial {
		code = 202
	}
	writeJSON(w, code, res)
}

func decodeBundleInputs(w http.ResponseWriter, body []byte) ([]store.SectionInput, string, bool) {
	b, diags := domain.ParseBundle(strings.NewReader(string(body)))
	if len(diags) > 0 || b == nil {
		writeErr(w, 400, errStr(strings.Join(diags, "; ")))
		return nil, "", false
	}
	var inputs []store.SectionInput
	// deterministic section order
	all := append([]string{}, domain.SectionKinds...)
	for k := range b.Sections {
		if !contains(all, k) {
			diags = append(diags, "unknown section: "+k)
		}
	}
	if len(diags) > 0 {
		writeErr(w, 400, errStr(strings.Join(diags, "; ")))
		return nil, "", false
	}
	for _, kind := range all {
		raw, ok := b.Sections[kind]
		if !ok {
			continue
		}
		t, perr := domain.DecodeTable(raw)
		inputs = append(inputs, store.SectionInput{Kind: kind, Table: t, ParseE: perr})
	}
	return inputs, b.BundleID + checksumBytes(body), true
}

func checksumBytes(b []byte) string {
	return ""
}

func (s *Server) reviseParams(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Params domain.Params `json:"params"`
		Reason string        `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Params.AlgorithmVersion == "" {
		req.Params.AlgorithmVersion = domain.CurrentParamsVersion
	}
	snap, err := s.Store.ReviseParams(r.Context(), r.PathValue("id"),
		req.Params, req.Reason)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		By         string             `json:"by"`
		TargetKind string             `json:"target_kind"`
		TargetID   string             `json:"target_id"`
		Verdict    string             `json:"verdict"`
		Rationale  string             `json:"rationale"`
		Adjustment map[string]float64 `json:"adjustment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.TargetKind == "" {
		req.TargetKind = "node"
	}
	dec, conflict, err := s.Store.Decide(r.Context(), r.PathValue("id"),
		req.By, req.TargetKind, req.TargetID, req.Verdict, req.Rationale,
		req.Adjustment)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"decision": dec, "conflict": conflict})
}

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WinnerID string `json:"winner_id"`
		Winner   string `json:"winner"` // convenience: verdict or decision id
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	winner := req.WinnerID
	if winner == "" {
		winner = req.Winner
	}
	snap, err := s.Store.ResolveConflict(r.Context(), r.PathValue("id"),
		r.PathValue("cid"), winner, req.Reason)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DecisionSeq int    `json:"decision_seq"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	snap, err := s.Store.Undo(r.Context(), r.PathValue("id"),
		req.DecisionSeq, req.Reason)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) {
	b, err := s.Store.Export(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	w.Header().Set("Content-Disposition",
		`attachment; filename="tga-project-`+r.PathValue("id")+`.json"`)
	writeJSON(w, 200, b)
}

func (s *Server) importEvents(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var b store.ExportBundle
	if err := json.Unmarshal(body, &b); err != nil {
		writeErr(w, 400, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	id, err := s.Store.ImportEvents(r.Context(), &b, req.Name)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	snap, err := s.Store.Snapshot(r.Context(), id, 0)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 201, snap)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type stringErr string

func (e stringErr) Error() string { return string(e) }
func errStr(s string) error       { return stringErr(s) }

func atoiPath(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
