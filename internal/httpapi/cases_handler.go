package httpapi

import (
	"encoding/json"
	"net/http"

	"tgaaudit/internal/cases"
)

func (s *Server) listCases(w http.ResponseWriter, r *http.Request) {
	m := cases.BuildManifest()
	writeJSON(w, 200, map[string]any{
		"manifest": m,
		"baseline": cases.Baseline(),
		"bundles":  scenarioBundles(),
	})
}

func (s *Server) getCase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "baseline-v1" {
		writeJSON(w, 200, cases.Baseline())
		return
	}
	if id == "manifest" {
		writeJSON(w, 200, cases.BuildManifest())
		return
	}
	for _, sc := range cases.Scenarios() {
		if sc.ID == id {
			writeJSON(w, 200, map[string]any{
				"scenario": sc, "bundle": sc.Build(),
			})
			return
		}
	}
	writeErr(w, 404, errStr("unknown case: "+id))
}

func scenarioBundles() []map[string]any {
	out := []map[string]any{}
	for _, sc := range cases.Scenarios() {
		b, _ := json.Marshal(sc.Build())
		out = append(out, map[string]any{"id": sc.ID, "title": sc.Title,
			"description": sc.Description, "changed": sc.Changed,
			"bundle": json.RawMessage(b)})
	}
	return out
}
