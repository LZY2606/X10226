// Package domain defines the core, storage-independent entities of the
// thermogravimetric (TGA) review system.
//
// Layering rules enforced across the codebase:
//
//   - The import layer preserves the original textual representation of every
//     value together with its declared unit / coordinate basis. Normalisation
//     into SI happens only in the derivation layer.
//   - Raw inputs are append-only: a revision creates a new version that points
//     at its parent; a submitted source is never overwritten.
//   - Every derived value carries an algorithm version, a parameter fingerprint
//     and explicit direct sources.
//   - Candidates, rejection reasons, errors and unknown states are first-class
//     data. Stale derived results remain viewable but are never presented as
//     current.
package domain

import "time"

// Source kinds, one per layered evidence stream.
const (
	KindSamples      = "samples"      // raw TG sampling points (time/temp/mass)
	KindDrift        = "drift"        // balance drift correction description
	KindFurnace      = "furnace"      // furnace program segments
	KindGas          = "gas"          // gas valve events
	KindBlank        = "blank"        // blank / baseline curve
	KindInitialMass  = "initial_mass" // sample initial mass layer
	KindReplayScript = "replay"       // counter-example replay script
)

// Source submission status.
const (
	StatusCommitted = "committed" // fully parsed and accepted
	StatusPartial   = "partial"   // import half-failure: some rows rejected
	StatusRejected  = "rejected"  // whole submission rejected
)

// Event types written into the append-only history log.
const (
	EvProjectCreated    = "project.created"
	EvSourceCommitted   = "source.committed"
	EvSourcePartial     = "source.partial"
	EvSourceRejected    = "source.rejected"
	EvParamRevised      = "param.revised"
	EvRunCreated        = "run.created"
	EvDecisionMade      = "decision.made"
	EvDecisionRevoked   = "decision.revoked"
	EvConflictResolved  = "conflict.resolved"
	EvRuleAsserted      = "rule.asserted"
	EvStateRestored     = "state.restored"
	EvBundleExported    = "bundle.exported"
	EvBundleImported    = "bundle.imported"
)

// Decision verdicts.
const (
	VerdictAccept = "accept"
	VerdictReject = "reject"
	VerdictAdjust = "adjust" // parameter adjustment requested by a reviewer
)

// Candidate review states.
const (
	CandPending  = "pending"
	CandAccepted = "accepted"
	CandRejected = "rejected"
)

// Severity levels for diagnostics.
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// Quantity is a value in its ORIGINAL representation. Derived normalisation is
// kept separately; Raw is never rewritten.
type Quantity struct {
	Raw    string  `json:"raw"`             // original textual representation
	Value  float64 `json:"value,omitempty"` // parsed value in DeclaredUnit
	Unit   string  `json:"unit,omitempty"`  // declared engineering unit
	Valid  bool    `json:"valid"`           // false => unconvertible, stays in place
	Reason string  `json:"reason,omitempty"`
}

// Coordinate carries a value plus the coordinate basis it is meaningful in
// (e.g. time vs furnace time, Celsius vs Kelvin).
type Coordinate struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string `json:"basis"` // e.g. "elapsed_s", "furnace_min", "celsius"
}

// Project is the review unit.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Seed      int64     `json:"seed"`
}

// Source is one versioned, append-only evidence layer.
type Source struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Kind           string    `json:"kind"`
	Version        int       `json:"version"`
	ParentSourceID string    `json:"parent_source_id,omitempty"`
	Label          string    `json:"label"`
	RawName        string    `json:"raw_name"`
	RawText        string    `json:"raw_text"`
	RawEncoding    string    `json:"raw_encoding"`
	Checksum       string    `json:"checksum"`
	Status         string    `json:"status"`
	RejectedRows   int       `json:"rejected_rows"`
	AcceptedRows   int       `json:"accepted_rows"`
	CreatedAt      time.Time `json:"created_at"`
	Seq            int64     `json:"seq"` // monotonic submission order
}

// SourceRow preserves one original record verbatim plus its (possibly failed)
// parse. No row is ever silently dropped to make plotting possible.
type SourceRow struct {
	ID         int64  `json:"-"`
	SourceID   string `json:"source_id"`
	RowIndex   int    `json:"row_index"`
	RawLine    string `json:"raw_line"`
	Fields     string `json:"fields,omitempty"` // JSON map slot->Quantity
	ParseOK    bool   `json:"parse_ok"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Diagnostic is an import/derivation diagnostic attached to an object.
type Diagnostic struct {
	ID        int64  `json:"-"`
	ProjectID string `json:"project_id"`
	SourceID  string `json:"source_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	RowIndex  int    `json:"row_index,omitempty"`
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RawRef    string `json:"raw_ref,omitempty"`
}

// Event is one immutable history entry (event sourcing log).
type Event struct {
	Seq       int64             `json:"seq"`
	ProjectID string            `json:"project_id"`
	Type      string            `json:"type"`
	At        time.Time         `json:"at"`
	Actor     string            `json:"actor"`
	SubjectID string            `json:"subject_id,omitempty"`
	Payload   map[string]string `json:"payload,omitempty"`
}

