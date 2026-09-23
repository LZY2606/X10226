// Package tga defines the domain model for the thermogravimetric
// analysis (TGA) stage-decomposition and atmosphere-event system.
//
// All domain quantities carry a unit or a coordinate basis. Raw inputs
// are immutable once submitted; revisions are expressed as new versions
// linked to their parent version. Every derived value records the
// algorithm version, a parameter fingerprint and its direct sources.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ID is a stable identity for every object shown in the review UI.
// Identities survive export, deletion of the local database and
// re-import unchanged.
type ID string

// Layer names one of the stratified baseline inputs.
type Layer string

const (
	LayerRawPoints Layer = "raw_points"       // raw sample points
	LayerDrift     Layer = "drift_correction" // balance drift correction
	LayerFurnace   Layer = "furnace_program"  // furnace program
	LayerGas       Layer = "gas_events"       // gas valve events
	LayerBlank     Layer = "blank_curve"      // blank curve
	LayerInitMass  Layer = "initial_mass"     // sample initial mass
)

// LayerOrder fixes the stratified storage order of the baseline layers.
var LayerOrder = []Layer{
	LayerRawPoints, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitMass,
}

// Canonical units used by the derived layer. Raw representations are
// preserved on the version; normalization happens only in derivation.
const (
	UnitSecond  = "s"
	UnitCelsius = "C"
	UnitMg      = "mg"
	UnitMlMin   = "mL/min"
	UnitPct     = "%"
	UnitPctS    = "%/s"
)

// Quantity is a domain value with its original textual representation,
// a normalized value in the canonical unit (NaN when unconvertible) and
// a diagnostic when conversion failed. Unconvertible values stay in
// place with a diagnostic; they are never dropped for plotting.
type Quantity struct {
	Raw   string  `json:"raw"`
	Value float64 `json:"value"` // canonical unit, NaN when unconvertible
	Unit  string  `json:"unit"`  // canonical unit
	Diag  string  `json:"diag,omitempty"`
}

// Diagnostic explains why a raw line or value could not be normalized.
type Diagnostic struct {
	Layer  Layer  `json:"layer"`
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// Version is one immutable submission of a baseline layer.
type Version struct {
	ID          ID     `json:"id"`
	Layer       Layer  `json:"layer"`
	Parent      ID     `json:"parent,omitempty"` // previous version of the same layer
	Raw         string `json:"raw"`              // original text, never overwritten
	Fingerprint string `json:"fingerprint"`
	Note        string `json:"note,omitempty"`
}

// Point is a raw sample point: furnace time, temperature, mass.
type Point struct {
	T    float64 `json:"t"`    // s, furnace time
	Temp float64 `json:"temp"` // C
	Mass float64 `json:"mass"` // mg
}

// FurnaceSeg is one ramp/hold segment of the furnace program.
type FurnaceSeg struct {
	T0          float64 `json:"t0"` // s
	T1          float64 `json:"t1"` // s
	Setpoint    float64 `json:"setpoint"`
	RateCPerMin float64 `json:"rate"`
}

// GasEvent is a gas valve switch. The valve switch time is NOT the
// sample reaction time: atmosphere delay and thermal lag shift it.
type GasEvent struct {
	T        float64 `json:"t"` // s, valve switch time
	Gas      string  `json:"gas"`
	Flow     float64 `json:"flow"` // mL/min
	FlowUnit string  `json:"flow_unit"`
}

// ParsedLayer is the normalized view of one version. Parse failures are
// kept as diagnostics alongside the retained raw text.
type ParsedLayer struct {
	Version ID           `json:"version"`
	Layer   Layer         `json:"layer"`
	Points  []Point       `json:"points,omitempty"` // raw_points / blank / drift
	Furnace []FurnaceSeg  `json:"furnace,omitempty"`
	Gas     []GasEvent    `json:"gas,omitempty"`
	InitMass *Quantity    `json:"init_mass,omitempty"`
	Diags   []Diagnostic  `json:"diags,omitempty"`
}

// Params are the reviewable derivation parameters. A revision creates a
// params_revised event; derived values carrying the old fingerprint
// become stale but stay viewable.
type Params struct {
	Revision      int     `json:"revision"`
	SmoothWindow  int     `json:"smooth_window"`   // DTG smoothing window, odd number of points
	GapFactor     float64 `json:"gap_factor"`      // dt > GapFactor*median(dt) is a sampling gap
	AtmosDelayS   float64 `json:"atmos_delay_s"`   // gas transport delay valve->sample
	ThermalLagS   float64 `json:"thermal_lag_s"`   // furnace thermocouple -> sample lag
	ProminencePct float64 `json:"prominence_pct"`  // min DTG peak prominence in %/s
	OverlapRatio  float64 `json:"overlap_ratio"`   // valley/peak ratio above which peaks overlap
}

// DefaultParams returns the initial parameter set (revision 1).
func DefaultParams() Params {
	return Params{
		Revision:      1,
		SmoothWindow:  5,
		GapFactor:     4.0,
		AtmosDelayS:   45,
		ThermalLagS:   20,
		ProminencePct: 0.002,
		OverlapRatio:  0.55,
	}
}

// Fingerprint is a stable hash of the parameter set.
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	return "fp-" + shortHash(string(b))
}

