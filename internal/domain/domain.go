// Package domain defines the immutable core types of the TGA review system:
// raw layered inputs, derived artifacts, and the append-only audit event log.
package domain

import "time"

// Layers are the six separately-versioned raw input layers. Every layer is
// stored verbatim; normalization happens only on the derived side.
const (
	LayerSamples = "samples" // raw (time, temperature, mass) sampling points
	LayerBalance = "balance" // balance drift / baseline correction records
	LayerProgram = "program" // furnace program segments
	LayerGas     = "gas"     // gas valve switch events
	LayerBlank   = "blank"   // blank (empty-pan) curve
	LayerInitial = "initial" // sample initial mass declarations / revisions
)

// Layers lists all raw layers in canonical order.
var Layers = []string{LayerSamples, LayerBalance, LayerProgram, LayerGas, LayerBlank, LayerInitial}

// Canonical units used after normalization. The original textual representation
// is always retained alongside the canonical value.
const (
	UnitMg      = "mg"
	UnitCelsius = "degC"
	UnitKelvin  = "K"
	UnitSecond  = "s"
	UnitMinute  = "min"
	UnitPercent = "%"
	UnitMgMin   = "mg/min"
)

// Q is a quantified value carrying its unit and the original textual
// representation. Values that cannot be converted keep Canonical == nil and
// carry a Diagnostic; they are never dropped to keep the picture pretty.
type Q struct {
	Canonical  *float64 `json:"canonical,omitempty"`
	Unit       string   `json:"unit,omitempty"` // canonical unit ("" when unconvertible)
	Original   string   `json:"original"`       // representation exactly as imported
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// Coord is a time coordinate. Seconds is the canonical furnace-time axis; the
// original text and its unit survive for provenance.
type Coord struct {
	Seconds    *float64 `json:"seconds,omitempty"`
	Original   string   `json:"original"`
	Unit       string   `json:"unit,omitempty"`
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// RawItem is one row of one raw layer. Inputs are immutable once committed:
// supersession is expressed by a new batch with Supersedes, never by mutation.
type RawItem struct {
	ID         string            `json:"id"`
	BatchID    string            `json:"batch_id"`
	ProjectID  string            `json:"project_id"`
	Layer      string            `json:"layer"`
	LineNo     int               `json:"line_no"`
	Fields     map[string]string `json:"fields"`
	Status     string            `json:"status"`
	Diagnostic string            `json:"diagnostic,omitempty"`
}

// Item statuses.
const (
	ItemOK      = "ok"
	ItemInvalid = "invalid"
)

// RawBatch groups one committed submission of one layer.
type RawBatch struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Layer          string    `json:"layer"`
	Payload        string    `json:"payload"`
	Supersedes     string    `json:"supersedes,omitempty"`
	ImportSeq      int       `json:"import_seq"`
	CommittedAt    time.Time `json:"committed_at"`
	PartialFailure string    `json:"partial_failure,omitempty"`
}

// ArtifactKind enumerates derived result families.
const (
	ArtNormalized  = "normalized_series"
	ArtTimeline    = "atmosphere_timeline"
	ArtProgramView = "furnace_program"
	ArtPeaks       = "dtg_peaks"
	ArtStages      = "stage_candidates"
	ArtResidue     = "residue"
)

// Artifact is a versioned derived result recording algorithm version, parameter
// fingerprint and the exact direct sources used to build it.
type Artifact struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	Kind         string         `json:"kind"`
	Subject      string         `json:"subject,omitempty"`
	AlgoVersion  string         `json:"algo_version"`
	ParamFP      string         `json:"param_fp"`
	Sources      []SourceRef    `json:"sources"`
	Status       string         `json:"status"`
	CreatedEvent string         `json:"created_event"`
	SupersededBy string         `json:"superseded_by,omitempty"`
	Payload      map[string]any `json:"payload"`
}

// SourceRef points at one direct source of a derived artifact.
type SourceRef struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Layer    string `json:"layer,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// Event kinds of the append-only log.
const (
	EvProjectCreated = "project_created"
	EvImport         = "raw_import"
	EvParams         = "params_revised"
	EvCompute        = "derived_computed"
	EvDecision       = "manual_decision"
	EvConflict       = "conflict_resolved"
	EvRevert         = "revert"
	EvAnnotate       = "judgment"
)

// Event is one immutable log entry. PrevHash chains the log.
type Event struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Seq        int64          `json:"seq"`
	Kind       string         `json:"kind"`
	At         time.Time      `json:"at"`
	Actor      string         `json:"actor"`
	Payload    map[string]any `json:"payload"`
	PrevHash   string         `json:"prev_hash"`
	Hash       string         `json:"hash"`
	RevertedBy string         `json:"reverted_by,omitempty"`
}

// Params is a versioned review-parameter set.
type Params struct {
	Version     int               `json:"version"`
	Values      map[string]string `json:"values"`
	Fingerprint string            `json:"fingerprint"`
	EventID     string            `json:"event_id"`
	At          time.Time         `json:"at"`
}

// Decision records an accept/reject/adjust verdict on a candidate.
type Decision struct {
	ID           string         `json:"id"`
	TargetKind   string         `json:"target_kind"`
	TargetID     string         `json:"target_id"`
	Verdict      string         `json:"verdict"`
	Reason       string         `json:"reason"`
	Adjustments  map[string]any `json:"adjustments,omitempty"`
	ArtifactID   string         `json:"artifact_id"`
	ConflictKey  string         `json:"conflict_key,omitempty"`
	SupersededBy string         `json:"superseded_by,omitempty"`
}

// Judgment codes emitted as machine-readable annotation events.
const (
	JValveNotReaction = "VALVE_TIME_NEQ_REACTION_TIME"
	JStagesMayOverlap = "STAGES_MAY_OVERLAP"
	JNoForcedTotal    = "STAGE_LOSSES_NE_NOT_SUM_100"
	JNoGapInterp      = "GAP_NOT_BRIDGED_BY_INTERPOLATION"
	JMultiCandidates  = "NONUNIQUE_KEEP_ALL_VALID_CANDIDATES"
	JDriftCorrected   = "BASELINE_DRIFT_CORRECTED"
	JInitialRevised   = "INITIAL_MASS_REVISED_RESIDUE_REBASED"
	JPartialImport    = "IMPORT_PARTIAL_FAILURE_BLAST_RADIUS"
	JOverlapPeaks     = "OVERLAPPING_PEAKS_NOT_MERGED"
	JNonMonotonic     = "NONMONOTONIC_NOISE_FLAGGED"
)
