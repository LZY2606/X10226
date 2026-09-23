package replay

import (
	"encoding/json"

	"tga-review/internal/domain"
)

// Payload types carried inside events.

type ProjectCreatedPayload struct {
	Name string `json:"name"`
}

type SourceCommitPayload struct {
	Kind      string          `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"`
	SourceID  string          `json:"source_id"`
	Payload   json.RawMessage `json:"payload"`
}

type ImportPayload struct {
	Report ImportReport `json:"report"`
}

type DecisionPayload struct {
	TargetNode string             `json:"target_node"`
	Verdict    string             `json:"verdict"`
	Params     map[string]float64 `json:"params,omitempty"`
	Rationale  string             `json:"rationale,omitempty"`
	Node       *domain.Node       `json:"node,omitempty"` // emitted adjusted node
}

type ConflictPayload struct {
	Key      string   `json:"key"`
	EventIDs []string `json:"event_ids"`
	NodeKeys []string `json:"node_keys"`
	Message  string   `json:"message"`
}

type ResolutionPayload struct {
	ConflictKey string `json:"conflict_key"`
	Choice      string `json:"choice"` // accept_first | accept_second | override | reopen
	Note        string `json:"note,omitempty"`
}

type UndoPayload struct {
	TargetEvent string `json:"target_event"`
	Reason      string `json:"reason,omitempty"`
}

type AdjustedNodePayload struct {
	Node domain.Node `json:"node"`
}

// Replay projects events [0..asOf] into a State. asOf<=0 means the full stream.
func Replay(projectID string, events []Event, asOf int64) *State {
	st := &State{
		ProjectID:  projectID,
		Sources:    map[string]*domain.Source{},
		Nodes:      map[string]*domain.Node{},
		CurrentIDs: map[string]bool{},
	}
	activeDecisions := map[string]domain.Decision{} // keyed by event id
	keyDecisions := map[string][]domain.Decision{}  // by node stable key
	var conflictByKey = map[string]ConflictView{}
	var conflictOrder []string

	for _, ev := range events {
		if asOf > 0 && ev.Seq > asOf {
			break
		}
		switch ev.Type {
		case EvProjectCreated:
			var p ProjectCreatedPayload
			_ = json.Unmarshal(ev.Payload, &p)
			st.Name = p.Name
			st.CreatedTS = ev.VirtualTS
		case EvSourceCommit:
			var p SourceCommitPayload
			_ = json.Unmarshal(ev.Payload, &p)
			src := &domain.Source{
				ID: p.SourceID, ProjectID: projectID, Kind: p.Kind,
				Version: p.Version, ParentID: p.ParentID,
				BornSeq: ev.Seq, VirtualTS: ev.VirtualTS, Payload: p.Payload,
			}
			st.Sources[p.Kind] = src
			st.AllSources = append(st.AllSources, src)
		case EvImport:
			var p ImportPayload
			_ = json.Unmarshal(ev.Payload, &p)
			r := p.Report
			r.Seq = ev.Seq
			r.EventID = ev.ID
			r.VirtualTS = ev.VirtualTS
			st.ImportBatches = append(st.ImportBatches, r)
		case EvDecision:
			if ev.Undone {
				// handled by EvUndo marking; skip decisions whose undo is within range
			}
			if !ev.Undone {
				d := buildDecision(ev)
				activeDecisions[ev.ID] = d
				st.Decisions = appendActive(st.Decisions, d)
				keyDecisions[d.TargetNode] = append(keyDecisions[d.TargetNode], d)
				var dp DecisionPayload
				_ = json.Unmarshal(ev.Payload, &dp)
				if dp.Node != nil {
					nd := *dp.Node
					nd.BornSeq = ev.Seq
					st.Nodes[nd.ID] = &nd
					st.NodeOrder = append(st.NodeOrder, nd.ID)
				}
			}
		case EvConflict:
			var p ConflictPayload
			_ = json.Unmarshal(ev.Payload, &p)
			cv := ConflictView{Key: p.Key, EventIDs: p.EventIDs, NodeKeys: p.NodeKeys,
				Message: p.Message, Open: true, Seq: ev.Seq}
			conflictByKey[p.Key] = cv
			conflictOrder = append(conflictOrder, p.Key)
		case EvResolution:
			var p ResolutionPayload
			_ = json.Unmarshal(ev.Payload, &p)
			if cv, ok := conflictByKey[p.ConflictKey]; ok {
				cv.Open = false
				cv.Resolution = p.Choice
				cv.ResolvedBy = ev.ID
				conflictByKey[p.ConflictKey] = cv
			}
		case EvUndo:
			var p UndoPayload
			_ = json.Unmarshal(ev.Payload, &p)
			applyUndo(st, activeDecisions, p.TargetEvent, ev)
		}
	}

	for _, k := range conflictOrder {
		st.Conflicts = append(st.Conflicts, conflictByKey[k])
	}

	// deterministic re-derivation from the current source set
	res := domain.Compute(projectID, domain.Sources{
		Sample:   st.Sources[domain.KindSamplePoints],
		Drift:    st.Sources[domain.KindDriftCorrection],
		Furnace:  st.Sources[domain.KindFurnaceProgram],
		Gas:      st.Sources[domain.KindGasValve],
		Blank:    st.Sources[domain.KindBlankCurve],
		InitMass: st.Sources[domain.KindInitialMass],
	})
	for i := range res.Judgments {
		res.Judgments[i].BornSeq = st.lastSeq()
	}
	st.Judgments = res.Judgments
	for _, n := range res.Nodes {
		nd := n
		if _, exists := st.Nodes[nd.ID]; !exists {
			st.Nodes[nd.ID] = &nd
			st.NodeOrder = append(st.NodeOrder, nd.ID)
		}
		st.CurrentIDs[nd.ID] = true
	}
	st.AsOfSeq = asOf
	return st
}

