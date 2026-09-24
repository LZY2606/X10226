package engine

import (
	"fmt"

	"tgaline/internal/domain"
	"tgaline/internal/tgalang"
)

// CommitInput parses and commits every layer present in text. Valid prefixes
// commit even when the stream half-fails; truncated/unknown rows stay attached
// to their layer with diagnostics. Original text is never overwritten.
func (p *Project) CommitInput(text, origin string, trunc bool) ([]*domain.Layer, []domain.ImportDiagnostic, error) {
	sections, globals := tgalang.Parse(text, tgalang.Options{ExplicitTruncation: trunc})
	var committed []*domain.Layer
	var allDiags []domain.ImportDiagnostic
	allDiags = append(allDiags, globals...)

	for _, sec := range sections {
		if sec.Kind == "" {
			// unknown section: no typed layer, diagnostics already recorded
			continue
		}
		accepted, rejected := 0, 0
		for _, r := range sec.Rows {
			if r.Status == domain.RowAccepted {
				accepted++
			} else {
				rejected++
			}
		}
		version := 1
		parent := ""
		if cur := p.current[sec.Kind]; cur != nil {
			version = cur.Version + 1
			parent = cur.ID
		}
		layer := &domain.Layer{
			ProjectID: p.ID, Kind: sec.Kind, Version: version, ParentID: parent,
			Origin: origin, Rows: sec.Rows, Diags: sec.Diags, Truncated: sec.Truncated,
			AcceptedCount: accepted, RejectedCount: rejected,
			CreatedAt: p.clock.Now().Format("2006-01-02T15:04:05.000000000Z07:00"),
		}
		sha, err := domain.Hash(canonLayer(layer))
		if err != nil {
			return committed, allDiags, err
		}
		layer.SHA = sha
		layer.ID = "ly_" + string(sec.Kind) + "_v" + itoa(version) + "_" + sha[:12]
		if err := p.store.SaveLayer(layer); err != nil {
			return committed, allDiags, err
		}
		if _, err := p.append(domain.EvLayerCommitted, domain.LayerCommitPayload{
			LayerID: layer.ID, Kind: sec.Kind, Version: version, ParentID: parent,
			SHA: layer.SHA, Origin: origin,
			AcceptedCount: accepted, RejectedCount: rejected, Truncated: sec.Truncated,
		}); err != nil {
			return committed, allDiags, err
		}
		committed = append(committed, layer)
	}
	// automatic rebuild after input commit
	if _, err := p.Rebuild("commit:" + origin); err != nil {
		return committed, allDiags, err
	}
	return committed, allDiags, nil
}

// canonLayer gives the layer a stable content hash independent of its ID.
func canonLayer(l *domain.Layer) any {
	return []any{
		string(l.Kind), l.Version, l.ParentID, l.Origin,
		rowsCanon(l.Rows), diagsCanon(l.Diags), l.Truncated,
		l.AcceptedCount, l.RejectedCount,
	}
}

// ReviseParams installs a new immutable named parameter set and rebuilds.
func (p *Project) ReviseParams(name string, params map[string]float64, parent, reason string, rebuild bool) (*ParamsVersion, error) {
	if _, ok := p.params[name]; ok && parent == "" {
		return nil, fmt.Errorf("engine: params %q already exists", name)
	}
	if parent == "" {
		parent = p.CurrentParamsName()
	}
	if _, err := p.append(domain.EvParamsRevised, domain.ParamsPayload{
		Name: name, Params: params, ParentName: parent, Reason: reason,
	}); err != nil {
		return nil, err
	}
	if rebuild {
		if _, err := p.Rebuild("params:" + name); err != nil {
			return nil, err
		}
	}
	return p.params[name], nil
}

