// Package tga holds the domain model, parsers and deterministic derivation
// engine for the thermogravimetric analysis (TGA) review system.
//
// Domain rules implemented here (see docs and machine-readable events):
//
//   - valve-switch time is not reaction time: an atmosphere delay
//     (transport) and a temperature lag are applied explicitly;
//   - decomposition stages may overlap and their mass losses are NOT
//     renormalised to sum to 100%;
//   - sampling gaps split series: derivatives and fits never span a gap
//     (no interpolation across gaps);
//   - candidates, rejections, error and unknown states are first-class data;
//   - every derived value carries algorithm version, parameter fingerprint and
//     direct source references;
//   - all domain quantities carry a unit or coordinate basis; values that
//     cannot be converted stay in place with diagnostics instead of being
//     dropped for plotting.
package tga

import "encoding/json"

// AlgorithmVersion is bumped whenever derivation semantics change.
const AlgorithmVersion = "tga-derive-1.0.0"

// StreamKind is one of the layered raw inputs.
type StreamKind string

const (
	StreamSample   StreamKind = "sample"   // sample header + initial mass
	StreamPoints   StreamKind = "points"   // raw sampled TG points
	StreamBlank    StreamKind = "blank"    // blank curve
	StreamDrift    StreamKind = "drift"    // balance drift correction
	StreamFurnace  StreamKind = "furnace"  // furnace program / setpoints
	StreamGas      StreamKind = "gas"      // gas valve events
)

// AllStreams lists the layered raw-input streams in review order.
var AllStreams = []StreamKind{
	StreamSample, StreamPoints, StreamBlank, StreamDrift, StreamFurnace, StreamGas,
}

// ParseDiag severity.
const (
	DiagError   = "error"   // row unusable but retained
	DiagWarning = "warning" // row usable with caveats
)

