package derive

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// Replay projects an ordered event log into State. Pure and deterministic.
func Replay(events []domain.Event) (*State, error) {
	st := &State{
		RawVersions: map[domain.Layer]LayerVersion{},
		Imports:     map[string][]ImportRecordState{},
		Rules:       map[string]map[string]any{},
		ReviewStage: "",
	}
	for _, e := range events {
		if err := st.apply(e); err != nil {
			return nil, fmt.Errorf("event %s: %w", e.ID, err)
		}
	}
	return st, nil
}

func (st *State) apply(e domain.Event) error {
	st.TipSeq = e.Seq
	switch e.Type {
	case domain.EvProjectCreated:
		st.ProjectID = asString(e.Payload["project_id"])
		st.Name = asString(e.Payload["name"])
		st.Seed = asInt64(e.Payload["seed"])
		if st.ReviewStage == "" {
			st.ReviewStage = domain.StageImporting
		}
	case domain.EvImportOpened:
		id := asString(e.Payload["import_id"])
		if _, ok := st.Imports[id]; !ok {
			st.Imports[id] = nil
			st.ImportOrder = append(st.ImportOrder, id)
		}
	case domain.EvRawRecordAdded:
		importID := asString(e.Payload["import_id"])
		var rec domain.RawRecord
		if err := decode(e.Payload["record"], &rec); err != nil {
			return err
		}
		st.Imports[importID] = append(st.Imports[importID], ImportRecordState{ImportID: importID, Record: rec})
	case domain.EvImportClosed:
		// At closure, parsed strata are installed as version 1. The engine is
		// responsible for emitting layer_revised events; here we just move the
		// review stage forward when requested.
		if s := asString(e.Payload["next_stage"]); s != "" {
			st.ReviewStage = s
		}
	case domain.EvReviewStageMoved:
		st.ReviewStage = asString(e.Payload["stage"])
	case domain.EvLayerRevised:
		return st.applyLayerRevised(e)
	case domain.EvDerivedComputed:
		return st.applyDerivedComputed(e)
	case domain.EvDecisionMade:
		return st.applyDecision(e)
	case domain.EvConflictNoted:
		return st.applyConflict(e, true)
	case domain.EvConflictResolved:
		return st.applyConflictResolved(e)
	case domain.EvJudgmentRule:
		return st.applyRule(e)
	}
	return nil
}

func (st *State) applyLayerRevised(e domain.Event) error {
	layer := domain.Layer(asString(e.Payload["layer"]))
	ver := LayerVersion{
		StableID: asString(e.Payload["stable_id"]),
		Version:  int(asInt64(e.Payload["version"])),
		ParentID: asString(e.Payload["parent_id"]),
		Reason:   asString(e.Payload["reason"]),
		AtSeq:    e.Seq,
		SourceID: asString(e.Payload["source_id"]),
	}
	st.RawVersions[layer] = ver

	switch layer {
	case domain.LayerRawPoints:
		var pts []domain.RawPoint
		if err := decode(e.Payload["points"], &pts); err != nil {
			return err
		}
		st.RawPoints = pts
	case domain.LayerBlank:
		var pts []domain.RawPoint
		if err := decode(e.Payload["points"], &pts); err != nil {
			return err
		}
		st.BlankPoints = pts
	case domain.LayerFurnace:
		var segs []domain.FurnaceSegment
		if err := decode(e.Payload["segments"], &segs); err != nil {
			return err
		}
		st.Furnace = segs
	case domain.LayerGas:
		var valves []domain.ValveEvent
		if err := decode(e.Payload["valves"], &valves); err != nil {
			return err
		}
		st.Valves = valves
	case domain.LayerInitialMass:
		var rv domain.RawValue
		if err := decode(e.Payload["raw"], &rv); err != nil {
			return err
		}
		im := &InitialMass{LayerVersion: ver, Raw: rv, Known: rv.Converted, MassMg: rv.Value}
		st.InitialMass = im
	case domain.LayerDrift:
		st.Drift = &DriftSpec{LayerVersion: ver, Model: asString(e.Payload["model"]), Note: asString(e.Payload["note"])}
	case domain.LayerParameters:
		var p Parameters
		if err := decode(e.Payload["parameters"], &p); err != nil {
			return err
		}
		st.Params = p
		st.ParamsHistory = append(st.ParamsHistory, p)
	}
	return nil
}

func (st *State) applyDerivedComputed(e domain.Event) error {
	var d Derived
	if err := decode(e.Payload["derived"], &d); err != nil {
		return err
	}
	d.AtSeq = e.Seq
	ref := DerivedSnapshotRef{
		AtSeq:        e.Seq,
		Invalidated:  asStringSlice(e.Payload["invalidated"]),
		SeriesID:     asString(e.Payload["series_id"]),
		CandidateIDs: asStringSlice(e.Payload["candidate_ids"]),
		ResidueID:    asString(e.Payload["residue_id"]),
	}
	st.History = append(st.History, ref)
	st.LatestDerived = &d
	return nil
}

