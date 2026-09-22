package model

const (
	EvtProjectCreated    = "project_created"
	EvtLayerImported     = "layer_imported"
	EvtParamsRevised     = "params_revised"
	EvtDerivedCommitted  = "derived_committed"
	EvtDecisionMade      = "decision_made"
	EvtConflictResolved  = "conflict_resolved"
	EvtJudgmentEmitted   = "judgment_emitted"
	EvtCounterRecorded   = "counterexample_recorded"
	EvtCounterVerdict    = "counterexample_verdict"
	EvtNote              = "note_added"
)

type Event struct {
	Seq       int64       `json:"seq"` // project-local sequence, dense
	UID       string      `json:"uid"` // evt_<hash(project,seq,...)>, stable across export/import
	Project   string      `json:"project"`
	Kind      string      `json:"kind"`
	At        string      `json:"at"` // RFC3339 UTC
	Parent    string      `json:"parent"` // previous event uid: append-only chain
	Note      string      `json:"note,omitempty"`
	Payload   any         `json:"payload"`
	// PayloadType lets the store decode JSON into the right concrete type.
	PayloadType string    `json:"payload_type"`
}

type ProjectInfo struct {
	UID         string `json:"uid"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type LayerPayload struct{ Layer LayerVersion `json:"layer"` }
type ParamsPayload struct{ Params ParamSet `json:"params"` }
type DerivedPayload struct {
	// Snapshot of artifacts produced by one recomputation. Artifacts whose
	// inputs did not change keep the same uid and are not repeated.
	Artifacts []Artifact `json:"artifacts"`
	// Inputs fingerprint covered by this commit, for replay diagnostics.
	InputFP   string     `json:"input_fp"`
}
type DecisionPayload struct{ Decision Decision `json:"decision"` }
type JudgmentPayload struct{ Judgments []Judgment `json:"judgments"` }
type CounterPayload struct{ Counter CounterExample `json:"counter"` }
type NotePayload struct{ Text string `json:"text"` }
