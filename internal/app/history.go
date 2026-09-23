package app

import (
	"context"
	"encoding/json"
	"fmt"

	"tga-review/internal/calc"
	"tga-review/internal/domain"
)

// View returns the current synchronized review model.
func (s *Service) View(ctx context.Context, projectID string) (*ViewModel, error) {
	events, err := s.st.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.viewAt(ctx, projectID, len(events), 0)
}

// ViewAt replays the project state at exactly event seq (0 = empty project).
// All displayed values carry their historical node ids; current nodes are never
// projected onto the past.
func (s *Service) ViewAt(ctx context.Context, projectID string, seq int) (*ViewModel, error) {
	total, err := s.st.EventCount(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if seq < 0 || seq > total {
		return nil, fmt.Errorf("seq %d out of range [0,%d]", seq, total)
	}
	return s.viewAt(ctx, projectID, seq, total)
}

func (s *Service) viewAt(ctx context.Context, projectID string, seq, total int) (*ViewModel, error) {
	p, err := s.st.GetProject(ctx, projectID)
	if err != nil || p == nil {
		return nil, err
	}
	vm := &ViewModel{
		Project: domain.Project{ID: p.ID, Name: p.Name, Description: p.Description, CreatedAt: p.CreatedAt},
		AtSeq:   seq, CurrentSeq: total, Historical: total > 0 && seq < total,
	}
	// sources committed up to seq
	allSrc, err := s.st.AllSources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	srcCommitSeq := s.sourceCommitSeq(ctx, projectID)
	for _, src := range allSrc {
		if srcCommitSeq[src.ID] <= seq {
			vm.Sources = append(vm.Sources, src)
		}
	}
	// params version at seq
	vm.Params, vm.ParamsFP, _ = s.paramsAt(ctx, projectID, seq)

	// nodes created up to seq; recreate "current" status within the window
	histNodes, err := s.st.NodesAtEvent(ctx, projectID, seq)
	if err != nil {
		return nil, err
	}
	curOfKind := map[domain.NodeKind]domain.Node{}
	for _, n := range histNodes {
		curOfKind[n.Kind] = n // ascending order => last is latest
	}
	// decisions/conflicts
	allDec, err := s.st.AllDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	decSeq := s.decisionSeq(ctx, projectID)
	live := map[string]bool{}
	for _, d := range allDec {
		if decSeq[d.ID] <= seq {
			vm.Decisions = append(vm.Decisions, d)
			if d.Live {
				live[d.ID] = true
			}
		}
	}
	allConf, err := s.st.AllConflicts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	confSeq := s.conflictSeq(ctx, projectID)
	for _, c := range allConf {
		if confSeq[c.ID] <= seq {
			vm.Conflicts = append(vm.Conflicts, c)
		}
	}
	sesSeq := s.sessionSeq(ctx, projectID)
	allSes, _ := s.st.Sessions(ctx, projectID)
	for _, se := range allSes {
		if sesSeq[se.ID] <= seq {
			vm.Sessions = append(vm.Sessions, se)
		}
	}
	archSeq := s.archiveSeq(ctx, projectID)
	allArch, _ := s.st.ArchiveEntries(ctx, projectID)
	for _, a := range allArch {
		if archSeq[a.ID] <= seq {
			vm.Archive = append(vm.Archive, a)
		}
	}

	for _, n := range histNodes {
		nv := NodeView{
			ID: n.ID, Kind: string(n.Kind), Algo: n.Algo, AlgoVer: n.AlgoVer,
			ParamFP: n.ParamFP, Inputs: n.Inputs, Status: n.Status,
			SupersedBy: n.SupersedBy, CreatedEventSeq: n.CreatedEventSeq,
			Diag: n.Diag, Payload: n.Payload,
		}
		cur := curOfKind[n.Kind]
		nv.Current = cur.ID == n.ID
		vm.Nodes = append(vm.Nodes, nv)
	}

	// stage views from the stages node current at seq
	if sn, ok := curOfKind[domain.NodeStages]; ok {
		var st calc.Stages
		unmarshalNode(sn.Payload["stages"], &st)
		for _, c := range st.Candidates {
			sv := StageView{StageCandidate: c, StageKey: c.Key}
			for _, d := range vm.Decisions {
				if d.Target != c.Key || !live[d.ID] {
					continue
				}
				if d.Kind == domain.DecisionAcceptStage {
					sv.Live = "accept"
					sv.DecisionID = d.ID
				}
				if d.Kind == domain.DecisionRejectStage {
					sv.Live = "reject"
					sv.DecisionID = d.ID
				}
			}
			vm.Stages = append(vm.Stages, sv)
		}
	}
	if rn, ok := curOfKind[domain.NodeResidue]; ok {
		var chain calc.ResidueChain
		unmarshalNode(rn.Payload["chain"], &chain)
		vm.Residue = &ResidueView{
			NodeID: rn.ID, ResiduePct: chain.ResiduePct, Chain: rn.Payload["chain"],
			Status: rn.Status, Inputs: rn.Inputs,
			Stale: seq < total && false,
		}
	}

	// review state
	vm.ReviewState = s.reviewState(vm)
	return vm, nil
}

func (s *Service) reviewState(vm *ViewModel) string {
	for _, c := range vm.Conflicts {
		if c.Status == "open" {
			return "conflict"
		}
	}
	for _, d := range vm.Decisions {
		if d.Kind == domain.DecisionConfirmResidue && d.Live {
			return "completed"
		}
	}
	if vm.Residue != nil {
		return "in_review"
	}
	return "importing"
}

func (s *Service) paramsAt(ctx context.Context, projectID string, seq int) (domain.Params, string, error) {
	events, _ := s.st.Events(ctx, projectID)
	fp := ""
	for _, e := range events {
		if e.Seq > seq {
			break
		}
		if e.Type == domain.EvtDecision {
			if kind, _ := e.Payload["kind"].(string); kind == domain.DecisionAdjustParams {
				if p2, ok := e.Payload["payload"].(map[string]any); ok {
					if f, ok := p2["fingerprint"].(string); ok {
						fp = f
					}
				}
			}
		}
	}
	if fp == "" {
		def := domain.DefaultParams()
		return def, def.Fingerprint(), nil
	}
	p, err := s.st.ParamsAtFP(ctx, projectID, fp)
	return p, fp, err
}

// event->object sequence maps, derived once per view from the event log.
func (s *Service) sourceCommitSeq(ctx context.Context, projectID string) map[string]int {
	out := map[string]int{}
	events, _ := s.st.Events(ctx, projectID)
	all, _ := s.st.AllSources(ctx, projectID)
	byLayer := map[string]int{}
	// sources do not embed event seq; attribute them to the import events.
	type sfrag struct{ layer, id string }
	var allFrags []sfrag
	for _, src := range all {
		allFrags = append(allFrags, sfrag{string(src.Layer), src.ID})
	}
	consumed := map[string]bool{}
	walkFrags := func(payload any, seq int) {
		raw, _ := json.Marshal(payload)
		var probe struct {
			Fragments []struct {
				Layer    string `json:"Layer"`
				SourceID string `json:"SourceID"`
			} `json:"fragments"`
		}
		_ = json.Unmarshal(raw, &probe)
		for _, f := range probe.Fragments {
			if f.SourceID != "" && !consumed[f.SourceID] {
				out[f.SourceID] = seq
				consumed[f.SourceID] = true
				byLayer[f.Layer] = seq
			}
		}
	}
	for _, e := range events {
		switch e.Type {
		case domain.EvtImport:
			walkFrags(e.Payload, e.Seq)
		case domain.EvtDecision:
			if kind, _ := e.Payload["kind"].(string); kind == domain.DecisionReviseInitMass {
				if p2, ok := e.Payload["payload"].(map[string]any); ok {
					if id, ok := p2["source_id"].(string); ok {
						out[id] = e.Seq
					}
				}
			}
		}
	}
	return out
}

func (s *Service) decisionSeq(ctx context.Context, projectID string) map[string]int {
	out := map[string]int{}
	events, _ := s.st.Events(ctx, projectID)
	for _, e := range events {
		if e.Type == domain.EvtDecision {
			if id, ok := e.Payload["decision_id"].(string); ok {
				out[id] = e.Seq
			}
		}
	}
	return out
}

func (s *Service) conflictSeq(ctx context.Context, projectID string) map[string]int {
	out := map[string]int{}
	events, _ := s.st.Events(ctx, projectID)
	for _, e := range events {
		if e.Type == domain.EvtConflict {
			if id, ok := e.Payload["conflict_id"].(string); ok {
				out[id] = e.Seq
			}
		}
		if e.Type == domain.EvtConflictResolve {
			if id, ok := e.Payload["conflict_id"].(string); ok {
				// keep the creation seq
				if _, ok := out[id]; !ok {
					out[id] = e.Seq
				}
			}
		}
	}
	return out
}

func (s *Service) sessionSeq(ctx context.Context, projectID string) map[string]int {
	out := map[string]int{}
	events, _ := s.st.Events(ctx, projectID)
	for _, e := range events {
		if e.Type == domain.EvtImport {
			if id, ok := e.Payload["session_id"].(string); ok {
				out[id] = e.Seq
			}
		}
	}
	return out
}

func (s *Service) archiveSeq(ctx context.Context, projectID string) map[string]int {
	out := map[string]int{}
	events, _ := s.st.Events(ctx, projectID)
	for _, e := range events {
		if e.Type == domain.EvtScenarios {
			raw, _ := json.Marshal(e.Payload)
			var probe struct {
				Entries []struct {
					ID string `json:"id"`
				} `json:"entries"`
			}
			_ = json.Unmarshal(raw, &probe)
			for _, en := range probe.Entries {
				out[en.ID] = e.Seq
			}
		}
	}
	return out
}