// Judgment codes recorded as machine-readable events.
const (
	JAtmosDelayLag = "atmosphere_delay_lag"  // valve time != sample reaction time
	JStagesOverlap = "stages_may_overlap"    // stages may overlap
	JNotForced100  = "stages_not_forced_100" // stage losses need not sum to 100%
	JGapNoInterp   = "gap_no_interpolation"  // data gaps are not interpolated across
	JOverlapAmbig  = "overlap_ambiguous"     // several legal decompositions retained
	JUnconvertible = "unconvertible_value"   // value kept raw with diagnostic
	JPartialImport = "partial_import"        // import half-failure
)

// Judgment is a machine-readable rationale attached to derivations.
type Judgment struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Refs   []ID   `json:"refs,omitempty"`
}

// ChainStep is one step of an expandable calculation chain.
type ChainStep struct {
	Label  string  `json:"label"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Source ID      `json:"source,omitempty"`
	Note   string  `json:"note,omitempty"`
}

// Sample is a corrected, normalized sample point.
type Sample struct {
	T    float64 `json:"t"`
	Temp float64 `json:"temp"`
	Mass float64 `json:"mass"` // corrected mg
	Pct  float64 `json:"pct"`  // % of initial mass
}

// CurvePayload is the corrected+normalized TG curve, split at gaps.
type CurvePayload struct {
	Segments [][]Sample `json:"segments"`
	M0       Quantity   `json:"m0"`
}

// DTGPoint is one point of the derivative thermogravimetric curve.
type DTGPoint struct {
	T    float64 `json:"t"`
	Temp float64 `json:"temp"`
	Pct  float64 `json:"pct"`
	DTG  float64 `json:"dtg"` // %/s, negative during mass loss
}

// DTGPayload carries DTG per segment; no values are computed across gaps.
type DTGPayload struct {
	Segments [][]DTGPoint `json:"segments"`
}

// AtmosInterval maps a gas valve event to sample-reaction time.
type AtmosInterval struct {
	Gas     string  `json:"gas"`
	Flow    float64 `json:"flow"`
	ValveT  float64 `json:"valve_t"`  // s, valve switch time
	SampleT float64 `json:"sample_t"` // s, atmosphere actually reaches the sample
}

// AtmosPayload is the atmosphere timeline at the sample position.
type AtmosPayload struct {
	Intervals []AtmosInterval `json:"intervals"`
}

// Stage is one decomposition-stage candidate. Bounds are sample-time
// coordinates, never valve times.
type Stage struct {
	ID           ID          `json:"id"`
	Name         string      `json:"name"`
	T0           float64     `json:"t0"`    // s, sample time
	T1           float64     `json:"t1"`    // s, sample time
	Temp0        float64     `json:"temp0"` // C
	Temp1        float64     `json:"temp1"` // C
	LossPct      float64     `json:"loss_pct"`
	PeakTemp     float64     `json:"peak_temp"`
	PeakDTG      float64     `json:"peak_dtg"`    // %/s
	ResiduePct   float64     `json:"residue_pct"` // % remaining at stage end
	Chain        []ChainStep `json:"chain"`
}

// StageSet is one legal decomposition. Contested sets coexist when the
// data do not uniquely decide (e.g. overlapping peaks).
type StageSet struct {
	ID        ID      `json:"id"`
	Label     string  `json:"label"`
	Stages    []Stage `json:"stages"`
	Contested bool    `json:"contested"`
	SumLoss   float64 `json:"sum_loss"` // recorded, never forced to 100
}

// RejectedPeak is a DTG feature rejected as a stage, with its reason.
type RejectedPeak struct {
	Temp   float64 `json:"temp"`
	DTG    float64 `json:"dtg"`
	Reason string  `json:"reason"`
}

// CandidatePayload holds all candidate sets plus rejected features.
type CandidatePayload struct {
	Sets     []StageSet     `json:"sets"`
	Rejected []RejectedPeak `json:"rejected,omitempty"`
}

// Residue is one residue-rate result with its calculation chain.
type Residue struct {
	ID      ID          `json:"id"`
	Label   string      `json:"label"` // "final" or a stage name
	StageID ID          `json:"stage_id,omitempty"`
	Pct     float64     `json:"pct"`
	Chain   []ChainStep `json:"chain"`
}

// ResiduePayload groups the residue results.
type ResiduePayload struct {
	Items []Residue `json:"items"`
}

// Derived kinds.
const (
	KCurve      = "corrected_curve"
	KDTG        = "dtg"
	KAtmos      = "atmosphere_timeline"
	KCandidates = "stage_candidates"
	KResidues   = "residues"
)

// AlgoVersion identifies the derivation algorithm.
const AlgoVersion = "derive/1"

// Derived is a derived value with algorithm version, parameter
// fingerprint and direct sources. Stale derived values remain viewable
// but may not masquerade as current.
type Derived struct {
	ID         ID              `json:"id"`
	Kind       string          `json:"kind"`
	Algo       string          `json:"algo"`
	ParamsFP   string          `json:"params_fp,omitempty"`
	Sources    []ID            `json:"sources"`
	Payload    json.RawMessage `json:"payload"`
	CreatedSeq int64           `json:"created_seq"`
}

// Decision actions.
const (
	ActAccept = "accept"
	ActReject = "reject"
	ActAdjust = "adjust"
)

// Decision is a human ruling on a candidate or residue.
type Decision struct {
	ID        ID                 `json:"id"`
	Target    ID                 `json:"target"`
	Action    string             `json:"action"`
	Params    map[string]float64 `json:"params,omitempty"`
	Rationale string             `json:"rationale"`
	Reviewer  string             `json:"reviewer"`
	Resolves  ID                 `json:"resolves,omitempty"` // conflict id being resolved
	Seq       int64              `json:"seq"`
	Reverted  bool               `json:"reverted"`
}

// Conflict records two contradictory live decisions.
type Conflict struct {
	ID        ID     `json:"id"`
	Decisions []ID   `json:"decisions"`
	Note      string `json:"note"`
	Resolved  bool   `json:"resolved"`
}

// Event kinds (the event log is the only mutable history).
const (
	EvLayerCommitted   = "layer_committed"
	EvImportPartial    = "import_partial"
	EvParamsRevised    = "params_revised"
	EvDerived          = "derived_generated"
	EvJudgment         = "judgment_recorded"
	EvDecision         = "decision_made"
	EvDecisionReverted = "decision_reverted"
	EvConflict         = "conflict_marked"
	EvArchiveRestored  = "archive_restored"
)

// Event is one immutable entry of the append-only log.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      ID              `json:"id"`
	Kind    string          `json:"kind"`
	At      int64           `json:"at"` // virtual clock reading
	Payload json.RawMessage `json:"payload"`
}

// Hash builds a deterministic identity from parts.
func Hash(parts ...string) ID {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return ID(hex.EncodeToString(h.Sum(nil))[:16])
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}

// FingerprintText hashes raw text for version identity.
func FingerprintText(s string) string { return "sha256-" + shortHash(s) }

// NaN reports whether the quantity failed conversion.
func (q Quantity) NaN() bool { return math.IsNaN(q.Value) }

// DecodePayload unmarshals a derived payload.
func DecodePayload[T any](d *Derived) (T, error) {
	var v T
	err := json.Unmarshal(d.Payload, &v)
	return v, err
}

// EncodePayload marshals a payload.
func EncodePayload(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// SortedIDs returns sorted ids, for deterministic fingerprints.
func SortedIDs(ids []ID) []ID {
	out := append([]ID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// FormatID renders a short human label for an ID.
func FormatID(id ID) string {
	s := string(id)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// ParseLayerName normalizes a layer header.
func ParseLayerName(s string) (Layer, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, l := range LayerOrder {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

// String renders a quantity with unit.
func (q Quantity) String() string {
	if q.NaN() {
		return fmt.Sprintf("%s [unconverted: %s]", q.Raw, q.Diag)
	}
	return fmt.Sprintf("%g %s", q.Value, q.Unit)
}
