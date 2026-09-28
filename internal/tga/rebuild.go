package tga

import "encoding/json"

// Rebuild replays the entire event log from scratch. Artifacts are
// content-addressed and cached, so replaying history recomputes exactly the
// artifact versions each event saw; a local input change only adds/expires
// dependent ids.
func Rebuild(p *Project) error {
	p.commits = map[Section][]*RawCommit{}
	p.parsed = map[string]*ParsedSections{}
	p.headCommit = map[Section]string{}
	p.current = map[Section]*RawCommit{}
	p.Params = []Params{defaultParams()}
	p.params = defaultParams()
	p.artifacts = map[string]any{}
	p.decisionByKey = map[string]*Decision{}
	p.decisions = nil
	p.bases = nil
	if len(p.Events) == 0 {
		return nil
	}
	if pc, err := decodeProject(p.Events[0]); err == nil {
		p.Title = pc.Title
		p.Description = pc.Description
	}

	// Track undone events.
	undone := map[string]bool{}
	for _, ev := range p.Events {
		if ev.Kind == EvUndo {
			var pl UndoPayload
			if err := json.Unmarshal(ev.Payload, &pl); err == nil {
				undone[pl.TargetEventID] = true
			}
		}
	}

	for _, ev := range p.Events {
		if undone[ev.ID] {
			ev.Undone = true
			continue
		}
		ev.Undone = false
		switch ev.Kind {
		case EvProjectCreated:
			// title already set
		case EvRawCommitted:
			var pl RawCommittedPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			rc := &RawCommit{
				ID: pl.CommitID, ProjectID: p.ID, Seq: ev.Seq, Section: pl.Section,
				ParentCommit: pl.Parent, Format: pl.Format, RawText: pl.RawText,
				Accepted: pl.Accepted, Diagnostics: pl.Diags, SubmittedAt: ev.At, Label: pl.Label,
			}
			p.commits[pl.Section] = append(p.commits[pl.Section], rc)
			p.headCommit[pl.Section] = rc.ID
			parsed, _ := ParseInput(pl.RawText, rc.ID)
			p.parsed[rc.ID] = parsed
			if pl.Accepted {
				p.current[pl.Section] = rc
			}
		case EvParamsRevised:
			var pl ParamsRevisedPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			pl.Params.Version = len(p.Params)
			p.Params = append(p.Params, pl.Params)
			p.params = pl.Params
		case EvDecision:
			var pl DecisionPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			d := &Decision{
				ID: pl.DecisionID, EventSeq: ev.Seq, Subject: pl.Subject,
				CandidateKey: pl.CandidateKey, Action: pl.Action, Reason: pl.Reason,
				Reviewer: pl.Reviewer, Supersedes: pl.ConflictFrom,
				Resolution: pl.ConflictResolution, At: ev.At,
			}
			if pl.CandidateKey != "" {
				p.decisionByKey[pl.CandidateKey] = d
			}
			p.decisions = append(p.decisions, d)
			if pl.Adjust != nil {
				np := *pl.Adjust
				np.Version = len(p.Params)
				p.Params = append(p.Params, np)
				p.params = np
			}
		case EvRuleBasis:
			var pl RuleBasisPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			p.bases = append(p.bases, &pl)
		}
	}

	// Derive current artifacts for the *current* accepted sources and params.
	p.derive()
	return nil
}

func decodeProject(ev *Event) (ProjectCreatedPayload, error) {
	var pl ProjectCreatedPayload
	err := json.Unmarshal(ev.Payload, &pl)
	return pl, err
}

