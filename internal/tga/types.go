// Package tga implements the domain layer of the thermogravimetric
// review system: layered raw inputs, unit handling, drift correction,
// DTG, stage decomposition, atmosphere-delay modelling and residual
// rate calculation chains.
//
// Design rules honoured here:
//   - every domain quantity carries a unit and a coordinate basis;
//   - the raw representation is preserved at import, normalisation
//     happens only in the derived layer;
//   - values that cannot be converted stay in place with a diagnostic;
//   - candidates, rejection reasons, errors and unknown states are
//     first-class data, never silently dropped.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AlgoVersion is bumped whenever any derived computation changes.
const AlgoVersion = "tga.v1"

// Layer names of the six stratified raw inputs.
const (
	LayerRawSamples     = "raw_samples"     // 原始采样点
	LayerBalanceDrift   = "balance_drift"   // 天平漂移校正
	LayerFurnaceProgram = "furnace_program" // 炉程序
	LayerGasEvents      = "gas_events"      // 气体阀事件
	LayerBlankCurve     = "blank_curve"     // 空白曲线
	LayerInitialMass    = "initial_mass"    // 样品初重
)

// LayerNames lists all layers in canonical order.
var LayerNames = []string{
	LayerRawSamples,
	LayerBalanceDrift,
	LayerFurnaceProgram,
	LayerGasEvents,
	LayerBlankCurve,
	LayerInitialMass,
}

// Diagnostic is a first-class, machine-readable note attached to a
// layer or a derived value. Code is stable for tests and tooling.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Where   string `json:"where,omitempty"`
}

// Quantity is a domain value with its original representation, the
// normalised value, unit and coordinate basis. When OK is false the
// value could not be converted; Raw stays in place and a Diagnostic
// is emitted by the parser instead of dropping the value.
type Quantity struct {
	Raw   string  `json:"raw"`   // original representation as submitted
	Value float64 `json:"value"` // normalised value (meaningful only when OK)
	Unit  string  `json:"unit"`  // normalised unit: mg, C, s, mL/min
	Basis string  `json:"basis"` // coordinate basis, e.g. furnace-clock, sample
	OK    bool    `json:"ok"`
}

// CanonicalJSON renders v with deterministic key order for hashing.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// HashID returns a stable content-addressed identifier.
func HashID(prefix string, v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return prefix + "_" + hex.EncodeToString(sum[:])[:20]
}

// Fingerprint hashes algorithm parameters into a short stable token.
func Fingerprint(v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return hex.EncodeToString(sum[:])[:16]
}

// ---------------------------------------------------------------------------
// Layer payloads (raw, as imported; quantities not yet normalised)
// ---------------------------------------------------------------------------

type RawSample struct {
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"`
	Temp Quantity `json:"temp"`
}

type RawSamplesLayer struct {
	Points []RawSample `json:"points"`
}

type DriftPoint struct {
	Time  Quantity `json:"time"`
	Drift Quantity `json:"drift"` // apparent mass gain of empty pan
}

type BalanceDriftLayer struct {
	Points []DriftPoint `json:"points"`
}

type FurnaceSegment struct {
	StartTime Quantity `json:"startTime"`
	StartTemp Quantity `json:"startTemp"`
	EndTime   Quantity `json:"endTime"`
	EndTemp   Quantity `json:"endTemp"`
}

type FurnaceProgramLayer struct {
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Time  Quantity `json:"time"` // valve switch time, furnace clock
	Valve string   `json:"valve"`
	Gas   string   `json:"gas"`
	Flow  Quantity `json:"flow"`
}

type GasEventsLayer struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"` // apparent mass of blank run
}

type BlankCurveLayer struct {
	Points []BlankPoint `json:"points"`
}

type InitialMassLayer struct {
	Mass Quantity `json:"mass"`
}

// ---------------------------------------------------------------------------
// Normalised intermediate data (derived layer)
// ---------------------------------------------------------------------------

// Point is one normalised sample: time s, mass mg, temperature C.
type Point struct {
	T    float64 `json:"t"`
	Mass float64 `json:"mass"`
	Temp float64 `json:"temp"`
}

// Segment is a contiguous run of samples. The pipeline never
// interpolates across the gap before a segment.
type Segment struct {
	Points    []Point `json:"points"`
	GapBefore bool    `json:"gapBefore"`
}

