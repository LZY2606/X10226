package app

import (
	"context"
	"fmt"
	"time"

	"tga-review/internal/domain"
)

// DecideStage accepts or rejects one stage proposal. Accepting mutually
// exclusive proposals (the merged form vs. a split part of the same cluster)
// records a conflict that must be resolved before review completion.
func (s *Service) DecideStage(ctx context.Context, projectID, stageKey, action, rationale string) (*domain.Decision, *domain.Conflict, error) {
	stagesNode, err := s.st.CurrentNode(ctx, projectID, domain.NodeStages)
	if err != nil {
		return nil, nil, err
	}
	if stagesNode == nil {
		return nil, nil, fmt.Errorf("no stage candidates yet; import the sample layers first")
	}
	if !stageExists(stagesNode, stageKey) {
		return nil, nil, fmt.Errorf("stage candidate %s not found in current stages", stageKey)
	}
	kind := domain.DecisionAcceptStage
	if action == "reject" {
		kind = domain.DecisionRejectStage
	}
	d, err := s.addDecision(ctx, projectID, kind, stageKey, nil, rationale)
	if err != nil {
		return nil, nil, err
	}
	var conflict *domain.Conflict
	if kind == domain.DecisionAcceptStage {
		conflict = s.detectConflicts(ctx, projectID)
	}
	// recompute residue only (accepted set changed)
	if _, _, err := s.recompute(ctx, projectID, "decision:"+d.ID); err != nil {
		return nil, nil, err
	}
	return d, conflict, nil
}

func stageExists(n *domain.Node, key string) bool {
	var st struct {
		Candidates []struct {
			Key string `json:"key"`
		} `json:"candidates"`
	}
	unmarshalNode(n.Payload["stages"], &st)
	for _, c := range st.Candidates {
		if c.Key == key {
			return true
		}
	}
	return false
}

func (s *Service) addDecision(ctx context.Context, projectID, kind, target string, payload map[string]any, rationale string) (*domain.Decision, error) {
	seq, err := s.st.NextCounter(ctx, projectID, "decision")
	if err != nil {
		return nil, err
	}
	params, fp, err := s.st.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// capture the current derived node versions visible to the reviewer
	var nodeIDs []string
	for _, k := range []domain.NodeKind{domain.NodeCorrectedSeries, domain.NodeFurnaceTrace, domain.NodeGasTimeline, domain.NodeDTG, domain.NodeStages, domain.NodeResidue} {
		if n, _ := s.st.CurrentNode(ctx, projectID, k); n != nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	now := s.now()
	id := domain.SeqID(domain.KindDecision, projectID, seq)
	d := &domain.Decision{
		ID: id, ProjectID: projectID, Seq: seq, Kind: kind, Target: target,
		Payload: payload, NodeIDs: nodeIDs, ParamFP: fp, Rationale: rationale, Live: true, CreatedAt: now,
	}
	_ = params
	if err := s.st.InsertDecision(ctx, *d); err != nil {
		return nil, err
	}
	evSeq, err := s.nextEventSeq(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: evSeq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": evSeq, "id": id}),
		Type: domain.EvtDecision, CreatedAt: now,
		Payload: map[string]any{"decision_id": id, "kind": kind, "target": target, "payload": payload, "rationale": rationale},
	}); err != nil {
		return nil, err
	}
	return d, nil
}

// detectConflicts scans live accepts for mutually exclusive proposals of the
// same overlap cluster.
func (s *Service) detectConflicts(ctx context.Context, projectID string) *domain.Conflict {
	decs, _ := s.st.AllDecisions(ctx, projectID)
	live := map[string]string{} // stageKey -> decisionID
	for _, d := range decs {
		if d.Live && d.Kind == domain.DecisionAcceptStage {
			live[d.Target] = d.ID
		}
	}
	clusters := map[string][]string{}
	for key := range live {
		c := clusterOf(key)
		if c == "" {
			continue
		}
		clusters[c] = append(clusters[c], key)
	}
	for c, keys := range clusters {
		hasMerged := false
		var partKeys []string
		for _, k := range keys {
			if proposalOf(k) == "merged" {
				hasMerged = true
			}
			if len(proposalOf(k)) >= 6 && proposalOf(k)[:5] == "part_" {
				partKeys = append(partKeys, k)
			}
		}
		if hasMerged && len(partKeys) > 0 {
			ids := []string{live["s"+c+"_merged"]}
			for _, k := range partKeys {
				ids = append(ids, live[k])
			}
			return s.openConflict(ctx, projectID, c, keys, ids)
		}
	}
	return nil
}

