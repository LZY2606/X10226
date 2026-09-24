package domain

import "encoding/json"

// Event kinds.
const (
	EvProjectCreated  = "project_created"
	EvBatchImported   = "batch_imported"
	EvParamsRevised   = "params_revised"
	EvInitialMassSet  = "initial_mass_set"
	EvDecision        = "decision"
	EvDecisionUndone  = "decision_undone"
	EvConflictResolved = "conflict_resolved"
	EvCounterJudgment = "counter_judgment"
	EvNote            = "note"
)

// Raw document kinds imported as immutable layers.
const (
	DocSample      = "sample"
	DocPoints      = "points"
	DocDrift       = "drift_correction"
	DocProgram     = "furnace_program"
	DocGasEvent    = "gas_valve_event"
	DocBlank       = "blank_curve"
	DocInitialMass = "initial_mass"
)

// Event is the unit of the append-only log. Payload is kept as typed JSON so
// every version is replayable; parent chains link revisions to prior versions.
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Seq       int64           `json:"seq"`
	Kind      string          `json:"kind"`
	At        string          `json:"at"`
	Actor     string          `json:"actor"`
	ParentIDs []string        `json:"parent_ids,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type ProjectCreated struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CaseKey     string `json:"case_key,omitempty"`
	Seed        int64  `json:"seed,omitempty"`
}

// BatchImported carries the verbatim input fragments plus parser output.
// A half-failed import stores accepted items in Accepted and rejected rows in
// Rejected; AcceptedCount/RejectedCount make the status explicit.
type BatchImported struct {
	BatchID string     `json:"batch_id"`
	Kind    string     `json:"doc_kind"`
	Raw     string     `json:"raw"`
	Format  string     `json:"format"`
	Accepted []any     `json:"accepted,omitempty"`
	Rejected []Reject  `json:"rejected,omitempty"`
	// AffectedObjects is filled at replay time, not trusted from the event.
	AffectedObjects []ObjectRef `json:"affected_objects,omitempty"`
	ParserVersion   string      `json:"parser_version"`
}

type Reject struct {
	Index  int      `json:"index"`
	Raw    any      `json:"raw,omitempty"`
	Reason string   `json:"reason"`
	Path   string   `json:"path,omitempty"`
}

type ParamsRevised struct {
	ParamsVersion string                 `json:"params_version"`
	ParentVersion string                 `json:"parent_version,omitempty"`
	Changes       map[string]float64    `json:"changes"`
	Note          string                 `json:"note,omitempty"`
	Fingerprint   string                 `json:"fingerprint"`
}

type InitialMassSet struct {
	SampleID      string `json:"sample_id"`
	Version       string `json:"version"`
	ParentVersion string `json:"parent_version,omitempty"`
	Mass          Q      `json:"mass"`
	Reason        string `json:"reason"`
	Source        string `json:"source"` // initial_layer | revision
}

// A Decision is the human verdict over a candidate/residual: accept | reject |
// parameter_change | stage_adjust. Decisions are never overwritten; undo and
// conflict resolution create further events.
type Decision struct {
	DecisionID    string         `json:"decision_id"`
	Undone        bool           `json:"undone,omitempty"`
	TargetKind    string         `json:"target_kind"` // candidate | residual | assertion
	TargetID      string         `json:"target_id"`
	Verdict       string         `json:"verdict"` // accept | reject | adjust
	Reason        string         `json:"reason"`
	Adjustment    map[string]any `json:"adjustment,omitempty"`
	// Snapshot of the derived object identity the decision was made against,
	// so history renders the conclusion seen at the time.
	DerivedID     string `json:"derived_id"`
	AlgorithmVer  string `json:"algorithm_version"`
	ParamVersion  string `json:"param_version"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
}

type DecisionUndone struct {
	DecisionID string `json:"decision_id"`
	Reason     string `json:"reason"`
}

type ConflictResolved struct {
	ConflictID  string   `json:"conflict_id"`
	KeptID      string   `json:"kept_decision_id"`
	DroppedIDs  []string `json:"dropped_decision_ids"`
	Policy      string   `json:"policy"`
	Reason      string   `json:"reason"`
}

// CounterJudgment is the machine-readable record required by the counterexample
// archive: atmosphere delay / lag, overlapping non-100% stages, and gap
// no-crossing judgments all live here as一等数据.
type CounterJudgment struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"` // info | warning | violation
	Subject  string            `json:"subject"`
	Message  string            `json:"message"`
	Evidence map[string]any    `json:"evidence,omitempty"`
	Holds    *bool             `json:"holds,omitempty"`
}

type Note struct {
	Text string `json:"text"`
}

type ObjectRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
