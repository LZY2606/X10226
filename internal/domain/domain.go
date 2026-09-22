// Package domain defines the immutable core model of the TGA archive:
// measures with units, raw representations, append-only event envelopes
// and the payloads carried by each event.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

// Layers that can be imported. Raw inputs of every layer are immutable;
// revisions are expressed as new sources with a parent pointer.
const (
	LayerSamples     = "raw_samples"
	LayerDrift       = "drift_correction"
	LayerFurnace     = "furnace_program"
	LayerGas         = "gas_valve_events"
	LayerBlank       = "blank_curve"
	LayerInitialMass = "initial_mass"
)

// Event types.
const (
	EvProjectCreated  = "project_created"
	EvSourceSubmitted = "source_submitted"
	EvParamsRevised   = "params_revised"
	EvRuling          = "ruling"
	EvConflictResolved = "conflict_resolved"
	EvUndo            = "undo"
)

// Diagnostic severity.
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// Diagnostic is attached to the exact input fragment or derived object it
// concerns. Unconvertible values stay in place with a diagnostic instead
// of being dropped.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Layer    string `json:"layer,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	Message  string `json:"message"`
}

// Measure is a quantity with a canonical SI value plus the original textual
// representation. When Valid is false the value could not be converted; Raw
// is preserved and a diagnostic explains why.
type Measure struct {
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
	Raw     string  `json:"raw"`
	RawUnit string  `json:"rawUnit,omitempty"`
	Valid   bool    `json:"valid"`
	Note    string  `json:"note,omitempty"`
}

func invalidMeasure(raw, rawUnit, note string) Measure {
	return Measure{Value: math.NaN(), Unit: "", Raw: raw, RawUnit: rawUnit, Valid: false, Note: note}
}

// RawSample keeps every channel in raw and canonical form.
type RawSample struct {
	Index int      `json:"index"`
	Time  Measure  `json:"time"`
	Temp  Measure  `json:"temp"`
	Mass  Measure  `json:"mass"`
	Flags []string `json:"flags,omitempty"`
}

// Gap is a declared sampling interval that must never be interpolated.
type Gap struct {
	From Measure `json:"from"`
	To   Measure `json:"to"`
	Note string  `json:"note,omitempty"`
}

// DriftSpec is the balance baseline-drift correction (constant rate model).
type DriftSpec struct {
	Rate     Measure  `json:"rate"` // canonical mg/s
	AnchorAt Measure  `json:"anchorAt"`
	Note     string   `json:"note,omitempty"`
	Points   []struct {
		At     Measure `json:"at"`
		Offset Measure `json:"offset"`
	} `json:"points,omitempty"`
}

// FurnaceSegment is one hold or ramp of the furnace program.
type FurnaceSegment struct {
	Kind     string  `json:"kind"` // hold | ramp
	StartTemp Measure `json:"startTemp"`
	EndTemp   Measure `json:"endTemp"`
	StartTime Measure `json:"startTime"`
	EndTime   Measure `json:"endTime"`
	Rate      Measure `json:"rate,omitempty"` // K/s for ramp
	Raw       string  `json:"raw"`
}