func (s *Service) openConflict(ctx context.Context, projectID, cluster string, keys, ids []string) *domain.Conflict {
	// already open?
	confs, _ := s.st.AllConflicts(ctx, projectID)
	for _, cf := range confs {
		if cf.StageKey == "s"+cluster+"_*" && cf.Status == "open" {
			return &cf
		}
	}
	seq, _ := s.st.NextCounter(ctx, projectID, "conflict")
	now := s.now()
	id := domain.SeqID(domain.KindConflict, projectID, seq)
	c := &domain.Conflict{
		ID: id, ProjectID: projectID, Seq: seq, StageKey: "s" + cluster + "_*",
		DecisionIDs: ids, Status: "open",
		Note:        "accepted mutually exclusive proposals: " + fmt.Sprint(keys),
		CreatedAt:   now,
	}
	_ = s.st.InsertConflict(ctx, *c)
	evSeq, _ := s.nextEventSeq(ctx, projectID)
	_ = s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: evSeq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": evSeq, "id": id}),
		Type: domain.EvtConflict, CreatedAt: now,
		Payload: map[string]any{"conflict_id": id, "stage_key": c.StageKey, "decision_ids": ids},
	})
	return c
}

func clusterOf(key string) string {
	// s3_merged -> 3 ; s3_part_a -> 3
	if len(key) < 3 || key[0] != 's' {
		return ""
	}
	i := 1
	for i < len(key) && key[i] >= '0' && key[i] <= '9' {
		i++
	}
	if i == 1 || i >= len(key) || key[i] != '_' {
		return ""
	}
	return key[1:i]
}

func proposalOf(key string) string {
	for i := 0; i+1 < len(key); i++ {
		if key[i] == '_' {
			return key[i+1:]
		}
	}
	return ""
}

// ResolveConflict applies a reviewer resolution and writes it to history.
func (s *Service) ResolveConflict(ctx context.Context, projectID, conflictID, resolution string) (*domain.Conflict, error) {
	confs, err := s.st.AllConflicts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var cf *domain.Conflict
	for i := range confs {
		if confs[i].ID == conflictID {
			cf = &confs[i]
		}
	}
	if cf == nil {
		return nil, fmt.Errorf("conflict not found")
	}
	status := map[string]string{
		"keep_first":   "resolved_keep_first",
		"keep_second":  "resolved_keep_second",
		"reject_both":  "resolved_reject_both",
	}[resolution]
	if status == "" {
		return nil, fmt.Errorf("resolution must be keep_first|keep_second|reject_both")
	}
	// decision order in cf: first is merged, others parts
	switch resolution {
	case "keep_first":
		for i := 1; i < len(cf.DecisionIDs); i++ {
			_ = s.st.SetDecisionLive(ctx, cf.DecisionIDs[i], false, "conflict:"+cf.ID)
		}
	case "keep_second":
		_ = s.st.SetDecisionLive(ctx, cf.DecisionIDs[0], false, "conflict:"+cf.ID)
	case "reject_both":
		for _, id := range cf.DecisionIDs {
			_ = s.st.SetDecisionLive(ctx, id, false, "conflict:"+cf.ID)
		}
	}
	now := s.now()
	if err := s.st.UpdateConflict(ctx, cf.ID, status, now); err != nil {
		return nil, err
	}
	evSeq, _ := s.nextEventSeq(ctx, projectID)
	_ = s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: evSeq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": evSeq, "id": cf.ID}),
		Type: domain.EvtConflictResolve, CreatedAt: now,
		Payload: map[string]any{"conflict_id": cf.ID, "resolution": status},
	})
	if _, _, err := s.recompute(ctx, projectID, "conflict_resolution:"+cf.ID); err != nil {
		return nil, err
	}
	cf.Status = status
	return cf, nil
}

// UndoDecision records an undo event and flips the decision non-live.
func (s *Service) UndoDecision(ctx context.Context, projectID, decisionID string) error {
	decs, err := s.st.AllDecisions(ctx, projectID)
	if err != nil {
		return err
	}
	var target *domain.Decision
	for i := range decs {
		if decs[i].ID == decisionID {
			target = &decs[i]
		}
	}
	if target == nil {
		return fmt.Errorf("decision not found")
	}
	if !target.Live {
		return fmt.Errorf("decision is not live")
	}
	if err := s.st.SetDecisionLive(ctx, decisionID, false, "undo"); err != nil {
		return err
	}
	ud, err := s.addDecision(ctx, projectID, domain.DecisionUndo, decisionID, map[string]any{"undone": decisionID}, "undo")
	if err != nil {
		return err
	}
	ud.Live = false
	_ = s.st.SetDecisionLive(ctx, ud.ID, false, "undo-marker")
	if _, _, err := s.recompute(ctx, projectID, "undo:"+decisionID); err != nil {
		return err
	}
	return nil
}

var _ = time.Now
