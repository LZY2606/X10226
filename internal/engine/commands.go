package engine

import (
	"encoding/json"
	"fmt"

	"tga/internal/domain"
)

// CreateProject opens a brand new project aggregate.
func (e *Engine) CreateProject(id, name string) *domain.Event {
	if e.Project != nil {
		panic("engine already hosts a project")
	}
	return e.commit(e.buildEvent(id, domain.EvProjectCreated, map[string]any{"project_id": id, "name": name}, []string{id}))
}

// Import applies one import document. Accepted layers are versioned; rejected
// layers are recorded as import_rejected events carrying their fragment and
// exact errors (half-failure is observable and auditable).
func (e *Engine) Import(raw []byte) (*ImportResult, []*domain.Event) {
	layers, paramsEnv, docErr := ParseImport(raw)
	result := &ImportResult{}
	var newEvents []*domain.Event
	if docErr != nil {
		ev := e.buildEvent(e.Project.ID, domain.EvImportRejected, map[string]any{
			"scope": "document", "errors": []string{docErr.Error()},
			"fragment": string(raw),
		}, nil)
		e.commit(ev)
		result.Rejected = append(result.Rejected, RejectedLayer{Fragment: raw, Errors: []string{docErr.Error()}})
		return result, []*domain.Event{ev}
	}

	seq := int64(len(e.events) + 1)
	for _, in := range layers {
		src, errs := e.buildLayer(e.Project.ID, in, e.heads, seq)
		if len(errs) > 0 {
			frag, _ := json.Marshal(in)
			ev := e.buildEvent(e.Project.ID, domain.EvImportRejected, map[string]any{
				"scope": "layer", "layer_type": string(in.Type),
				"errors": errs, "fragment": string(frag),
			}, nil)
			e.commit(ev)
			result.Rejected = append(result.Rejected, RejectedLayer{
				Type: in.Type, Fragment: frag, Errors: errs,
			})
			continue
		}
		ev := e.buildEvent(e.Project.ID, domain.EvSourceImported, map[string]any{
			"layer_type": string(src.Type), "version": src.Version,
			"origin": string(src.Origin), "label": src.Label,
			"parent_id": src.ParentID,
		}, nil)
		putSource(ev, src)
		e.commit(ev)
		result.Accepted = append(result.Accepted, AcceptedLayer{Type: src.Type, Source: src})
		newEvents = append(newEvents, ev)
	}
	if paramsEnv != nil {
		ev := e.reviseParams(paramsEnv.Params)
		newEvents = append(newEvents, ev)
	}

	derived := e.recomputeAfterChange()
	newEvents = append(newEvents, derived...)
	return result, newEvents
}

// ReviseParams creates a new parameter version (parent chain) and recomputes.
func (e *Engine) ReviseParams(p domain.Params) []*domain.Event {
	ev := e.reviseParams(p)
	return append([]*domain.Event{ev}, e.recomputeAfterChange()...)
}

func (e *Engine) reviseParams(p domain.Params) *domain.Event {
	if p.AlgVersion == "" {
		p.AlgVersion = domain.AlgorithmVersion
	}
	seq := int64(len(e.events) + 1)
	src := &domain.Source{
		ProjectID: e.Project.ID, Type: domain.SourceParams, Label: "algorithm parameters",
		Origin: domain.OriginImported, CreatedEventSeq: seq, Params: &p,
		Basis: domain.CoordinateBasis{},
	}
	if prev := e.heads[domain.SourceParams]; prev != nil {
		src.ParentID = prev.ID
		src.Version = prev.Version + 1
	} else {
		src.Version = 1
	}
	src.ID = sourceID(src)
	ev := e.buildEvent(e.Project.ID, domain.EvParamRevised, map[string]any{
		"layer_type": string(src.Type), "version": src.Version, "parent_id": src.ParentID,
	}, nil)
	putSource(ev, src)
	return e.commit(ev)
}