// AppliedCandidates overlays the live (non-retracted) decisions on the latest
// computed candidates.
func (st *State) AppliedCandidates() []Candidate {
	if st.LatestDerived == nil {
		return nil
	}
	cands := append([]Candidate(nil), st.LatestDerived.Candidates...)
	byID := map[string]int{}
	for i := range cands {
		byID[cands[i].ID] = i
	}
	for _, dec := range st.Decisions {
		if dec.Retracted {
			continue
		}
		i, ok := byID[dec.CandidateID]
		if !ok {
			continue
		}
		cands[i].DecisionIDs = append(cands[i].DecisionIDs, dec.ID)
		switch dec.Kind {
		case "accept":
			cands[i].Status = CandStatusAccepted
		case "reject":
			cands[i].Status = CandStatusRejected
		case "adjust":
			cands[i].Status = CandStatusAdjusted
			cands[i].Adjusted = true
			if dec.AdjustOnsetS != nil {
				cands[i].OnsetS = *dec.AdjustOnsetS
				cands[i].OnsetC = tempAt(st.LatestDerived.Series.Points, *dec.AdjustOnsetS)
			}
			if dec.AdjustOffsetS != nil {
				cands[i].OffsetS = *dec.AdjustOffsetS
				cands[i].OffsetC = tempAt(st.LatestDerived.Series.Points, *dec.AdjustOffsetS)
			}
			recomputeLoss(&cands[i], st)
		}
	}
	return cands
}

func recomputeLoss(c *Candidate, st *State) {
	if st.LatestDerived == nil || st.LatestDerived.Series == nil || st.InitialMass == nil {
		return
	}
	mOn, ok1 := massAt(st.LatestDerived.Series.Points, c.OnsetS)
	mOff, ok2 := massAt(st.LatestDerived.Series.Points, c.OffsetS)
	if ok1 && ok2 {
		c.LossMg = mOn - mOff
		c.LossPct = c.LossMg / st.InitialMass.MassMg * 100
		c.BoundsKnown = true
	}
}

func (st *State) applyDecision(e domain.Event) error {
	d := Decision{
		ID:          asString(e.Payload["decision_id"]),
		CandidateID: asString(e.Payload["candidate_id"]),
		CandSeq:     asInt64(e.Payload["candidate_seq"]),
		Kind:        asString(e.Payload["kind"]),
		Reason:      asString(e.Payload["reason"]),
		AtSeq:       e.Seq,
		RetractsID:  asString(e.Payload["retracts_id"]),
	}
	if v, ok := e.Payload["adjust_onset_s"]; ok {
		f := asFloat(v)
		d.AdjustOnsetS = &f
	}
	if v, ok := e.Payload["adjust_offset_s"]; ok {
		f := asFloat(v)
		d.AdjustOffsetS = &f
	}
	if d.RetractsID != "" {
		for i := range st.Decisions {
			if st.Decisions[i].ID == d.RetractsID {
				st.Decisions[i].Retracted = true
			}
		}
	}
	if cid := asString(e.Payload["conflict_id"]); cid != "" {
		d.ConflictIDs = append(d.ConflictIDs, cid)
	}
	st.Decisions = append(st.Decisions, d)
	return nil
}

func (st *State) applyConflict(e domain.Event, pending bool) error {
	c := Conflict{
		ID:          asString(e.Payload["conflict_id"]),
		CandidateID: asString(e.Payload["candidate_id"]),
		DecisionA:   asString(e.Payload["decision_a"]),
		DecisionB:   asString(e.Payload["decision_b"]),
		Pending:     pending,
		AtSeq:       e.Seq,
	}
	st.Conflicts = append(st.Conflicts, c)
	return nil
}

func (st *State) applyConflictResolved(e domain.Event) error {
	id := asString(e.Payload["conflict_id"])
	for i := range st.Conflicts {
		if st.Conflicts[i].ID == id {
			st.Conflicts[i].Pending = false
			st.Conflicts[i].Resolution = asString(e.Payload["resolution"])
			st.Conflicts[i].ResolvedBy = asString(e.Payload["decision_id"])
		}
	}
	return nil
}

func (st *State) applyRule(e domain.Event) error {
	ruleID := asString(e.Payload["rule_id"])
	m := map[string]any{}
	if body, ok := e.Payload["assertion"]; ok {
		if err := decode(body, &m); err != nil {
			return err
		}
	}
	m["event_id"] = e.ID
	m["seq"] = e.Seq
	st.Rules[ruleID] = m
	return nil
}

// ViewAt rebuilds the state as it was at an inclusive seq for history views.
func ViewAt(events []domain.Event, seq int64) (*State, error) {
	var cut []domain.Event
	for _, e := range events {
		if e.Seq <= seq {
			cut = append(cut, e)
		}
	}
	return Replay(cut)
}

// ---------- small payload helpers ----------

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, a := range arr {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func decode(v any, dst any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