// ValveEvent is one gas-valve switch as recorded (not as experienced by sample).
type ValveEvent struct {
	Time Measure `json:"time"`
	Gas  string  `json:"gas"`
	Flow Measure `json:"flow"` // canonical mL/min, may be invalid
	Note string  `json:"note,omitempty"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	Time Measure `json:"time"`
	Mass Measure `json:"mass"`
}

// InitialMass states sample starting mass and the time coordinate it refers to.
type InitialMass struct {
	Mass    Measure `json:"mass"`
	At      Measure `json:"at"`
	Basis   string  `json:"basis,omitempty"`
	Replaces string `json:"replaces,omitempty"`
}

// SourcePayload is carried by source_submitted events. Accepted==false records
// a half-failed import fragment: raw text and diagnostics are preserved and no
// usable source object enters that layer.
type SourcePayload struct {
	Layer          string           `json:"layer"`
	SourceID       string           `json:"sourceId"`
	ParentSourceID string           `json:"parentSourceId,omitempty"`
	Label          string           `json:"label"`
	RawText        string           `json:"rawText"`
	Accepted       bool             `json:"accepted"`
	Diagnostics    []Diagnostic     `json:"diagnostics,omitempty"`
	Samples        []RawSample      `json:"samples,omitempty"`
	Gaps           []Gap            `json:"gaps,omitempty"`
	Drift          *DriftSpec       `json:"drift,omitempty"`
	Furnace        []FurnaceSegment `json:"furnace,omitempty"`
	Valves         []ValveEvent     `json:"valves,omitempty"`
	Blank          []BlankPoint      `json:"blank,omitempty"`
	InitialMass    *InitialMass     `json:"initialMass,omitempty"`
}

// ProjectPayload creates a project.
type ProjectPayload struct {
	Name   string `json:"name"`
	CaseID string `json:"caseId,omitempty"`
	Seed   int64  `json:"seed,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Params are the tunable derivation parameters.
type Params struct {
	ValveDelayS         float64 `json:"valveDelayS"`
	ReferenceTempLagK   float64 `json:"referenceTempLagK"`
	DTGHalfWindow       int     `json:"dtgHalfWindow"`
	PeakProminenceMgS   float64 `json:"peakProminenceMgS"`
	MinPeakSeparationS  float64 `json:"minPeakSeparationS"`
	NoiseThresholdMgS   float64 `json:"noiseThresholdMgS"`
	StageMarginS        float64 `json:"stageMarginS"`
	BlankExtrapolate    bool    `json:"blankExtrapolate"`
}

// DefaultParams returns the v1 review parameters.
func DefaultParams() Params {
	return Params{
		ValveDelayS:       18,
		ReferenceTempLagK: 6,
		DTGHalfWindow:     2,
		PeakProminenceMgS: 0.0008,
		MinPeakSeparationS: 60,
		NoiseThresholdMgS: 0.0006,
		StageMarginS:      20,
		BlankExtrapolate:  false,
	}
}

// ParamsPayload is carried by params_revised events.
type ParamsPayload struct {
	ParamVersion string  `json:"paramVersion"`
	Parent       string  `json:"parent,omitempty"`
	Params       Params  `json:"params"`
	Reason       string  `json:"reason,omitempty"`
}

// RulingPayload is carried by ruling events. Adjusted carries an optional
// per-candidate parameter adjustment (window override etc.).
type RulingPayload struct {
	RulingID    string             `json:"rulingId"`
	CandidateID string             `json:"candidateId"`
	Decision    string             `json:"decision"` // accept | reject
	Rationale   string             `json:"rationale,omitempty"`
	Adjusted    map[string]float64 `json:"adjusted,omitempty"`
}

// ConflictPayload resolves two clashing rulings on one candidate.
type ConflictPayload struct {
	CandidateID string `json:"candidateId"`
	KeptRuling  string `json:"keptRuling"`
	Note        string `json:"note,omitempty"`
}

// UndoPayload deactivates a prior ruling; the event itself stays in history.
type UndoPayload struct {
	RulingID string `json:"rulingId"`
	Reason   string `json:"reason,omitempty"`
}

// Envelope is the append-only event record. ID is content-derived and does
// NOT depend on Seq, so identity survives export/re-import/resequencing.
type Envelope struct {
	ID      string          `json:"id"`
	Seq     int64           `json:"seq"`
	Project string          `json:"project"`
	Type    string          `json:"type"`
	At      string          `json:"at"`
	Parent  string          `json:"parent"`
	Payload json.RawMessage `json:"payload"`
}

// CanonicalJSON renders data deterministically for hashing.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		panic(err)
	}
	return b
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		// json.Unmarshal of structs into any yields ordered keys already;
		// structs marshal in field order, which is stable.
		return t
	default:
		return v
	}
}

// EventID computes the stable content identity of an event.
func EventID(project, typ, at, parent string, payload any) string {
	sum := sha256.Sum256(nil)
	_ = sum
	h := sha256.New()
	w := json.NewEncoder(h)
	_ = w.Encode(struct {
		Project string `json:"project"`
		Type    string `json:"type"`
		At      string `json:"at"`
		Parent  string `json:"parent"`
		Payload any    `json:"payload"`
	}{project, typ, at, parent, canonicalPayload(payload)})
	return "ev:" + hex.EncodeToString(h.Sum(nil))[:16]
}

func canonicalPayload(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// HashID returns a stable prefixed id for arbitrary canonical content.
func HashID(prefix string, parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write(CanonicalJSON(p))
		_, _ = h.Write([]byte{0})
	}
	return prefix + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}
