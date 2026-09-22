package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"tgaaudit/internal/domain"
	"tgaaudit/internal/pipeline"
)

// Snapshot builds the review read-model. asOfSeq=0 returns the current state;
// otherwise it replays events up to and including that seq and reconstructs
// the exact dependencies and conclusions visible then, without overwriting
// history with the latest computation.
func (s *Store) Snapshot(ctx context.Context, projectID string, asOfSeq int) (*DerivationSnapshot, error) {
	events, err := s.eventsUntil(ctx, projectID, asOfSeq)
	if err != nil {
		return nil, err
	}
	snap := &DerivationSnapshot{
		ProjectID:       projectID,
		CurrentBatchIDs: map[string]string{},
		Stages:          map[string][]pipeline.Stage{},
		Residues:        map[string]pipeline.ResidueData{},
	}

	// latest batch per kind and latest params as of the cutoff
	batchesByKind := map[string]domain.Batch{}
	var params *domain.Params
	var fp string
	var undoneSeqs map[int]bool
	conflicts := []domain.Conflict{}
	undos := []domain.UndoRecord{}
	attempts := []domain.ImportAttempt{}
	allBatches := []domain.Batch{}
	var derivedSeqs []int

	for _, ev := range events {
		switch ev.Type {
		case domain.EvProjectCreated:
			var pl struct {
				Name string `json:"name"`
				Seed int64  `json:"seed"`
				Note string `json:"note"`
			}
			_ = json.Unmarshal(ev.Payload, &pl)
			snap.ProjectName = pl.Name
			snap.Seed = pl.Seed
		case domain.EvCommitted:
			var bat domain.Batch
			if err := json.Unmarshal(ev.Payload, &bat); err != nil {
				return nil, err
			}
			bat.EventID = ev.ID
			batchesByKind[bat.Kind] = bat
			allBatches = append(allBatches, bat)
		case domain.EvParamRevised:
			var pl struct {
				Params      domain.Params `json:"params"`
				Fingerprint string        `json:"fingerprint"`
			}
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil, err
			}
			p := pl.Params
			params = &p
			fp = pl.Fingerprint
		case domain.EvDerived:
			derivedSeqs = append(derivedSeqs, ev.Seq)
			var pl struct {
				SetID string `json:"set_id"`
			}
			_ = json.Unmarshal(ev.Payload, &pl)
			_ = pl.SetID
		case domain.EvImportAttempted:
			var att domain.ImportAttempt
			if err := json.Unmarshal(ev.Payload, &att); err != nil {
				return nil, err
			}
			attempts = append(attempts, att)
		case domain.EvDecision:
			var dec domain.Decision
			if err := json.Unmarshal(ev.Payload, &dec); err != nil {
				return nil, err
			}
		case domain.EvUndid:
			var u domain.UndoRecord
			if err := json.Unmarshal(ev.Payload, &u); err != nil {
				return nil, err
			}
			undos = append(undos, u)
			if undoneSeqs == nil {
				undoneSeqs = map[int]bool{}
			}
			undoneSeqs[u.UndoneSeq] = true
		case domain.EvConflict, domain.EvResolved:
			var c domain.Conflict
			if err := json.Unmarshal(ev.Payload, &c); err != nil {
				return nil, err
			}
			conflicts = upsertConflict(conflicts, c, ev)
		}
	}

	// Filter decisions whose event was later undone.
	decisions := []domain.Decision{}
	decByID := map[string]domain.Decision{}
	for _, ev := range events {
		if ev.Type != domain.EvDecision {
			continue
		}
		var dec domain.Decision
		_ = json.Unmarshal(ev.Payload, &dec)
		if undoneSeqs[ev.Seq] {
			continue
		}
		decisions = append(decisions, dec)
		decByID[dec.ID] = dec
	}
	// apply conflict winner annotation
	for i := range decisions {
		for _, c := range conflicts {
			if c.Winner != "" && decisions[i].ID != c.Winner &&
				(decisions[i].ID == c.DecisionA || decisions[i].ID == c.DecisionB) {
				decisions[i].Verdict = "overridden_by_conflict_resolution"
			}
		}
	}

	// Build the batches argument for derivation.
	cur := pipeline.CurrentBatches{}
	for k, b2 := range batchesByKind {
		b := b2
		cur[k] = &b
		snap.CurrentBatchIDs[k] = b.ID
	}
	p := domain.DefaultParams()
	if params != nil {
		p = *params
	}
	if fp == "" {
		fp = pipeline.ParamFingerprint(p)
	}
	der := pipeline.Derive(cur, p)
	snap.CurrentParams = p
	snap.ParamFingerprint = fp
	snap.CurrentSetID = der.SetID
	snap.Batches = allBatches
	snap.Attempts = attempts
	snap.Decisions = decisions
	snap.Conflicts = conflicts
	snap.Undos = undos
	snap.Events = events
	if len(events) > 0 {
		snap.AsOfSeq = events[len(events)-1].Seq
	}

	// Nodes visible at this point in history: every node created by a derived
	// event at or before cutoff. A node is "current" if it belongs to the
	// newest derivation set; older nodes remain readable as superseded.
	lastSetIDs := map[string]bool{}
	if len(derivedSeqs) > 0 {
		lastSetIDs = nodeIDsAtSeq(events, derivedSeqs[len(derivedSeqs)-1])
	}
	nodes := []NodeView{}
	decByTarget := map[string]*domain.Decision{}
	for i := range decisions {
		decByTarget[decisions[i].TargetID] = &decisions[i]
	}
	seenNode := map[string]bool{}
	for _, ev := range events {
		if ev.Type != domain.EvDerived {
			continue
		}
		var pl struct {
			SetID string   `json:"set_id"`
			Nodes []string `json:"node_ids"`
		}
		_ = json.Unmarshal(ev.Payload, &pl)
		for _, id := range pl.Nodes {
			if seenNode[id] {
				continue
			}
			seenNode[id] = true
		}
	}
	// Read node payloads from projections table (nodes are immutable once
	// written). For an at-seq view, only nodes introduced by events <= cutoff
	// would exist after an export/reimport; therefore load only those ids.
	visible := nodesIntroducedBy(events)
	if asOfSeq == 0 {
		// current: table contains all nodes ever; mark state via set.
		allNodes, err := s.loadNodes(ctx, projectID, nil)
		if err != nil {
			return nil, err
		}
		currentIDs := map[string]bool{}
		for _, n := range der.Nodes {
			currentIDs[n.ID] = true
		}
		for _, n := range allNodes {
			state := "superseded"
			if currentIDs[n.ID] {
				state = "current"
			}
			nodes = append(nodes, NodeView{Node: n, State: state,
				Decision: decByTarget[n.ID]})
		}
	} else {
		allNodes, err := s.loadNodes(ctx, projectID, visible)
		if err != nil {
			return nil, err
		}
		for _, n := range allNodes {
			state := "superseded"
			if n.ID == "" {
				continue
			}
			if lastSetIDs[n.ID] {
				state = "current"
			}
			nodes = append(nodes, NodeView{Node: n, State: state,
				Decision: decByTarget[n.ID]})
		}
	}
	sortNodes(nodes)
	snap.Nodes = nodes

	// Materialise current derivation payloads for the charts.
	snap.Corrected = der.Corrected
	snap.Dtg = der.Dtg
	snap.Gaps = der.Gaps
	snap.PeakSets = der.PeakSets
	snap.Stages = der.Stages
	snap.Reactions = der.Reactions
	snap.Residues = der.Residues
	snap.Diagnostics = der.Diagnostics

	// Rule events from the current set projection.
	rows, err := s.db.QueryContext(ctx,
		`SELECT code,subject,severity,message,refs FROM rule_events
		 WHERE project_id=? AND set_id=? ORDER BY seq_in_set`,
		projectID, der.SetID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var code, subj, sev, msg, refs string
			if err := rows.Scan(&code, &subj, &sev, &msg, &refs); err != nil {
				return nil, err
			}
			re := domain.RuleEvent{Code: code, Subject: subj, Severity: sev, Message: msg}
			_ = json.Unmarshal([]byte(refs), &re.Refs)
			snap.RuleEvents = append(snap.RuleEvents, re)
		}
	}
	return snap, nil
}

