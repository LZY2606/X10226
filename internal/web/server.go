// Package web serves the review UI: synchronized TG/DTG, furnace program,
// atmosphere timeline and stage candidates, expandable residue calc
// chains, decisions/undo, time-travel, export and import.
package web

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"tgaview/internal/casepack"
	"tgaview/internal/core"
	"tgaview/internal/store"
	"tgaview/internal/tga"
)

// Server is the review HTTP server.
type Server struct {
	St  *store.Store
	Mux *http.ServeMux
}

// New wires all routes.
func New(st *store.Store) *Server {
	s := &Server{St: st, Mux: http.NewServeMux()}
	s.Mux.HandleFunc("/", s.handleIndex)
	s.Mux.HandleFunc("/review", s.handleReview)
	s.Mux.HandleFunc("/source/", s.handleSource)
	s.Mux.HandleFunc("/derived/", s.handleDerived)
	s.Mux.HandleFunc("/api/chain/", s.handleChain)
	s.Mux.HandleFunc("/events", s.handleEvents)
	s.Mux.HandleFunc("/export", s.handleExport)
	s.Mux.HandleFunc("/import", s.handleImport)
	s.Mux.HandleFunc("/action/replay", s.handleReplay)
	s.Mux.HandleFunc("/action/recompute", s.handleRecompute)
	s.Mux.HandleFunc("/action/decision", s.handleDecision)
	s.Mux.HandleFunc("/action/undo", s.handleUndo)
	return s
}

func esc(s string) string { return html.EscapeString(s) }

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *Server) stateAt(seq int64) (*core.State, error) { return core.RebuildState(s.St, seq) }

func latestDerived(state *core.State, kind string) *core.Derived {
	var best *core.Derived
	for _, id := range state.DerivedOrder {
		d := state.Derived[id]
		if d.Kind == kind && (best == nil || d.Seq > best.Seq) {
			best = d
		}
	}
	return best
}

