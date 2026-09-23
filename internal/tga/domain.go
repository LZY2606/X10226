package tga

// Domain model for the TGA stage-decomposition / atmosphere-event review system.
//
// Layering rule: raw imports are never overwritten; every later revision is a
// new record version with a parent. Derived values carry algorithm version,
// parameter fingerprint and direct sources.

// RawValue preserves the original textual representation. Normalized is zero
// with a non-empty Diagnostic when conversion failed.
type RawValue struct {
	Raw        string  `json:"raw"`
	Normalized float64 `json:"normalized,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

func (v RawValue) OK() bool { return v.Diagnostic == "" }

type Diagnostic struct {
	Scope   string `json:"scope"`
	Message string `json:"message"`
	Row     int    `json:"row,omitempty"`
	Field   string `json:"field,omitempty"`
}

// Record kinds (each kind forms its own version lineage).
const (
	KindPoints  = "points"  // raw sample sampling points
	KindBlank   = "blank"   // blank/baseline curve
	KindFurnace = "furnace" // furnace program
	KindGas     = "gas"     // gas valve events
	KindDrift   = "drift"   // balance drift correction
	KindMass0   = "mass0"   // sample initial mass (layered weighing)
)

type Record struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"project_id"`
	Kind         string       `json:"kind"`
	Name         string       `json:"name"`
	Version      int          `json:"version"`
	ParentID     string       `json:"parent_id,omitempty"`
	ImportID     string       `json:"import_id"`
	CreatedAt    string       `json:"created_at"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
	Payload      any          `json:"payload"`
	ContentHash  string       `json:"content_hash"`
	RawBytesHash string       `json:"raw_bytes_hash"`
}

// ---- payloads (raw layer; values keep original representation) ----

type RawPoint struct {
	N    int      `json:"n"`
	Time RawValue `json:"t"`
	Mass RawValue `json:"m"`
	Temp RawValue `json:"temp,omitempty"`
}

type PointsPayload struct {
	TimeUnit string     `json:"time_unit"`
	MassUnit string     `json:"mass_unit"`
	TempUnit string     `json:"temp_unit,omitempty"`
	Rows     []RawPoint `json:"rows"`
}

type BlankPayload struct {
	TimeUnit string     `json:"time_unit"`
	MassUnit string     `json:"mass_unit"`
	Rows     []RawPoint `json:"rows"`
}

type FurnaceSegment struct {
	Index int      `json:"index"`
	Kind  string   `json:"kind"` // hold | ramp
	Start RawValue `json:"start_s"`
	End   RawValue `json:"end_s"`
	From  RawValue `json:"from_c"`
	To    RawValue `json:"to_c"`
}

type FurnacePayload struct {
	TimeUnit string           `json:"time_unit"`
	TempUnit string           `json:"temp_unit"`
	Segments []FurnaceSegment `json:"segments"`
}

type GasValveEvent struct {
	N      int      `json:"n"`
	At     RawValue `json:"at"`
	Valve  string   `json:"valve"`
	Action string   `json:"action"`
	Gas    string   `json:"gas"`
	Raw    string   `json:"raw,omitempty"`
}

type GasPayload struct {
	TimeUnit string          `json:"time_unit"`
	Events   []GasValveEvent `json:"events"`
}

type DriftPayload struct {
	Method     string   `json:"method"` // blank | linear | none
	Slope      RawValue `json:"slope_mg_per_min,omitempty"`
	Intercept  RawValue `json:"intercept_mg,omitempty"`
	Reference  string   `json:"reference,omitempty"` // record id when method=blank
	Note       string   `json:"note,omitempty"`
}

type Mass0Payload struct {
	Mass   RawValue `json:"mass"`
	Method string   `json:"method"`
	Layer  string   `json:"layer"` // initial | revised
	Note   string   `json:"note,omitempty"`
}

// ---- events (append-only log) ----

const (
	EvtProjectCreated = "project.created"
	EvtImportCommitted = "import.committed"
	EvtParamsRevised   = "params.revised"
	EvtAnalysisRan     = "analysis.ran"
	EvtDecisionMade    = "decision.made"
	EvtDecisionRevoked = "decision.revoked"
	EvtNotePosted      = "note.posted"
)

type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	At        string          `json:"at"`
	Actor     string          `json:"actor"`
	Payload   map[string]any  `json:"payload,omitempty"`
}

// ---- parameters ----

type Parameters struct {
	Version          int     `json:"version"`
	AtmDelayS        float64 `json:"atm_delay_s"`
	ThermalLagS      float64 `json:"thermal_lag_s"`
	SmoothingWindowS float64 `json:"smoothing_window_s"`
	MinProminenceMg  float64 `json:"min_prominence_mg"`
	PeakMergeS       float64 `json:"peak_merge_s"`
	NoiseBandMg      float64 `json:"noise_band_mg"`
	GapThresholdS    float64 `json:"gap_threshold_s"`
}

func DefaultParameters() Parameters {
	return Parameters{
		Version:          1,
		AtmDelayS:        60,
		ThermalLagS:      40,
		SmoothingWindowS: 30,
		MinProminenceMg:  0.05,
		PeakMergeS:       220,
		NoiseBandMg:      0.01,
		GapThresholdS:    20,
	}
}

// ---- derivations ----

const DerivationAlgorithm = "tga-stage-decomp"
const DerivationAlgorithmVersion = "3.1.0"

const (
	SlotCurve   = "curve"
	SlotDTG     = "dtg"
	SlotStages  = "stages"
	SlotResidue = "residue"
)

type SourceRef struct {
	Kind      string `json:"kind"` // record | derivation | params
	ID        string `json:"id,omitempty"`
	Version   int    `json:"version,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

type Assertion struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"` // info | warning
	Message  string         `json:"message"`
	Refs     []string       `json:"refs,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type Derivation struct {
	ID               string       `json:"id"`
	Slot             string       `json:"slot"`
	Version          int          `json:"version"`
	Algorithm        string       `json:"algorithm"`
	AlgorithmVersion string       `json:"algorithm_version"`
	ParamFingerprint string       `json:"param_fingerprint"`
	Sources          []SourceRef  `json:"sources"`
	CreatedSeq       int          `json:"created_seq"`
	CreatedEventID   string       `json:"created_event_id"`
	SupersededSeq    int          `json:"superseded_seq,omitempty"`
	Status           string       `json:"status"` // current | stale
	StaleReasons     []string     `json:"stale_reasons,omitempty"`
	Assertions       []Assertion  `json:"assertions,omitempty"`
	Payload          any          `json:"payload"`
}

// ---- decisions ----

const (
	ActionAccept = "accept"
	ActionReject = "reject"
	ActionAdjust = "adjust"
)

type StageAdjustment struct {
	StartS  *float64 `json:"start_s,omitempty"`
	EndS    *float64 `json:"end_s,omitempty"`
	Note    string   `json:"note,omitempty"`
}

type Decision struct {
	ID            string           `json:"id"`
	StageID       string           `json:"stage_id"`
	GroupID       string           `json:"group_id,omitempty"`
	Action        string           `json:"action"`
	Reason        string           `json:"reason,omitempty"`
	Adjustment    *StageAdjustment `json:"adjustment,omitempty"`
	Seq           int              `json:"seq"`
	At            string           `json:"at"`
	Actor         string           `json:"actor"`
	Revoked       bool             `json:"revoked"`
	RevokedSeq    int              `json:"revoked_seq,omitempty"`
	ConflictWith  []string         `json:"conflict_with,omitempty"`
}

// ChainStep is one expandable step of a residue calculation chain.
type ChainStep struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Formula  string         `json:"formula"`
	Value    *RawValue      `json:"value,omitempty"`
	Sources  []SourceRef    `json:"sources,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}