func (s *Store) eventsUntil(ctx context.Context, projectID string, asOf int) ([]domain.Event, error) {
	q := `SELECT seq,id,project_id,at,type,payload,prev_hash,hash FROM events
	      WHERE project_id=?`
	args := []any{projectID}
	if asOf > 0 {
		q += ` AND seq<=?`
		args = append(args, asOf)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var ev domain.Event
		var pj string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.ProjectID, &ev.At, &ev.Type,
			&pj, &ev.PrevHash, &ev.Hash); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(pj)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) loadNodes(ctx context.Context, projectID string, ids map[string]bool) ([]domain.Node, error) {
	q := `SELECT payload FROM nodes WHERE project_id=?`
	var args []any
	args = append(args, projectID)
	if ids != nil {
		q += ` AND id IN (SELECT value FROM json_each(?))`
		idList := []string{}
		for id := range ids {
			idList = append(idList, id)
		}
		b, _ := json.Marshal(idList)
		args = append(args, string(b))
	}
	q += ` ORDER BY event_seq, kind, id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Node{}
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var n domain.Node
		if err := json.Unmarshal([]byte(pj), &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func setIDAtSeq(events []domain.Event, seq int) string {
	for _, ev := range events {
		if ev.Seq == seq && ev.Type == domain.EvDerived {
			var pl struct {
				SetID string `json:"set_id"`
			}
			_ = json.Unmarshal(ev.Payload, &pl)
			return pl.SetID
		}
	}
	return ""
}

func nodesIntroducedBy(events []domain.Event) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.Type != domain.EvDerived {
			continue
		}
		var pl struct {
			NodeIds []string `json:"node_ids"`
		}
		_ = json.Unmarshal(ev.Payload, &pl)
		for _, id := range pl.NodeIds {
			out[id] = true
		}
	}
	return out
}

func nodeIDsAtSeq(events []domain.Event, seq int) map[string]bool {
	for _, ev := range events {
		if ev.Seq == seq && ev.Type == domain.EvDerived {
			var pl struct {
				NodeIDs []string `json:"node_ids"`
			}
			_ = json.Unmarshal(ev.Payload, &pl)
			out := map[string]bool{}
			for _, id := range pl.NodeIDs {
				out[id] = true
			}
			return out
		}
	}
	return map[string]bool{}
}

func upsertConflict(cs []domain.Conflict, c domain.Conflict, ev domain.Event) []domain.Conflict {
	for i := range cs {
		if cs[i].ID == c.ID {
			if ev.Type == domain.EvResolved {
				cs[i].ResolvedBy = c.ResolvedBy
				cs[i].Winner = c.Winner
				cs[i].Reason = c.Reason
			}
			return cs
		}
	}
	return append(cs, c)
}

func sortNodes(nodes []NodeView) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j].EventSeq < nodes[j-1].EventSeq; j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
}

var _ = sql.ErrNoRows