// Rebuild recomputes derived nodes. Structural sharing: a node is only written
// when its content-addressed id is new; unchanged inputs reproduce old ids so
// only dependent results change.
func (p *Project) Rebuild(cause string) (*BuildNodes, error) {
	paramsName := p.CurrentParamsName()
	pv := p.params[paramsName]
	if pv == nil {
		return nil, fmt.Errorf("engine: no parameters installed")
	}
	inputs := map[string]string{}
	versions := map[string]int{}
	for _, k := range domain.AllLayerKinds() {
		if l := p.current[k]; l != nil {
			inputs[string(k)] = l.ID
			versions[string(k)] = l.Version
		} else {
			inputs[string(k)] = ""
			versions[string(k)] = 0
		}
	}
	bid := domain.ShortID("bld_", []any{
		"build", AlgoVersion, pv.FP, inputs,
	})
	bn := ComputeBuild(p.current, pv.Params, bid)

	// parent build (most recent)
	parentBuild := ""
	if len(p.builds) > 0 {
		if pl, err := buildPayload(p.builds[len(p.builds)-1]); err == nil {
			parentBuild = pl.BuildID
		}
	}

	nodes := bn.nodeSlice()
	var ids []string
	for _, n := range nodes {
		if _, exists := p.nodes[n.ID]; !exists {
			if err := p.store.SaveNode(n); err != nil {
				return nil, err
			}
			p.nodes[n.ID] = n
		}
		ids = append(ids, n.ID)
	}
	sortStrings(ids)

	stale := false
	if len(p.builds) > 0 {
		prev, _ := buildPayload(p.builds[len(p.builds)-1])
		stale = prev.BuildID != bid
	}
	if _, err := p.append(domain.EvBuildSucceeded, domain.BuildPayload{
		BuildID: bid, Algorithm: "tga", AlgoVersion: AlgoVersion,
		ParamsName: paramsName, ParamsFP: pv.FP,
		Inputs: inputs, InputVersions: versions, NodeIDs: ids,
		Stale: stale, Cause: cause, ParentBuildID: parentBuild,
	}); err != nil {
		return nil, err
	}
	return &bn, nil
}

// Decide records an adjudication. A verdict opposing the current effective
// verdict on the same subject is stored as an explicit conflict (both kept);
// ResolveConflict later picks a winner.
func (p *Project) Decide(subjectID, kind, reason string, params map[string]float64) (*domain.Decision, error) {
	valid := map[string]bool{"accept": true, "reject": true, "adjust": true}
	if !valid[kind] {
		return nil, fmt.Errorf("engine: invalid decision kind %q", kind)
	}
	verdict := map[string]string{"accept": "accepted", "reject": "rejected", "adjust": "adjusted"}[kind]
	pl := domain.DecisionPayload{
		SubjectID: subjectID, Kind: kind, Verdict: verdict,
		Reason: reason, Params: params,
	}
	if prev := p.effective[subjectID]; prev != nil && isOpposing(prev.Kind, kind) {
		pl.ConflictWithEvent = prev.EventID
	}
	ev, err := p.append(domain.EvDecisionMade, pl)
	if err != nil {
		return nil, err
	}
	return decisionForEvent(p, ev.ID), nil
}

func decisionForEvent(p *Project, evID string) *domain.Decision {
	for _, d := range p.decisions {
		if d.EventID == evID {
			return d
		}
	}
	return nil
}

// Undo marks a prior decision undone; the undo itself is a history event.
func (p *Project) Undo(eventID, reason string) error {
	var target *domain.Decision
	for _, d := range p.decisions {
		if d.EventID == eventID {
			target = d
		}
	}
	if target == nil {
		return fmt.Errorf("engine: decision event %q not found", eventID)
	}
	if target.Undone {
		return fmt.Errorf("engine: decision %q already undone", eventID)
	}
	_, err := p.append(domain.EvDecisionUndone, map[string]string{
		"event_id": eventID, "reason": reason,
	})
	return err
}

// ResolveConflict chooses a winner among conflicting verdicts on a subject.
func (p *Project) ResolveConflict(subjectID, winnerEventID, reason string) error {
	found := false
	for _, d := range p.decisions {
		if d.SubjectID == subjectID && d.EventID == winnerEventID {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("engine: winner %q not found for subject %q", winnerEventID, subjectID)
	}
	_, err := p.append(domain.EvConflictResolved, map[string]string{
		"subject_id": subjectID, "winner_event_id": winnerEventID, "reason": reason,
	})
	return err
}

func (b *BuildNodes) nodeSlice() []*domain.Node {
	return []*domain.Node{b.Corrected, b.Furnace, b.Gap, b.Noise, b.DTG, b.TG,
		b.Peaks, b.Reaction, b.Stages, b.Residues}
}

func buildPayload(ev *domain.Event) (domain.BuildPayload, error) {
	var pl domain.BuildPayload
	err := recode(ev.Payload, &pl)
	return pl, err
}