// Curve is the drift/blank-corrected sample curve, split at gaps.
type Curve struct {
	Segments    []Segment    `json:"segments"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Points flattens the curve for plotting; gap boundaries remain
// recorded on the Segments structure itself.
func (c Curve) Points() []Point {
	var out []Point
	for _, s := range c.Segments {
		out = append(out, s.Points...)
	}
	return out
}

// DTGPoint is the smoothed mass derivative at a sample time.
type DTGPoint struct {
	T    float64 `json:"t"`
	Temp float64 `json:"temp"`
	DmDt float64 `json:"dmDt"` // mg/s
}

type DTGSegment struct {
	Points []DTGPoint `json:"points"`
}

type DTGCurve struct {
	Segments    []DTGSegment `json:"segments"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// ---------------------------------------------------------------------------
// Atmosphere model
// ---------------------------------------------------------------------------

// AtmosphereEvent pairs the valve switch time (furnace clock) with
// the modelled sample reaction time. The two differ by the gas
// transport delay plus the thermal lag; they are never conflated.
type AtmosphereEvent struct {
	ValveTime    float64 `json:"valveTime"`    // s, furnace clock
	ReactionTime float64 `json:"reactionTime"` // s, sample basis
	Gas          string  `json:"gas"`
	Valve        string `json:"valve"`
	FlowMLMin    float64 `json:"flowMLMin"`
}

// AtmosphereWindow is the span of sample time dominated by one gas.
type AtmosphereWindow struct {
	Gas          string  `json:"gas"`
	FromReaction float64 `json:"fromReaction"`
	ToReaction   float64 `json:"toReaction"` // +Inf when open-ended
}

type AtmosphereTimeline struct {
	Events      []AtmosphereEvent  `json:"events"`
	Windows     []AtmosphereWindow `json:"windows"`
	DelayS      float64            `json:"delayS"`
	LagS        float64            `json:"lagS"`
	Diagnostics []Diagnostic       `json:"diagnostics,omitempty"`
}

// ---------------------------------------------------------------------------
// Stage candidates
// ---------------------------------------------------------------------------

// CandidateStatus is the lifecycle state of a stage candidate.
type CandidateStatus string

const (
	StatusCandidate CandidateStatus = "candidate" // awaits review
	StatusUnknown   CandidateStatus = "unknown"   // cannot be decided from data
	StatusRejected  CandidateStatus = "rejected"  // rejected by the algorithm
)

// StageCandidate is one proposed decomposition stage. Stages may
// overlap and their mass losses are not normalised to 100 %.
type StageCandidate struct {
	Label        string          `json:"label"`
	Status       CandidateStatus `json:"status"`
	Reasons      []string        `json:"reasons,omitempty"` // machine-readable rejection/unknown causes
	StartT       float64         `json:"startT"`            // s
	EndT         float64         `json:"endT"`              // s
	StartTemp    float64         `json:"startTemp"`         // C
	EndTemp      float64         `json:"endTemp"`           // C
	PeakT        float64         `json:"peakT"`             // s of max |dm/dt|
	PeakTemp     float64         `json:"peakTemp"`          // C
	PeakDmDt     float64         `json:"peakDmDt"`          // mg/s (negative)
	LossMg       float64         `json:"lossMg"`            // mass loss inside window
	LossPct      float64         `json:"lossPct"`           // % of initial mass
	LossKnown    bool            `json:"lossKnown"`         // false when window touches a gap
	OverlapsWith []string        `json:"overlapsWith,omitempty"`
}

type StageSet struct {
	Candidates  []StageCandidate `json:"candidates"`
	SumPct      float64          `json:"sumPct"` // may exceed 100; never forced
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
}

// ---------------------------------------------------------------------------
// Residual rate calculation chain
// ---------------------------------------------------------------------------

// ChainStep is one auditable step of a calculation chain.
type ChainStep struct {
	Step        string   `json:"step"`
	Description string   `json:"description"`
	Inputs      []string `json:"inputs,omitempty"` // human-readable input refs
	Value       float64  `json:"value"`
	Unit        string   `json:"unit"`
}

type ResidualRate struct {
	FinalMassMg   float64      `json:"finalMassMg"`
	InitialMassMg float64      `json:"initialMassMg"`
	RatePct       float64      `json:"ratePct"`
	Chain         []ChainStep  `json:"chain"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

// ---------------------------------------------------------------------------
// Pipeline parameters
// ---------------------------------------------------------------------------

type Params struct {
	GapThresholdS    float64 `json:"gapThresholdS"`    // sampling gap detection threshold
	SmoothWindow     int     `json:"smoothWindow"`     // DTG smoothing window (points, odd)
	ProminenceMinPct float64 `json:"prominenceMinPct"` // min peak prominence, % of initial mass
	TransportDelayS  float64 `json:"transportDelayS"`  // gas transport delay
	ThermalLagS      float64 `json:"thermalLagS"`      // sample thermal lag
	NoiseBandMg      float64 `json:"noiseBandMg"`      // tolerated non-monotonic band
}

func DefaultParams() Params {
	return Params{
		GapThresholdS:    5,
		SmoothWindow:     11,
		ProminenceMinPct: 0.5,
		TransportDelayS:  45,
		ThermalLagS:      30,
		NoiseBandMg:      0.01,
	}
}

func (p Params) Fingerprint() string { return Fingerprint(p) }

// SortedKeys is a small helper for deterministic output.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