func (s *State) lastSeq() int64 {
	m := int64(0)
	for _, src := range s.AllSources {
		if src.BornSeq > m {
			m = src.BornSeq
		}
	}
	return m
}

func buildDecision(ev Event) domain.Decision {
	var dp DecisionPayload
	_ = json.Unmarshal(ev.Payload, &dp)
	return domain.Decision{
		EventID: ev.ID, Seq: ev.Seq, Actor: ev.Actor,
		Verdict: dp.Verdict, TargetNode: dp.TargetNode,
		Params: dp.Params, Rationale: dp.Rationale, VirtualTS: ev.VirtualTS,
	}
}

func appendActive(ds []domain.Decision, d domain.Decision) []domain.Decision {
	out := ds[:0:0]
	for _, x := range ds {
		if x.EventID != d.EventID {
			out = append(out, x)
		}
	}
	return append(out, d)
}

func applyUndo(st *State, active map[string]domain.Decision, targetID string, undoEv Event) {
	if d, ok := active[targetID]; ok {
		d.Undone = true
		d.UndoneBy = undoEv.ID
		// replace in decisions slice
		for i := range st.Decisions {
			if st.Decisions[i].EventID == targetID {
				st.Decisions[i] = d
			}
		}
		delete(active, targetID)
	}
}

// EvaluateConflicts checks the current decisions and returns conflict events to
// append (without persisting): same candidate accepted+rejected, or two
// overlapping dtg candidates both accepted without an overlap acknowledgement.
func EvaluateConflicts(st *State) []ConflictPayload {
	var out []ConflictPayload
	byNode := map[string][]domain.Decision{}
	var active []domain.Decision
	for _, d := range st.Decisions {
		if !d.Undone {
			active = append(active, d)
		}
	}
	for _, d := range active {
		byNode[d.TargetNode] = append(byNode[d.TargetNode], d)
	}
	for node, ds := range byNode {
		var acc, rej bool
		var ids []string
		for _, d := range ds {
			switch d.Verdict {
			case "accepted":
				acc = true
			case "rejected":
				rej = true
			}
			ids = append(ids, d.EventID)
		}
		if acc && rej {
			out = append(out, ConflictPayload{
				Key: "verdict:" + node, EventIDs: ids, NodeKeys: []string{node},
				Message: "same candidate was both accepted and rejected",
			})
		}
	}
	// overlapping accepted dtg stages
	acceptedStages := map[string]domain.Decision{}
	for _, d := range active {
		if d.Verdict != "accepted" {
			continue
		}
		if n, ok := st.Nodes[d.TargetNode]; ok && n.Kind == domain.NodeStage {
			acceptedStages[d.TargetNode] = d
		}
	}
	seen := map[string]bool{}
	for idA, dA := range acceptedStages {
		nA := st.Nodes[idA]
		var cA domain.StageCandidatePayload
		_ = json.Unmarshal(nA.Payload, &cA)
		overlapAck, _ := dA.Params["acknowledge_overlap"]
		for idB := range acceptedStages {
			if idA == idB || seen[idA+"|"+idB] || seen[idB+"|"+idA] {
				continue
			}
			nB := st.Nodes[idB]
			var cB domain.StageCandidatePayload
			_ = json.Unmarshal(nB.Payload, &cB)
			if cA.StartS < cB.EndS && cB.StartS < cA.EndS && overlapAck != 1 {
				out = append(out, ConflictPayload{
					Key: "overlap:" + minKey(idA, idB),
					EventIDs: []string{dA.EventID, acceptedStages[idB].EventID},
					NodeKeys: []string{idA, idB},
					Message:  "overlapping stages both accepted without acknowledgement; " +
						"stage losses are not forced to sum to 100%",
				})
				seen[idA+"|"+idB] = true
			}
		}
	}
	return out
}

func minKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}
