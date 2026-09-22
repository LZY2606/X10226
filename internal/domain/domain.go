// Package domain defines the immutable TGA review model: raw imported layers,
// derived stage decomposition, decisions and a replayable event ledger.
package domain

import "encoding/json"

// Units used by the system. Raw values retain their original unit string; the
// derived layer normalises to these canonical units.
const (
	UnitSecond = "s"
	UnitMinute = "min"
	UnitCelsius = "degC"
	UnitKelvin = "K"
	UnitMilligram = "mg"
	UnitGram = "g"
	UnitMicrogram = "ug"
	UnitPercent = "%"
	UnitSCCM = "sccm"
	UnitmgPerMin = "mg/min"
	UnitMinPerMin = "min/min"
)

// Layer names identify the six stratified raw inputs.
const (
	LayerSamples   = "samples"   // original sampled mass/temperature points
	LayerDrift     = "drift"     // balance drift corrections
	LayerProgram   = "program"   // furnace program segments
	LayerGasEvents = "gas"       // gas valve events
	LayerBlank     = "blank"     // blank/baseline curve
	LayerInitial   = "initial"   // sample initial mass statements
)

// Layers is the fixed ordering of raw layers.
var Layers = []string{LayerSamples, LayerDrift, LayerProgram, LayerGasEvents, LayerBlank, LayerInitial}

// Status of an imported fragment row.
const (
	StatusOK         = "OK"
	StatusDiagnostic = "DIAGNOSTIC" // retained in place, not convertible/usable
)

// Quantity is a raw value exactly as imported: its textual representation is
// never lost. If it cannot be converted, Value carries NaN-ish sentinel and
// Diagnostic explains why; it stays in place and is never dropped for plots.
type Quantity struct {
	Raw        string  `json:"raw"`
	Unit       string  `json:"unit"`
	Value      float64 `json:"value"`
	Canonical  string  `json:"canonical_unit,omitempty"`
	ConvertedValue float64 `json:"converted_value"`
	Convertible bool   `json:"convertible"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// TimePoint is one original sampled point with furnace time, mass, temperature.
type TimePoint struct {
	ID      string   `json:"id"`
	Ordinal int      `json:"ordinal"`
	T       Quantity `json:"t"`
	Mass    Quantity `json:"mass"`
	Temp    Quantity `json:"temp"`
}

// DriftCorrection is a balance baseline drift statement at a time.
type DriftCorrection struct {
	ID      string   `json:"id"`
	Ordinal int      `json:"ordinal"`
	T       Quantity `json:"t"`
	Offset  Quantity `json:"offset"` // mass correction added to reading
	Kind    string   `json:"kind"`   // e.g. "linear", "point"
}

// ProgramSegment is one furnace program segment (target temperature, rate).
type ProgramSegment struct {
	ID        string   `json:"id"`
	Ordinal   int      `json:"ordinal"`
	TStart    Quantity `json:"t_start"`
	TEnd      Quantity `json:"t_end"`
	TempStart Quantity `json:"temp_start"`
	TempEnd   Quantity `json:"temp_end"`
	Rate      Quantity `json:"rate"`
	Label     string   `json:"label"`
}

// GasEvent is one gas valve switch at the valve. Reaction timing at the sample
// is derived later with delay/lag; valve time never equals reaction time.
type GasEvent struct {
	ID      string   `json:"id"`
	Ordinal int      `json:"ordinal"`
	T       Quantity `json:"t"`
	FromGas string   `json:"from_gas"`
	ToGas   string   `json:"to_gas"`
	Flow    Quantity `json:"flow"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	ID      string   `json:"id"`
	Ordinal int      `json:"ordinal"`
	T       Quantity `json:"t"`
	Mass    Quantity `json:"mass"`
}

// InitialMass is one statement of sample initial mass. Revisions add new rows
// with a parent id; earlier statements are never overwritten.
type InitialMass struct {
	ID       string   `json:"id"`
	ParentID string   `json:"parent_id,omitempty"`
	Ordinal  int      `json:"ordinal"`
	Mass     Quantity `json:"mass"`
	Reason   string   `json:"reason"`
}

// Batch groups raw fragments imported in one submission. A half-failed import
// has AcceptedLayers and RejectedLayers populated and is never partial inside
// one layer.
type Batch struct {
	ID              string          `json:"id"`
	CreatedAt       string          `json:"created_at"`
	Kind            string          `json:"kind"` // "layer" | "bundle"
	SourceRef       string          `json:"source_ref"`
	Payload         json.RawMessage `json:"payload,omitempty"` // original bytes retained
	AcceptedLayers  []string        `json:"accepted_layers"`
	RejectedLayers  []RejectedLayer `json:"rejected_layers"`
	Note            string          `json:"note,omitempty"`
}

// RejectedLayer records why a layer within a multi-layer import was not
// accepted; the other layers can still land.
type RejectedLayer struct {
	Layer  string `json:"layer"`
	Reason string `json:"reason"`
}
