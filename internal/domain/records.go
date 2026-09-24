package domain

// Sample describes one specimen run.
type Sample struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"`
}

// RawPoint is an original sampling point, stored in its imported units and with
// its original textual representation. Values that failed conversion remain in
// place (MassQ/TempQ carry diagnostics) and are never silently dropped.
type RawPoint struct {
	ID       string `json:"id"`
	SampleID string `json:"sample_id"`
	Seq      int    `json:"seq"`
	TimeQ    Q      `json:"time"`    // furnace clock time, UnitTimeMin canonical
	MassQ    Q      `json:"mass"`    // balance reading, UnitMassMG canonical
	TempQ    Q      `json:"temp"`    // furnace measured temperature, UnitTempC canonical
	TimeBasis string `json:"time_basis"`
	GapAfter bool   `json:"gap_after,omitempty"` // declared sampling gap after this point
}

// DriftModel is a balance baseline-drift correction (linear in furnace time).
// It is a distinct input layer, never overwritten in place.
type DriftModel struct {
	ID            string `json:"id"`
	SampleID      string `json:"sample_id"`
	Version       string `json:"version"`
	ParentVersion string `json:"parent_version,omitempty"`
	SlopeQ        Q      `json:"slope"`  // mg/min
	InterceptQ    Q      `json:"intercept"` // mg at t=0
	AppliesFromQ  Q      `json:"applies_from,omitempty"` // min; empty => whole run
	AppliesToQ    Q      `json:"applies_to,omitempty"`
	Note          string `json:"note,omitempty"`
}

// FurnaceSegment is one programmed temperature ramp/isothermal step.
type FurnaceSegment struct {
	ID          string `json:"id"`
	FromTimeQ   Q      `json:"from_time"` // min
	ToTimeQ     Q      `json:"to_time"`
	FromTempQ   Q      `json:"from_temp"` // degC
	ToTempQ     Q      `json:"to_temp"`
	Kind        string `json:"kind"` // ramp | hold
}

// GasValveEvent is a commanded valve switch on the valve clock.
type GasValveEvent struct {
	ID         string `json:"id"`
	AtTimeQ    Q      `json:"at_time"` // min, BasisValve
	FromGas    string `json:"from_gas"`
	ToGas      string `json:"to_gas"`
	FlowQ      Q      `json:"flow"` // mL/min
	Note       string `json:"note,omitempty"`
}

// BlankPoint is one point of the blank/baseline curve (empty pan run).
type BlankPoint struct {
	ID      string `json:"id"`
	TimeQ   Q      `json:"time"` // min
	MassQ   Q      `json:"mass"` // mg, blank-pan balance reading
}

// InitialMassLayer stores sample initial mass as its own stratified layer
// (declared value and the estimator window). Revisions create new versions.
type InitialMassLayer struct {
	ID            string `json:"id"`
	SampleID      string `json:"sample_id"`
	Version       string `json:"version"`
	ParentVersion string `json:"parent_version,omitempty"`
	DeclaredMassQ Q      `json:"declared_mass"` // mg
	EstWindowFrom Q      `json:"est_window_from"` // min
	EstWindowTo   Q      `json:"est_window_to"`
	Source        string `json:"source"` // initial_layer | revision
	Note          string `json:"note,omitempty"`
}
