// Package events implements the append-only event log. Raw inputs are never
// overwritten: revisions are expressed as new events with a parent link.
// Undo/conflict resolution are themselves events, and replaying to any old
// event restores the dependency versions and visible conclusions of that time.
package events

import (
	"encoding/json"

	"tga-review/internal/domain"
)

type Kind string

const (
	KindBatchImported Kind = "batch_imported" // 导入（含半失败）
	KindParamsRevised Kind = "params_revised" // 参数修订（版本化）
	KindDecision      Kind = "decision"       // 人工决定：接受/否定/调整
	KindConflict      Kind = "conflict"       // 冲突解决（撤销旧决定）
	KindUndo          Kind = "undo"           // 撤销任意历史事件
)

// Event is an immutable log entry.
type Event struct {
	Seq        int64           `json:"seq"`
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	ParentID   string          `json:"parent_id"`
	Kind       Kind            `json:"kind"`
	OccurredAt string          `json:"occurred_at"`
	Actor      string          `json:"actor"`
	Summary    string          `json:"summary"`
	UndoneOf   string          `json:"undone_of,omitempty"` // for undo events
	Payload    json.RawMessage `json:"payload"`
}

// Payload types -------------------------------------------------------------

type BatchPayload struct {
	BatchID string   `json:"batch_id"`
	Records []string `json:"records"`
}

type ParamsPayload struct {
	Revision       int               `json:"revision"`
	SupersedesRev  int               `json:"supersedes_revision"`
	Parameters     map[string]domain.Quantity `json:"parameters"`
	Reason         string            `json:"reason,omitempty"`
	Fingerprint    string            `json:"fingerprint"`
}

type DecisionPayload struct {
	TargetID      string `json:"target_id"`   // candidate/residue/derived id
	TargetKind    string `json:"target_kind"`
	Verdict       string `json:"verdict"`     // accepted | rejected | adjusted
	Notes         string `json:"notes,omitempty"`
	NewParams     map[string]domain.Quantity `json:"new_params,omitempty"`
	ParameterRev  int    `json:"parameter_revision"`
	ConflictsWith string `json:"conflicts_with,omitempty"` // prior decision id
}

type ConflictPayload struct {
	KeptDecisionID    string `json:"kept_decision_id"`
	DroppedDecisionID string `json:"dropped_decision_id"`
	Reason            string `json:"reason"`
}

type UndoPayload struct {
	UndoneEventID string `json:"undone_event_id"`
	UndoneSeq     int64  `json:"undone_seq"`
	Reason        string `json:"reason"`
}

// State is a replay projection.
type State struct {
	ProjectID   string
	Records     []domain.RawRecord
	Batches     []domain.Batch
	Params      []ParameterRevision
	Decisions   map[string]Decision // keyed by target id
	DecisionLog []Decision
	Conflicts   []ConflictRecord
	Undone      map[int64]bool // undone event seqs
	LiveAt      int64          // highest replayed seq
}

type ParameterRevision struct {
	Revision    int
	EventSeq    int64
	Parameters  map[string]domain.Quantity
	Fingerprint string
	Reason      string
}

type Decision struct {
	EventSeq      int64
	EventID       string
	TargetID      string
	TargetKind    string
	Verdict       string
	Notes         string
	ParameterRev  int
	Undone        bool
	ConflictsWith string
}

type ConflictRecord struct {
	EventSeq          int64
	KeptDecisionID    string
	DroppedDecisionID string
	Reason            string
}

func NewState(projectID string) *State {
	return &State{ProjectID: projectID, Decisions: map[string]Decision{}, Undone: map[int64]bool{}}
}

// Apply folds one event into the state.
func (s *State) Apply(e Event) error {
	if s.Undone[e.Seq] {
		// already marked undone by a later undo before reaching it (not
		// possible in sequential replay, but kept defensive)
	}
	switch e.Kind {
	case KindBatchImported:
		var p BatchPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		// records are attached by the store layer when projecting; Apply
		// only records the batch id; ApplyRecord adds the record.
	case KindParamsRevised:
		var p ParamsPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Params = append(s.Params, ParameterRevision{
			Revision: p.Revision, EventSeq: e.Seq,
			Parameters: p.Parameters, Fingerprint: p.Fingerprint, Reason: p.Reason,
		})
	case KindDecision:
		var p DecisionPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		d := Decision{
			EventSeq: e.Seq, EventID: e.ID, TargetID: p.TargetID,
			TargetKind: p.TargetKind, Verdict: p.Verdict, Notes: p.Notes,
			ParameterRev: p.ParameterRev, ConflictsWith: p.ConflictsWith,
		}
		s.Decisions[p.TargetID] = d
		s.DecisionLog = append(s.DecisionLog, d)
	case KindConflict:
		var p ConflictPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Conflicts = append(s.Conflicts, ConflictRecord{
			EventSeq: e.Seq, KeptDecisionID: p.KeptDecisionID,
			DroppedDecisionID: p.DroppedDecisionID, Reason: p.Reason,
		})
		// mark the dropped decision undone in projection
		for i := range s.DecisionLog {
			if s.DecisionLog[i].EventID == p.DroppedDecisionID {
				s.DecisionLog[i].Undone = true
			}
		}
		if d, ok := s.Decisions[decisionKeyByID(s, p.DroppedDecisionID)]; ok && d.EventID == p.DroppedDecisionID {
			d.Undone = true
			s.Decisions[d.TargetID] = d
		}
	case KindUndo:
		var p UndoPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Undone[p.UndoneSeq] = true
		// reverse the effect of the undone event
		switch {
		default:
			s.retract(p.UndoneSeq)
		}
	}
	s.LiveAt = e.Seq
	return nil
}

func decisionKeyByID(s *State, eventID string) string {
	for _, d := range s.DecisionLog {
		if d.EventID == eventID {
			return d.TargetID
		}
	}
	return ""
}

// retract removes the projection effect of event seq.
func (s *State) retract(seq int64) {
	var keptParams []ParameterRevision
	for _, pr := range s.Params {
		if pr.EventSeq != seq {
			keptParams = append(keptParams, pr)
		}
	}
	s.Params = keptParams

	var keptLog []Decision
	for _, d := range s.DecisionLog {
		if d.EventSeq != seq {
			keptLog = append(keptLog, d)
		}
	}
	s.DecisionLog = keptLog
	for k, d := range s.Decisions {
		if d.EventSeq == seq {
			delete(s.Decisions, k)
		}
	}
	var keptConf []ConflictRecord
	for _, c := range s.Conflicts {
		if c.EventSeq != seq {
			keptConf = append(keptConf, c)
		}
	}
	s.Conflicts = keptConf
	// batch retraction handled by AttachRecords filtering in the store layer.
}

// CurrentParams returns the live parameter revision (0 = built-in defaults).
func (s *State) CurrentParams() ParameterRevision {
	if len(s.Params) == 0 {
		return ParameterRevision{Revision: 0, Parameters: map[string]domain.Quantity{}}
	}
	return s.Params[len(s.Params)-1]
}
