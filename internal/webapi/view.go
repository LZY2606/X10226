package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"tgareview/internal/domain"
	"tgareview/internal/project"
)

type val struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Value  *float64 `json:"value"`
	Unit   string   `json:"unit"`
	Basis  string   `json:"basis"`
	Status string   `json:"status"`
}

type layerView struct {
	Kind       string `json:"kind"`
	HeadRev    string `json:"head_rev"`
	ParentRev  string `json:"parent_rev"`
	SourceFile string `json:"source_file"`
	Present    bool   `json:"present"`
}

type candView struct {
	ID             string   `json:"id"`
	GroupID        string   `json:"group_id"`
	Option         string   `json:"option"`
	StartS         float64  `json:"start_s"`
	EndS           float64  `json:"end_s"`
	PeakS          float64  `json:"peak_s"`
	PeakTSampleC   float64  `json:"peak_t_sample_c"`
	ActiveGas      string   `json:"active_gas"`
	ValveSwitchS   *float64 `json:"valve_switch_s"`
	ReactionOnsetS *float64 `json:"reaction_onset_s"`
	LossPct        *float64 `json:"loss_pct"`
	LossMg         *float64 `json:"loss_mg"`
	ResidueEndPct  *float64 `json:"residue_end_pct"`
	Status         string   `json:"status"`
	Reasons        []string `json:"reasons,omitempty"`
	OverlapsWith   []string `json:"overlaps_with,omitempty"`
	Decision       *decView `json:"decision,omitempty"`
	Conflict       bool     `json:"conflict"`
}

type decView struct {
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Rationale  string `json:"rationale"`
	Supersedes string `json:"supersedes,omitempty"`
	ConflictOf string `json:"conflict_of,omitempty"`
	Undone     bool   `json:"undone"`
}

type residueView struct {
	ID         string      `json:"id"`
	ResiduePct *float64    `json:"residue_pct"`
	Status     string      `json:"status"`
	Chain      []chainView `json:"chain"`
}
type chainView struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Value      *float64 `json:"value"`
	Unit       string   `json:"unit"`
	Basis      string   `json:"basis"`
	Status     string   `json:"status"`
	SourceKind string   `json:"source_kind,omitempty"`
	SourceRev  string   `json:"source_rev,omitempty"`
	Formula    string   `json:"formula,omitempty"`
	Diag       []string `json:"diagnostics,omitempty"`
}

type artView struct {
	ID                string            `json:"id"`
	AlgorithmVersion  string            `json:"algorithm_version"`
	ParamsFingerprint string            `json:"params_fingerprint"`
	Basis             map[string]string `json:"basis"`
	BasisSeq          int64             `json:"basis_seq"`
	Current           bool              `json:"current"`
	Stale             bool              `json:"stale"`
	Points            json.RawMessage   `json:"points"`
	Gaps              json.RawMessage   `json:"gaps"`
	Candidates        []candView        `json:"candidates"`
	Rejected          json.RawMessage   `json:"rejected_features"`
	Residue           residueView       `json:"residue"`
	Judgements        json.RawMessage   `json:"judgements"`
	Diagnostics       []string          `json:"diagnostics"`
}

type viewModel struct {
	ProjectID    string          `json:"project_id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Seq          int64           `json:"seq"`
	Layers       []layerView     `json:"layers"`
	Params       json.RawMessage `json:"params"`
	ParamsRevID  string          `json:"params_rev_id"`
	CurrentArtID string          `json:"current_artifact_id"`
	Artifacts    []artView       `json:"artifacts"`
	DecisionLog  []decView       `json:"decision_log"`
}

func (a *API) buildView(st *project.State, atSeq int64) viewModel {
	v := viewModel{
		ProjectID: st.ProjectID, Name: st.Name, Description: st.Description, Seq: st.Seq,
		CurrentArtID: st.CurrentArtifactID(),
	}
	for _, k := range domain.AllLayers() {
		rev := st.HeadRev[k]
		lv := layerView{Kind: string(k), HeadRev: rev, Present: rev != ""}
		if rev != "" {
			lv.ParentRev = st.RevParent[rev]
		}
		v.Layers = append(v.Layers, lv)
	}
	pb, _ := json.Marshal(st.Params)
	v.Params = pb
	v.ParamsRevID = st.ParamsRevID

	// stable artifact order: by basis seq
	type ord struct {
		id  string
		seq int64
	}
	var order []ord
	for _, id := range st.ArtifactOrder {
		order = append(order, ord{id, st.BasisSeq[id]})
	}
	for _, o := range order {
		art := st.Artifacts[o.id]
		av := artView{
			ID: art.ID, AlgorithmVersion: art.AlgorithmVersion,
			ParamsFingerprint: art.ParamsFingerprint, Basis: art.Basis,
			BasisSeq: o.seq, Current: art.ID == v.CurrentArtID, Stale: st.IsStale(art),
			Diagnostics: art.Diagnostics,
		}
		av.Points, _ = json.Marshal(art.Points)
		av.Gaps, _ = json.Marshal(art.Gaps)
		av.Rejected, _ = json.Marshal(art.Rejected)
		av.Judgements, _ = json.Marshal(art.Judgements)
		for _, c := range art.Candidates {
			cv := candView{
				ID: c.ID, GroupID: c.GroupID, Option: c.Option, StartS: c.StartS, EndS: c.EndS,
				PeakS: c.PeakS, PeakTSampleC: c.PeakTSampleC, ActiveGas: c.ActiveGas,
				ValveSwitchS: c.ValveSwitchS, ReactionOnsetS: c.ReactionOnsetS,
				LossPct: c.LossPct, LossMg: c.LossMg, ResidueEndPct: c.ResidueEndPct,
				Status: c.Status, Reasons: c.Reasons, OverlapsWith: c.OverlapsWith,
			}
			if d := st.DecisionFor(c.ID); d != nil {
				cv.Decision = &decView{ID: d.ID, Seq: d.Seq, Kind: d.Kind, Value: d.Value,
					Rationale: d.Rationale, Supersedes: d.Supersedes, ConflictOf: d.ConflictOf, Undone: d.Undone}
				cv.Conflict = d.ConflictOf != ""
			}
			av.Candidates = append(av.Candidates, cv)
		}
		rv := residueView{ID: art.Residue.ID, ResiduePct: art.Residue.ResiduePct, Status: art.Residue.Status}
		for _, n := range art.Residue.Nodes {
			rv.Chain = append(rv.Chain, chainView{
				ID: n.ID, Label: n.Label, Value: n.Value, Unit: n.Unit, Basis: n.Basis,
				Status: n.Status, SourceKind: n.SourceKind, SourceRev: n.SourceRev,
				Formula: n.Formula, Diag: n.Diagnostics,
			})
		}
		av.Residue = rv
		v.Artifacts = append(v.Artifacts, av)
	}
	for _, d := range st.DecisionLog {
		v.DecisionLog = append(v.DecisionLog, decView{
			ID: d.ID, Seq: d.Seq, Kind: d.Kind, Value: d.Value, Rationale: d.Rationale,
			Supersedes: d.Supersedes, ConflictOf: d.ConflictOf, Undone: d.Undone,
		})
	}
	return v
}

func (a *API) view(w http.ResponseWriter, r *http.Request) {
	st, err := a.svc.CurrentState(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, a.buildView(st, st.Seq))
}

func (a *API) viewAt(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		writeErr(w, 400, "seq must be an integer")
		return
	}
	st, err := a.svc.StateAt(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, a.buildView(st, seq))
}
