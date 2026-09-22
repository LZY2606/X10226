package domain

import "time"

// EventType enumerates the append-only project events.
type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvImportBatch    EventType = "import_batch"     // one submit, possibly partial
	EvParamsSet      EventType = "params_set"       // parameter revision
	EvDecision       EventType = "decision"         // accept / reject / adjust
	EvDecisionRevoke EventType = "decision_revoke"
	EvNote           EventType = "note"
)

// Event is one immutable append-only log entry. Seq is assigned by the store;
// IDs are content derived so export/import keeps identity.
type Event struct {
	ID      string          `json:"id"`
	Project string          `json:"project_id"`
	Seq     int64           `json:"seq"`
	Type    EventType       `json:"type"`
	At      time.Time       `json:"at"`
	Payload map[string]any  `json:"payload"`
}

// ImportItemResult reports what happened to one file inside an import batch.
type ImportItemResult struct {
	FileName   string       `json:"file_name"`
	Accepted   bool         `json:"accepted"`
	RecordID   string       `json:"record_id,omitempty"`
	Kind       LayerKind    `json:"kind,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Error      string       `json:"error,omitempty"` // fully rejected
}

// ImportBatchPayload: the batch keeps accepted layer ids plus per-file
// outcomes. A batch with some rejected files is an "import half failure":
// accepted parts stay, rejected parts are recorded.
type ImportBatchPayload struct {
	Accepted []string           `json:"accepted_layer_ids"`
	Results  []ImportItemResult `json:"results"`
}

type ParamsSetPayload struct {
	ParamsID string `json:"params_id"`
	Version  string `json:"version"`
	Parent   string `json:"parent_id,omitempty"`
	Note     string `json:"note,omitempty"`
}

type DecisionPayload struct {
	DecisionID string `json:"decision_id"`
	TargetKind string `json:"target_kind"` // stage_candidate | residue
	TargetID   string `json:"target_id"`
	Verdict    string `json:"verdict"` // accepted | rejected | adjusted
	// Adjustments (all optional): override candidate boundaries / residue.
	StartMin *float64 `json:"start_min,omitempty"`
	EndMin   *float64 `json:"end_min,omitempty"`
	Reason   string   `json:"reason"`
}

type DecisionRevokePayload struct {
	DecisionID string `json:"decision_id"`
	Reason     string `json:"reason"`
}

// Decision is the projected human verdict.
type Decision struct {
	ID         string    `json:"id"`
	EventSeq   int64     `json:"event_seq"`
	TargetKind string    `json:"target_kind"`
	TargetID   string    `json:"target_id"`
	Verdict    string    `json:"verdict"`
	StartMin   *float64  `json:"start_min,omitempty"`
	EndMin     *float64  `json:"end_min,omitempty"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
	Revoked    bool      `json:"revoked,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Finding codes emitted by the derivation pipeline (machine readable).
const (
	FindGasDelayReactionMismatch = "GAS_DELAY_REACTION_MISMATCH"
	FindTempLag                  = "TEMP_LAG_OFFSET"
	FindOverlappingCandidates    = "OVERLAPPING_PEAK_CANDIDATES"
	FindNonMonotonicSpikes       = "NONMONOTONIC_SPIKES_KEPT_RAW"
	FindGapNotInterpolated       = "GAP_NOT_INTERPOLATED"
	FindStageCrossesGap          = "STAGE_CROSSES_GAP_UNKNOWN"
	FindInitialMassRevised       = "INITIAL_MASS_REVISED"
	FindImportPartial            = "IMPORT_PARTIAL_FAILURE"
	FindUnconvertibleKept        = "UNCONVERTIBLE_VALUE_KEPT"
	FindStageSumNot100           = "STAGE_LOSS_SUM_NOT_100"
	FindStagesOverlap            = "STAGES_OVERLAP_ALLOWED"
	FindResidueUnknown           = "RESIDUE_UNKNOWN"
	FindDriftApplied             = "BASELINE_DRIFT_CORRECTED"
	FindAmbiguousDecomposition   = "AMBIGUOUS_DECOMPOSITION_ALL_CANDIDATES"
	FindOutOfOrderPoints         = "POINTS_OUT_OF_ORDER_KEPT_RAW"
)

// Finding is a first-class machine readable judgment / evidence item.
type Finding struct {
	Code     string            `json:"code"`
	Severity Severity          `json:"severity"`
	Message  string            `json:"message"`
	Refs     []string          `json:"refs,omitempty"`
	Data     map[string]float64 `json:"data,omitempty"`
}