// Q is a quantity that carries its original representation, a unit /
// coordinate basis and, when convertible, its canonical value.
type Q struct {
	Raw      string  `json:"raw"`             // exact original token
	Unit     string  `json:"unit"`            // declared unit / coordinate basis
	Canon    string  `json:"canonical_unit"`  // canonical unit after normalisation
	Value    float64 `json:"value,omitempty"` // value expressed in canonical unit
	Valid    bool    `json:"valid"`           // false => kept but not converted
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Row is one raw, immutable parsed line.
type Row struct {
	ID         string            `json:"id"`
	Stream     StreamKind        `json:"stream"`
	LineNo     int               `json:"line_no"`
	Raw        string            `json:"raw"`
	Fields     map[string]string `json:"fields"`
	Values     map[string]Q      `json:"values"`
	Severity   string            `json:"severity,omitempty"`
	Diagnostic string            `json:"diagnostic,omitempty"`
}

// Submission is the result of parsing one import attempt. Text is retained
// verbatim (immutable raw layer).
type Submission struct {
	Text     string                 `json:"text"`
	Sections map[StreamKind]string  `json:"sections"`
	Rows     map[StreamKind][]*Row `json:"rows"`
	// State: ok (all rows clean), partial (some rows kept, some rejected),
	// failed (nothing usable).
	State       string   `json:"state"`
	Diagnostics []Diag   `json:"diagnostics"`
}

// Diag is an import / parse diagnostic attached to a row or whole import.
type Diag struct {
	LineNo int    `json:"line_no,omitempty"`
	Stream string `json:"stream,omitempty"`
	Level  string `json:"level"`
	Code   string `json:"code"`
	Msg    string `json:"msg"`
}

// Parameters control derivation. They are versioned and fingerprinted.
type Parameters struct {
	// AtmosphereDelayS is transport delay between valve command and gas
	// reaching the sample (seconds, furnace-time basis).
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	// TemperatureLagK shifts furnace -> sample temperature (K).
	TemperatureLagK float64 `json:"temperature_lag_k"`
	// DriftRateUgMin is balance baseline drift in ug/min (linear vs time).
	DriftRateUgMin float64 `json:"drift_rate_ug_min"`
	// ApplyBlank enables blank-curve subtraction (linear interpolation of the
	// blank, never extrapolated beyond its span).
	ApplyBlank bool `json:"apply_blank"`
	// NoiseWindowS is non-monotonic smoothing window; mass is never forced
	// monotone.
	NoiseWindowS float64 `json:"noise_window_s"`
	// PeakProminencePct is DTG peak prominence threshold in %/min.
	PeakProminencePct float64 `json:"peak_prominence_pct_min"`
	// GapThresholdS flags time deltas larger than this as sampling gaps.
	GapThresholdS float64 `json:"gap_threshold_s"`
	// MinOverlapSepS: peaks closer than this in time are treated as an
	// overlapping cluster with multiple legal stage candidates.
	MinOverlapSepS float64 `json:"min_overlap_sep_s"`
}

// DefaultParameters returns the baseline rule set.
func DefaultParameters() Parameters {
	return Parameters{
		AtmosphereDelayS:  90,
		TemperatureLagK:  8,
		DriftRateUgMin:   0.0,
		ApplyBlank:       true,
		NoiseWindowS:     30,
		PeakProminencePct: 0.05,
		GapThresholdS:    120,
		MinOverlapSepS:   240,
	}
}

// Fingerprint hashes the parameter set so derived values name the rule version
// that produced them.
func (p Parameters) Fingerprint() string {
	b, _ := json.Marshal(p)
	return "param-" + shortHash(string(b))
}

// Basis describes the direct inputs of a derivation step.
type Basis struct {
	Algorithm  string   `json:"algorithm"`
	ParamFP    string   `json:"parameter_fingerprint"`
	SourceRefs []string `json:"source_refs"` // row or source-version ids
	ArtifactRefs []string `json:"artifact_refs,omitempty"`
}

// Artifact kinds.
const (
	KindCurve    = "corrected_curve"
	KindDTG      = "dtg_series"
	KindPeak     = "peak"
	KindStage    = "stage_candidate"
	KindResidue  = "residue"
	KindGasWin   = "gas_window"
	KindGap      = "sampling_gap"
	KindNote     = "domain_note"
)

// Artifact is any first-class derived value. Payload is kind-specific JSON.
type Artifact struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`        // logical key for supersession
	Label     string          `json:"label"`
	Payload   json.RawMessage `json:"payload"`
	Basis     Basis           `json:"basis"`
	// Status: current | stale | unknown | rejected-candidate (latter via
	// stage payload).
	Status    string          `json:"status"`
	Supersedes string         `json:"supersedes,omitempty"`
}

// Decision states for human review of stage candidates.
const (
	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"
	DecisionAdjusted = "adjusted"
)

// ---- typed payloads ----

type CurvePoint struct {
	T          float64 `json:"t_s"`
	Tfurnace   float64 `json:"t_furnace_c"`
	Tsample    float64 `json:"t_sample_c"`
	MrawMg     float64 `json:"m_raw_mg"`
	McorrMg    float64 `json:"m_corr_mg"`
	Mpct       float64 `json:"m_pct"` // of effective initial mass
	SegmentID  string  `json:"segment_id"`
	GapBefore  bool    `json:"gap_before"`
	NonMonotone bool   `json:"non_monotone"`
	SourceRow  string  `json:"source_row"`
}

type CurvePayload struct {
	Points       []CurvePoint `json:"points"`
	CorrectionMg float64      `json:"correction_note_mg,omitempty"`
	InitialMg    Q            `json:"initial_mass"`
	Note         string       `json:"note,omitempty"`
}

type DTGPoint struct {
	T          float64 `json:"t_s"`
	Tsample    float64 `json:"t_sample_c"`
	RatePctMin float64 `json:"rate_pct_min"`
	SegmentID  string  `json:"segment_id"`
}

type DTGPayload struct {
	Points []DTGPoint `json:"points"`
	Note   string     `json:"note,omitempty"`
}

type PeakPayload struct {
	Ts              float64 `json:"t_peak_s"`
	TsampleC        float64 `json:"t_peak_sample_c"`
	RatePctMin      float64 `json:"rate_pct_min"`
	ClusterID       string  `json:"cluster_id"`
	ClusterMembers  int     `json:"cluster_members"`
	SegmentID       string  `json:"segment_id"`
}

type StageCandidate struct {
	ID             string   `json:"id"`
	PeakID         string   `json:"peak_id"`
	ClusterID      string   `json:"cluster_id"`
	TStartS        float64  `json:"t_start_s"`
	TEndS          float64  `json:"t_end_s"`
	TStartSampleC  float64  `json:"t_start_sample_c"`
	TEndSampleC    float64  `json:"t_end_sample_c"`
	MassLossPct    float64  `json:"mass_loss_pct"`
	MassLossMg     float64  `json:"mass_loss_mg"`
	OverlapsWith   []string `json:"overlaps_with"`
	GasWindowID    string   `json:"gas_window_id"`
	Atmosphere     string   `json:"atmosphere"`
	Status         string   `json:"status"` // candidate | accepted | rejected | adjusted | unknown
	UnknownReason  string   `json:"unknown_reason,omitempty"`
	RejectionReasons []string `json:"rejection_reasons,omitempty"`
	Note           string   `json:"note,omitempty"`
}

type StagePayload struct{ Stage StageCandidate `json:"stage"` }

type ResidueChain struct {
	FinalMg       float64 `json:"final_mg"`
	InitialMg     float64 `json:"initial_mg"`
	ResiduePct    float64 `json:"residue_pct"`
	SumStagesPct  float64 `json:"sum_stage_loss_pct"`
	BalancedTo100 bool    `json:"forced_to_100"` // always false; documented
	GapTruncated  bool    `json:"gap_truncated"`
	// Steps is the expandable calculation chain.
	Steps []ResidueStep `json:"steps"`
}

type ResidueStep struct {
	Order      int     `json:"order"`
	Op         string  `json:"op"`
	Detail     string  `json:"detail"`
	ValueMg    float64 `json:"value_mg,omitempty"`
	ValuePct   float64 `json:"value_pct,omitempty"`
	Ref        string  `json:"ref"`
	RefKind    string  `json:"ref_kind"`
}

type ResiduePayload struct{ Residue ResidueChain `json:"residue"` }

type GasWindow struct {
	ID             string  `json:"id"`
	ValveEventRow  string  `json:"valve_event_row"`
	ValveTS        float64 `json:"valve_t_s"`
	ReactionTS     float64 `json:"reaction_t_s"`
	ValveTfurnaceC float64 `json:"valve_t_furnace_c"`
	ReactionTsampC float64 `json:"reaction_t_sample_c"`
	Gas            string  `json:"gas"`
	Note           string  `json:"note"`
}

type GasWindowPayload struct{ Window GasWindow `json:"window"` }

type GapPayload struct {
	ID          string  `json:"id"`
	TBeforeS    float64 `json:"t_before_s"`
	TAfterS     float64 `json:"t_after_s"`
	DurationS   float64 `json:"duration_s"`
	Interpolated bool   `json:"interpolated"` // always false
	LeftSegment string  `json:"left_segment"`
	RightSegment string `json:"right_segment"`
}

type NotePayload struct {
	Code string `json:"code"`
	Rule string `json:"rule"`
}
