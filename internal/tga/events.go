package tga

import "time"

// EventKind enumerates the append-only log entries.
type EventKind string

const (
	EvProjectCreated   EventKind = "project_created"
	EvRawCommitted     EventKind = "raw_committed"
	EvParamsRevised    EventKind = "params_revised"
	EvDecision         EventKind = "decision"
	EvRuleBasis        EventKind = "rule_basis"
	EvUndo             EventKind = "undo"
)

// Event is one immutable append-log entry. Payload is one of the *Payload
// structs below, round-tripped as raw JSON.
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Seq       int             `json:"seq"`
	Kind      EventKind       `json:"kind"`
	Payload   JSON            `json:"payload"`
	At        time.Time       `json:"at"`
	Undone    bool            `json:"undone,omitempty"`
	UndoneBy  string          `json:"undone_by,omitempty"`
}

// JSON is an encoded payload fragment.
type JSON []byte

func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}
func (j *JSON) UnmarshalJSON(b []byte) error { *j = append((*j)[:0], b...); return nil }

type ProjectCreatedPayload struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// RawCommittedPayload records one submitted chunk. When a section half-fails,
// Accepted=false and the raw text plus diagnostics are still retained; it is
// never promoted to the normalised layer.
type RawCommittedPayload struct {
	CommitID  string       `json:"commit_id"`
	Section   Section      `json:"section"`
	Parent    string       `json:"parent,omitempty"`
	Format    string       `json:"format"`
	RawText   string       `json:"raw_text"`
	Accepted  bool         `json:"accepted"`
	Label     string       `json:"label"`
	Diags     []Diagnostic `json:"diags"`
	InputClip string       `json:"input_clip"` // the affected input fragment
}

type ParamsRevisedPayload struct {
	Params  Params `json:"params"`
	Reason  string `json:"reason"`
}

// DecisionPayload records an accept / reject / parameter adjustment made by a
// reviewer against a specific candidate or residue. Conflicts resolve with
// explicit resolution semantics; everything lands in history.
type DecisionPayload struct {
	DecisionID string `json:"decision_id"`
	Subject    string `json:"subject"` // candidate id | residue | params
	Action     string `json:"action"`  // accept | reject | adjust
	Reason     string `json:"reason"`
	Reviewer   string `json:"reviewer"`
	// Adjust carries an optional parameter adjustment snapshot (only for
	// action=adjust on params subject).
	Adjust *Params `json:"adjust,omitempty"`
	// ConflictFrom names the prior decision this one overrides, if any.
	ConflictFrom string `json:"conflict_from,omitempty"`
	// ConflictResolution describes how the conflict was settled.
	ConflictResolution string `json:"conflict_resolution,omitempty"`
	// CandidateKey is the logical key (cluster+variant) of the subject, so
	// candidates remain addressable across re-derivations.
	CandidateKey string `json:"candidate_key,omitempty"`
}

// RuleBasisPayload is a machine-readable statement of the judgement basis for a
// situation (atmosphere delay, drift, overlap, noise, gap, initial-mass
// revision, half-failure). The review UI and external tooling read these.
type RuleBasisPayload struct {
	BasisID   string         `json:"basis_id"`
	Code      string         `json:"code"`
	Statement string         `json:"statement"`
	Evidence  map[string]any `json:"evidence"`
	RelatesTo []string       `json:"relates_to"` // artifact/commit/decision ids
}

type UndoPayload struct {
	TargetEventID string `json:"target_event_id"`
	Reason        string `json:"reason"`
}