// derive builds the normalised layers, TG/DTG, stages and residue for the
// current lineage. Old artifacts remain in the map (viewable history); only
// current ids are reported by Views.
func (p *Project) derive() {
	par := p.params

	// normalise helper layers
	var driftN *NormDrift
	if c := p.current[SecDrift]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecDrift})
		id := artifactID(AlgoNormalise, par.fingerprint(), srcs, map[string]any{"section": SecDrift, "commit": c.ID})
		driftN = &NormDrift{ArtifactID: id, Algorithm: AlgoNormalise, ParamFP: par.fingerprint(), Sources: srcs, Drift: ps.Drift}
		p.artifacts[id] = driftN
	}
	var blankN *NormBlank
	if c := p.current[SecBlank]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecBlank})
		pts := make([]NormSample, 0, len(ps.Blank))
		for _, b := range ps.Blank {
			if t, ok1 := b.Time.NormFloat(); ok1 {
				if m, ok2 := b.BlankMass.NormFloat(); ok2 {
					pts = append(pts, NormSample{TimeMin: t, MassMg: m, Row: b.Row})
				}
			}
		}
		id := artifactID(AlgoNormalise, par.fingerprint(), srcs, map[string]any{"section": SecBlank, "commit": c.ID, "n": len(pts)})
		blankN = &NormBlank{ArtifactID: id, Algorithm: AlgoNormalise, ParamFP: par.fingerprint(), Sources: srcs, Points: pts}
		p.artifacts[id] = blankN
	}
	var initN *NormInitialMass
	if c := p.current[SecInitialMass]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecInitialMass})
		id := artifactID(AlgoNormalise, par.fingerprint(), srcs, map[string]any{"section": SecInitialMass, "commit": c.ID})
		initN = &NormInitialMass{ArtifactID: id, Algorithm: AlgoNormalise, ParamFP: par.fingerprint(), Sources: srcs, Initial: ps.InitialMass}
		p.artifacts[id] = initN
	}
	var gasN *NormGas
	if c := p.current[SecGas]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecGas})
		id := artifactID(AlgoNormalise, par.fingerprint(), srcs, map[string]any{"section": SecGas, "commit": c.ID})
		gasN = &NormGas{ArtifactID: id, Algorithm: AlgoNormalise, ParamFP: par.fingerprint(), Sources: srcs, Events: ps.Gas}
		p.artifacts[id] = gasN
	}
	var furnN *NormFurnace
	if c := p.current[SecFurnace]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecFurnace})
		id := artifactID(AlgoNormalise, par.fingerprint(), srcs, map[string]any{"section": SecFurnace, "commit": c.ID})
		furnN = &NormFurnace{ArtifactID: id, Algorithm: AlgoNormalise, ParamFP: par.fingerprint(), Sources: srcs, Steps: ps.Furnace}
		p.artifacts[id] = furnN
	}

	// samples
	var series *NormSeries
	if c := p.current[SecSamples]; c != nil {
		ps := p.parsed[c.ID]
		srcs := sourcesFor(p, []Section{SecSamples, SecDrift, SecBlank})
		series = buildSeries(ps.Samples, driftN, blankN, par, srcs, initN)
		p.artifacts[series.ArtifactID] = series
	}

	p.furnace = furnN
	p.gas = gasN
	p.series = series
	p.initial = initN
	if series == nil {
		p.tg = nil
		p.stages = nil
		p.residue = nil
		return
	}
	var initMass *float64
	if initN != nil && initN.Initial != nil && initN.Initial.Mass.OK() {
		v, _ := initN.Initial.Mass.NormFloat()
		initMass = &v
	}
	tg := buildTG(series, initMass, par)
	p.artifacts[tg.ArtifactID] = tg
	p.tg = tg

	st := buildStages(tg, series, gasN, par)
	// apply human decisions to candidate status by logical key
	for i := range st.Candidates {
		c := &st.Candidates[i]
		key := candidateKey(c)
		if d, ok := p.decisionByKey[key]; ok && !d.Undone {
			switch d.Action {
			case "accept":
				c.Status = "accepted"
			case "reject":
				c.Status = "rejected"
				c.RejectReasons = append(c.RejectReasons, "reviewer rejected: "+d.Reason)
			}
		}
	}
	p.artifacts[st.ArtifactID] = st
	p.stages = st

	res := buildResidue(series, initN, tg, par)
	p.artifacts[res.ArtifactID] = res
	p.residue = res
}

// candidateKey is the stable logical address across re-derivations.
func candidateKey(c *StageCandidate) string {
	return c.ClusterID + "/" + c.Variant
}