func decode[T any](d *core.Derived) (*T, error) {
	var v T
	if err := json.Unmarshal(d.Body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// correctedBody is the payload shape of the tg.corrected derived value.
type correctedBody struct {
	Series   *tga.Series   `json:"series"`
	Segments []tga.Segment `json:"segments"`
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	redirect(w, r, "/review")
}

// ---- actions ---------------------------------------------------------------

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := casepack.Replay(s.St); err != nil {
		http.Error(w, "replay failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/review")
}

func (s *Server) handleRecompute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := core.RunPipelineCurrent(s.St); err != nil {
		http.Error(w, "recompute failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/review")
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target := r.FormValue("target")
	action := r.FormValue("action")
	reviewer := r.FormValue("reviewer")
	rationale := r.FormValue("rationale")
	supersedes := r.FormValue("supersedes")
	if target == "" || reviewer == "" {
		http.Error(w, "target and reviewer are required", http.StatusBadRequest)
		return
	}
	switch action {
	case "accept", "reject", "adjust":
	default:
		http.Error(w, "action must be accept|reject|adjust", http.StatusBadRequest)
		return
	}
	params := map[string]any{}
	for _, k := range []string{"atmos_delay_min", "atmos_tau_min", "min_rate_mg_min", "onset_ratio", "overlap_ratio", "noise_band_mg", "gap_threshold_min", "dtg_window"} {
		if v := r.FormValue("param_" + k); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				http.Error(w, fmt.Sprintf("param %s: %v", k, err), http.StatusBadRequest)
				return
			}
			params[k] = f
		}
	}
	head, _ := s.St.Head()
	id := "dec-" + tga.Fingerprint(map[string]any{
		"target": target, "action": action, "reviewer": reviewer,
		"rationale": rationale, "params": params, "head": head,
	})
	if _, err := s.St.Append(core.EvDecisionMade, target, core.DecisionPayload{
		ID: id, Target: target, Action: action, Params: params,
		Rationale: rationale, Reviewer: reviewer, Supersedes: supersedes,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// parameter adjustments immediately produce a new computation
	if action == "adjust" && len(params) > 0 {
		if err := core.RunPipelineCurrent(s.St); err != nil {
			http.Error(w, "recompute after adjust failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	redirect(w, r, "/review")
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := r.FormValue("decision_id")
	reason := r.FormValue("reason")
	if id == "" {
		http.Error(w, "decision_id required", http.StatusBadRequest)
		return
	}
	if _, err := s.St.Append(core.EvDecisionUndone, id, core.UndoPayload{DecisionID: id, Reason: reason}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/review")
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/jsonl; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tgaview-export.jsonl"`)
	if err := s.St.Export(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body io.Reader
	if f, _, err := r.FormFile("file"); err == nil {
		defer f.Close()
		body = f
	} else if txt := r.FormValue("payload"); txt != "" {
		body = strings.NewReader(txt)
	} else {
		http.Error(w, "provide a file upload or pasted payload", http.StatusBadRequest)
		return
	}
	n, err := s.St.Import(body)
	if err != nil {
		http.Error(w, "import failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = n
	redirect(w, r, "/review")
}

// ---- read views --------------------------------------------------------------

func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	// /source/{kind}/{version}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/source/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	ver, err := strconv.Atoi(parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	state, err := s.stateAt(0)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	src := state.Sources[parts[0]]
	if src == nil {
		http.NotFound(w, r)
		return
	}
	for _, v := range src.Versions {
		if v.Version == ver {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintf(w, "# source=%s version=v%d parent=v%d seq=%d format=%s\n", v.SourceID, v.Version, v.Parent, v.Seq, v.Format)
			if v.Note != "" {
				fmt.Fprintf(w, "# note=%s\n", v.Note)
			}
			io.WriteString(w, v.Raw)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) handleDerived(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/derived/")
	state, err := s.stateAt(0)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d := state.Derived[id]
	if d == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"id": d.ID, "kind": d.Kind, "algo_version": d.AlgoVersion,
		"param_fingerprint": d.ParamFingerprint, "params": d.Params,
		"inputs": d.Inputs, "stale": d.Stale, "stale_reasons": d.StaleReasons,
		"body": json.RawMessage(d.Body),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/chain/")
	seq, _ := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
	state, err := s.stateAt(seq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	d := state.Derived[id]
	if d == nil {
		http.Error(w, "no such derived: "+esc(id), 404)
		return
	}
	res, err := decode[tga.Residue](d)
	if err != nil {
		http.Error(w, "not a residue chain: "+esc(id), 400)
		return
	}
	var b strings.Builder
	b.WriteString(`<table class="chain"><tr><th>步骤</th><th>值</th><th>单位/基准</th><th>来源身份</th></tr>`)
	for _, c := range res.Chain {
		ref := c.Ref
		if strings.HasPrefix(ref, "src:") {
			sid, v, _ := splitSrcRef(ref)
			ref = fmt.Sprintf(`<a href="/source/%s/%d">%s</a>`, sid, v, esc(c.Ref))
		} else if strings.HasPrefix(ref, "der:") {
			ref = fmt.Sprintf(`<a href="/derived/%s">%s</a>`, strings.TrimPrefix(c.Ref, "der:"), esc(c.Ref))
		}
		fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			esc(c.Label), esc(c.Value), esc(c.Unit), ref)
	}
	b.WriteString(`</table>`)
	if d.Stale {
		fmt.Fprintf(&b, `<p class="stale">此计算链已过期（%s），仅作历史查看。</p>`, esc(strings.Join(d.StaleReasons, "；")))
	}
	io.WriteString(w, b.String())
}

func splitSrcRef(ref string) (string, int, bool) {
	var sid string
	var v int
	if _, err := fmt.Sscanf(strings.TrimPrefix(ref, "src:"), "%[^@]@v%d", &sid, &v); err != nil {
		return "", 0, false
	}
	return sid, v, true
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	evs, err := s.St.Events(0)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var b strings.Builder
	b.WriteString(pageHead("事件历史"))
	b.WriteString(`<p><a href="/review">← 返回复核</a></p><table><tr><th>seq</th><th>tick</th><th>kind</th><th>entity</th><th>回看</th></tr>`)
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		fmt.Fprintf(&b, `<tr><td>%d</td><td>%d</td><td>%s</td><td class="mono">%s</td><td><a href="/review?seq=%d">恢复此事件视角</a></td></tr>`,
			e.Seq, e.At, esc(e.Kind), esc(e.Entity), e.Seq)
	}
	b.WriteString(`</table></body></html>`)
	io.WriteString(w, b.String())
}

// sortedKeys is a small helper for deterministic rendering.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