// Parameters is a versioned review configuration.
type Parameters struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Version        int       `json:"version"`
	ParentID       string    `json:"parent_id,omitempty"`
	Fingerprint    string    `json:"fingerprint"`
	AlgorithmVer   string    `json:"algorithm_version"`
	AtmosDelayS    float64   `json:"atmos_delay_s"`
	ThermalLagS    float64   `json:"thermal_lag_s"`
	PeakProminence float64   `json:"dtg_peak_prominence_pct_per_min"`
	PeakMinSepS    float64   `json:"dtg_peak_min_sep_s"`
	SmoothWindowS  float64   `json:"smooth_window_s"`
	MergeOverlap   bool      `json:"allow_overlap"`
	GapMaxS        float64   `json:"gap_max_interp_s"`
	CreatedAt      time.Time `json:"created_at"`
	Note           string    `json:"note,omitempty"`
}

// SourceRef names a direct provenance edge.
type SourceRef struct {
	Kind       string `json:"kind"`
	SourceID   string `json:"source_id"`
	Version    int    `json:"version"`
	RowIndexes []int  `json:"row_indexes,omitempty"`
}

// Run is a derivation run whose inputs are pinned by identity.
type Run struct {
	ID           string      `json:"id"`
	ProjectID    string      `json:"project"`
	Seq          int64       `json:"seq"`
	AlgorithmVer string      `json:"algorithm_version"`
	ParamID      string      `json:"param_id"`
	ParamFP      string      `json:"param_fingerprint"`
	SourceSet    string      `json:"source_set"` // checksum of sorted source ids+versions
	Sources      []SourceRef `json:"sources"`
	Current      bool        `json:"current"` // latest valid run for current inputs
	SupersededBy string      `json:"superseded_by,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	Note         string      `json:"note,omitempty"`
}

// StageCandidate is an explicit (possibly rejected / unknown) decomposition.
type StageCandidate struct {
	ID            string    `json:"id"`
	RunID         string    `json:"run_id"`
	Index         int       `json:"index"`
	StartS        float64   `json:"start_s"`
	EndS          float64   `json:"end_s"`
	PeakS         float64   `json:"peak_s"`
	PeakDTG       float64   `json:"peak_dtg_pct_per_min"`
	MassLossPct   float64   `json:"mass_loss_pct"`
	MassLossErr   float64   `json:"mass_loss_err_pct"`
	OverlapsWith  []int     `json:"overlaps_with,omitempty"`
	ForcedSum     bool      `json:"forced_sum_100"`
	State         string    `json:"state"`
	RejectedReason  string  `json:"rejected_reason,omitempty"`
	Unknown         bool    `json:"unknown,omitempty"`
	UnknownReason   string  `json:"unknown_reason,omitempty"`
	AtmosReactionS float64 `json:"atmos_reaction_s"`
	ValveS          float64 `json:"valve_s,omitempty"`
	Sources       []SourceRef `json:"sources,omitempty"`
}

// ResidueResult is a final residue rate with a full expandable chain.
type ResidueResult struct {
	ID          string            `json:"id"`
	RunID       string            `json:"run_id"`
	AtS         float64           `json:"at_s"`
	Percent     float64           `json:"residue_pct"`
	ErrPct      float64           `json:"err_pct"`
	Unknown     bool              `json:"unknown"`
	UnknownReason string          `json:"unknown_reason,omitempty"`
	Chain       []ChainStep       `json:"chain"`
	Sources     []SourceRef       `json:"sources"`
}

// ChainStep is one node in an expandable calculation chain.
type ChainStep struct {
	Order   int      `json:"order"`
	Op      string   `json:"op"`
	Detail  string   `json:"detail"`
	Value   string   `json:"value,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Basis   string   `json:"basis,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// RuleEvidence is a machine-readable justification event (persisted + exported).
type RuleEvidence struct {
	Code      string    `json:"code"`
	Statement string    `json:"statement"`
	SubjectID string    `json:"subject_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Data      string    `json:"data,omitempty"` // JSON payload
	CreatedAt time.Time `json:"created_at"`
}

// Decision records a human verdict; conflicts and revocations are history too.
type Decision struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	RunID       string    `json:"run_id"`
	TargetKind  string    `json:"target_kind"` // stage | residue
	TargetID    string    `json:"target_id"`
	Verdict     string    `json:"verdict"`
	Note        string    `json:"note"`
	ParamID     string    `json:"param_id,omitempty"`
	Revoked     bool      `json:"revoked"`
	RevokesID   string    `json:"revokes_id,omitempty"`
	ConflictOf  string    `json:"conflict_of,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Seq         int64     `json:"seq"`
}

// NormalisedPoint is a derived SI point used only for plotting/analysis.
type NormalisedPoint struct {
	T                 float64 `json:"t_s"`
	TempC             float64 `json:"temp_c"`
	MassG             float64 `json:"mass_g"`
	CorrectedMassG    float64 `json:"corrected_mass_g"`
	MassPct           float64 `json:"mass_pct"`
	DTGPctPerMin      float64 `json:"dtg_pct_per_min"`
	Gap               bool    `json:"gap"`
	NonMonotonic      bool    `json:"non_monotonic"`
	BlankSubtracted   bool    `json:"blank_subtracted"`
	SourceRow         int     `json:"source_row"`
}
