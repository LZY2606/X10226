// Package domain defines the typed model for the TGA review system:
// versioned source inputs, derived results with provenance, and the
// append-only event log. Every domain quantity carries its unit and the
// original raw representation; unconvertible values stay in place with a
// diagnostic instead of being dropped.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// AlgoVersion identifies the derivation algorithm version stamped on every
// derived record.
const AlgoVersion = "tga-1.0.0"

// Input kinds (layered baseline of the case archive).
const (
	InRawPoints   = "raw_points"        // 原始采样点
	InDriftCorr   = "drift_correction"  // 天平漂移校正
	InFurnaceProg = "furnace_program"   // 炉程序
	InGasEvents   = "gas_events"        // 气体阀事件
	InBlankCurve  = "blank_curve"       // 空白曲线
	InMassLayers  = "mass_layers"       // 样品初重分层
)

// InputKinds lists all input kinds in canonical baseline order.
var InputKinds = []string{InRawPoints, InDriftCorr, InFurnaceProg, InGasEvents, InBlankCurve, InMassLayers}

// Derived kinds.
const (
	DerCorrected  = "corrected_curve"
	DerDTG        = "dtg_curve"
	DerStages     = "stage_candidates"
	DerResidue    = "residue"
	DerAtmosphere = "atmosphere_timeline"
)

// Event kinds (machine readable).
const (
	EvImportCommitted     = "import.committed"
	EvImportPartialFailed = "import.partial_failed"
	EvSourceRevised       = "source.revised"
	EvCandidatesGenerated = "candidates.generated"
	EvResultStaled        = "result.staled"
	EvJudgeAtmosDelay     = "judgment.atmosphere_delay"
	EvJudgeOverlap        = "judgment.overlap_allowed"
	EvJudgeNoForcedSum    = "judgment.no_forced_sum"
	EvJudgeGapNoInterp    = "judgment.gap_no_interpolate"
	EvDecisionAccept      = "decision.accept"
	EvDecisionReject      = "decision.reject"
	EvDecisionAdjust      = "decision.adjust_params"
	EvDecisionConflict    = "decision.conflict"
	EvDecisionRevert      = "decision.revert"
	EvArchiveImported     = "archive.imported"
)

// Quantity is a domain value with unit and original raw representation.
// OK=false means the raw text could not be converted; the value is kept in
// place with Diag and never silently dropped.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Raw   string  `json:"raw"`
	OK    bool    `json:"ok"`
	Diag  string  `json:"diag,omitempty"`
}

// Q builds a valid quantity.
func Q(v float64, unit string) Quantity {
	return Quantity{Value: v, Unit: unit, Raw: fmt.Sprintf("%g %s", v, unit), OK: true}
}

// BadQ builds an unconvertible quantity kept in place with a diagnostic.
func BadQ(raw, unit, diag string) Quantity {
	return Quantity{Unit: unit, Raw: raw, OK: false, Diag: diag}
}

// ---- Source input payloads ----

type SamplePoint struct {
	Seq      int      `json:"seq"`
	Time     Quantity `json:"time"` // s
	Temp     Quantity `json:"temp"` // °C
	Mass     Quantity `json:"mass"` // mg
	GapAfter bool     `json:"gap_after,omitempty"`
}

type RawPointsDoc struct {
	Points []SamplePoint `json:"points"`
}

type DriftPoint struct {
	Time  Quantity `json:"time"` // s
	Drift Quantity `json:"drift"` // mg, balance baseline drift to subtract
}

type DriftDoc struct {
	Points []DriftPoint `json:"points"`
}

type FurnaceSegment struct {
	StartTime   Quantity  `json:"start_time"`   // s
	RateCPerMin *float64  `json:"rate_c_per_min,omitempty"`
	TargetTemp  Quantity  `json:"target_temp"`  // °C
	Comment     string    `json:"comment,omitempty"`
}

type FurnaceDoc struct {
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Time      Quantity `json:"time"` // s, valve switch time
	FromGas   string   `json:"from_gas"`
	ToGas     string   `json:"to_gas"`
	FlowMlMin Quantity `json:"flow_ml_min"`
}

type GasEventsDoc struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	Time Quantity `json:"time"` // s
	Mass Quantity `json:"mass"` // mg, empty-pan blank mass
}

type BlankDoc struct {
	Points []BlankPoint `json:"points"`
}

// MassLayer is one layer of the sample initial-mass record (e.g. crucible
// tare, gross, net). Layers are preserved, not collapsed.
type MassLayer struct {
	Label string   `json:"label"`
	Mass  Quantity `json:"mass"` // mg
}

type MassLayersDoc struct {
	Layers []MassLayer `json:"layers"`
}

// NetInitialMass returns the layer labelled "net" (sample initial mass).
func (d MassLayersDoc) NetInitialMass() (Quantity, bool) {
	for _, l := range d.Layers {
		if l.Label == "net" && l.Mass.OK {
			return l.Mass, true
		}
	}
	return Quantity{}, false
}

// ---- Records ----

// SourceRef pins a dependency to an exact input id+version.
type SourceRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// InputRecord is one immutable version of a source input. Once committed it
// is never overwritten; revisions are new versions with Parent set.
type InputRecord struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Version       int             `json:"version"`
	ParentVersion int             `json:"parent_version,omitempty"`
	Raw           string          `json:"raw"` // original representation as submitted
	Doc           json.RawMessage `json:"doc"`
	CreatedSeq    int64           `json:"created_seq"`
	Note          string          `json:"note,omitempty"`
}