// recomputeAfterChange rebuilds only layers whose direct inputs changed. Each
// layer's ID is content-addressed over its parameters and direct inputs, so an
// unchanged layer keeps the exact same derivation and identity.
func (e *Engine) recomputeAfterChange() []*domain.Event {
	c := e.cur()

	// Heads before this batch decide versions and which layers are unchanged.
	baseID := map[domain.DerivationKind]string{}
	baseVer := map[domain.DerivationKind]int{}
	for _, k := range []domain.DerivationKind{domain.DerivSeries, domain.DerivDTG, domain.DerivStages, domain.DerivResidue} {
		if h := e.headDeriv[k]; h != nil {
			baseID[k] = h.ID
			baseVer[k] = h.Version
		}
	}
	fp := paramFingerprint(c.params)

	seriesRes := buildSeries(c.samples, c.drift, c.blank, c.params)
	seriesID := deriveID(domain.DerivSeries, fp, seriesRes)
	seriesVer := candidateVersion(e, domain.DerivSeries, seriesID, baseVer[domain.DerivSeries])

	dtgRes := buildDTG(seriesRes, c.params)
	dtgID := deriveID(domain.DerivDTG, fp, []any{seriesID, dtgParamSubset(c.params)})
	dtgVer := candidateVersion(e, domain.DerivDTG, dtgID, baseVer[domain.DerivDTG])

	stagesRes := buildStages(seriesRes, dtgRes, c.gas, c.program, c.initMass, c.params)
	stagesID := deriveID(domain.DerivStages, fp, []any{seriesID, dtgID, stagesInput(c), stagesParamSubset(c.params)})
	stagesVer := candidateVersion(e, domain.DerivStages, stagesID, baseVer[domain.DerivStages])

	resRes := buildResidue(seriesRes, stagesRes, c.initMass)
	residueID := deriveID(domain.DerivResidue, fp, []any{seriesID, stagesID, residueInput(c), residueParamSubset(c.params)})
	residueVer := candidateVersion(e, domain.DerivResidue, residueID, baseVer[domain.DerivResidue])

	cands := []*domain.Derivation{
		candidate(e.Project.ID, domain.DerivSeries, seriesVer, fp,
			directSourceIDs(c.samples, c.drift, c.blank), seriesID, seriesRes),
		candidate(e.Project.ID, domain.DerivDTG, dtgVer, fp, []string{seriesID}, dtgID, dtgRes),
		candidate(e.Project.ID, domain.DerivStages, stagesVer, fp,
			directSourceIDs(c.gas, c.program, seriesID, dtgID), stagesID, stagesRes),
		candidate(e.Project.ID, domain.DerivResidue, residueVer, fp,
			directSourceIDs(c.initMass, seriesID, stagesID), residueID, resRes),
	}

	var changed []*domain.Derivation
	for _, d := range cands {
		if baseID[d.Kind] != d.ID {
			changed = append(changed, d)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	var events []*domain.Event
	ev := e.buildEvent(e.Project.ID, domain.EvDerived, map[string]any{"count": len(changed)}, nil)
	putDerivations(ev, changed)
	e.commit(ev)
	events = append(events, ev)

	js := collectJudgements(seriesRes, dtgRes, stagesRes, resRes)
	if len(js) > 0 {
		jev := e.buildEvent(e.Project.ID, domain.EvJudgement, map[string]any{"count": len(js)}, nil)
		var raw []any
		for _, j := range js {
			var m map[string]any
			jsonRoundTrip(j, &m)
			raw = append(raw, m)
		}
		jev.Payload["judgements"] = raw
		e.commit(jev)
		events = append(events, jev)
	}
	return events
}

func stagesInput(c currentSources) any {
	return []any{c.gas, c.program}
}
func residueInput(c currentSources) any { return c.initMass }

func candidateVersion(e *Engine, kind domain.DerivationKind, id string, baseVer int) int {
	if ex, ok := e.derivations[id]; ok {
		return ex.Version
	}
	return baseVer + 1
}

func candidate(projectID string, kind domain.DerivationKind, ver int, fp string, refs []string, id string, payload any) *domain.Derivation {
	d := &domain.Derivation{
		ID: id, ProjectID: projectID, Kind: kind, Version: ver,
		AlgVersion: domain.AlgorithmVersion, ParamFingerprint: fp, DirectSources: refs,
	}
	switch kind {
	case domain.DerivSeries:
		d.Series = payload.(*domain.SeriesResult)
	case domain.DerivDTG:
		d.DTG = payload.(*domain.DTGResult)
	case domain.DerivStages:
		d.Stages = payload.(*domain.StagesResult)
	case domain.DerivResidue:
		d.Residue = payload.(*domain.ResidueResultSet)
	}
	return d
}

func directSourceIDs(items ...any) []string {
	var out []string
	for _, it := range items {
		switch v := it.(type) {
		case *domain.Source:
			if v != nil {
				out = append(out, v.ID)
			}
		case string:
			if v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}


var _ = fmt.Sprintf