// DerivedRecord is a computed result with algorithm version, parameter
// fingerprint and direct sources. Stale results remain viewable but are
// labelled and never masquerade as current.
type DerivedRecord struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Version     int             `json:"version"`
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fp"`
	Sources     []SourceRef     `json:"sources"`
	Payload     json.RawMessage `json:"payload"`
	CreatedSeq  int64           `json:"created_seq"`
	Stale       bool            `json:"stale"`
	StaleReason string          `json:"stale_reason,omitempty"`
}

// Event is one append-only history entry.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	TS      int64           `json:"ts"` // virtual clock, unix seconds
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// ---- Analysis parameters ----

// Params are the tunable analysis parameters. Fingerprint identifies the
// exact parameter set on every derived record.
type Params struct {
	SmoothWindow      int     `json:"smooth_window"`       // DTG smoothing window (points)
	MinProminencePct  float64 `json:"min_prominence_pct"`  // min DTG peak prominence, %/min
	AtmosphereDelayS  float64 `json:"atmosphere_delay_s"`  // gas transport delay valve->sample
	ThermalLagS       float64 `json:"thermal_lag_s"`       // furnace->sample temperature lag
	ResidueFromS      float64 `json:"residue_from_s"`      // residue evaluated from this time
	MinStageWidthS    float64 `json:"min_stage_width_s"`   // reject narrower stage candidates
}

// DefaultParams returns the baseline parameter set.
func DefaultParams() Params {
	return Params{
		SmoothWindow:     5,
		MinProminencePct: 0.15,
		AtmosphereDelayS: 45,
		ThermalLagS:      20,
		ResidueFromS:     0,
		MinStageWidthS:   30,
	}
}

// Fingerprint returns a stable hash of the parameter set.
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// ---- Derived payloads ----

type CorrPoint struct {
	T       float64 `json:"t"`        // s
	Temp    float64 `json:"temp"`     // °C
	MassMg  float64 `json:"mass_mg"`  // drift-corrected
	MassPct float64 `json:"mass_pct"` // vs initial mass
}

type CorrectedCurve struct {
	Points   []CorrPoint `json:"points"`
	GapAfter []float64   `json:"gap_after,omitempty"` // times where a sampling gap follows
}

type DTGPoint struct {
	T    float64 `json:"t"`
	Temp float64 `json:"temp"`
	DTG  float64 `json:"dtg"` // %/min, negative = losing mass
}

type DTGCurve struct {
	Points []DTGPoint `json:"points"`
}

// Stage is one decomposition-stage candidate. Stages may overlap and their
// mass losses are NOT forced to sum to 100%.
type Stage struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	OnsetS         float64  `json:"onset_s"`
	PeakS          float64  `json:"peak_s"`
	EndS           float64  `json:"end_s"`
	PeakTempC      float64  `json:"peak_temp_c"`
	MassLossPct    float64  `json:"mass_loss_pct"`
	Overlaps       []string `json:"overlaps,omitempty"`
	Atmosphere     string   `json:"atmosphere"` // gas active at reaction time (delay-corrected)
}

// RejectedStage keeps a rejected candidate and its reason as first-class data.
type RejectedStage struct {
	PeakS  float64 `json:"peak_s"`
	Reason string  `json:"reason"`
}

// Judgment is a machine-readable judgment attached to a stage set.
type Judgment struct {
	Code     string   `json:"code"` // e.g. "gap_no_interpolate"
	Detail   string   `json:"detail"`
	Affected []string `json:"affected,omitempty"`
}

type StageSet struct {
	Candidates []Stage          `json:"candidates"`
	Rejected   []RejectedStage  `json:"rejected,omitempty"`
	Judgments  []Judgment       `json:"judgments"`
	SumPct     float64          `json:"sum_pct"` // informational only, not forced to 100
}

// ChainStep is one step of an expandable calculation chain.
type ChainStep struct {
	Desc    string      `json:"desc"`
	Value   float64     `json:"value"`
	Unit    string      `json:"unit"`
	Sources []SourceRef `json:"sources,omitempty"`
}

type ResidueResult struct {
	FinalMassPct float64     `json:"final_mass_pct"`
	FinalMassMg  float64     `json:"final_mass_mg"`
	InitialMass  Quantity    `json:"initial_mass"`
	Chain        []ChainStep `json:"chain"`
}

// AtmoSegment maps a valve switch to the delay-corrected reaction interval.
type AtmoSegment struct {
	Gas       string  `json:"gas"`
	ValveS    float64 `json:"valve_s"`    // valve switch time
	ReactionS float64 `json:"reaction_s"` // sample actually sees the gas
	DelayS    float64 `json:"delay_s"`
	EndS      float64 `json:"end_s"`
}

type AtmosphereTimeline struct {
	Segments []AtmoSegment `json:"segments"`
}

// SortStages orders stages by onset then peak for stable display.
func SortStages(s []Stage) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].OnsetS != s[j].OnsetS {
			return s[i].OnsetS < s[j].OnsetS
		}
		return s[i].PeakS < s[j].PeakS
	})
}
