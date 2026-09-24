// Package domain contains the core value types for the TGA review system.
//
// Every measured or derived physical quantity travels as a Q: it keeps the
// raw textual representation that was imported, a normalised value with a
// declared unit, and a coordinate basis (elapsed furnace time vs wall clock,
// temperature program vs measured temperature). Values that cannot be
// converted stay in place with a diagnostic instead of being dropped.
package domain

import "time"

// Coordinate bases used by the system.
const (
	BasisFurnaceTime     = "furnace_time_s"      // elapsed time inside the furnace program, seconds
	BasisWallClock       = "wall_clock"          // operator's wall clock timestamp
	BasisTemperatureMeas = "temperature_meas_c"  // thermocouple measured temperature
	BasisTemperatureProg = "temperature_prog_c"  // programmed furnace temperature
)

// Units.
const (
	UnitMilligram = "mg"
	UnitGram      = "g"
	UnitCelsius   = "degC"
	UnitKelvin    = "K"
	UnitSecond    = "s"
	UnitMinute    = "min"
	UnitPercent   = "%"
	UnitMGPermin = "mg/min"
)

// Q is a quantity with unit, coordinate basis, raw representation and provenance.
type Q struct {
	Raw      string  `json:"raw"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Basis    string  `json:"basis"`
	Valid    bool    `json:"valid"`
	Diag     string  `json:"diag,omitempty"`
	SourceID string  `json:"source_id,omitempty"`
	Line     int     `json:"line,omitempty"`
}

// Source kinds: the six layers that an import is stratified into.
const (
	KindSamples  = "samples"
	KindDrift    = "balance_drift"
	KindProgram  = "furnace_program"
	KindGas      = "gas_valve_events"
	KindBlank    = "blank_curve"
	KindInitialM = "initial_mass"
)

// SourceKinds lists every import layer in canonical order.
var SourceKinds = []string{KindSamples, KindDrift, KindProgram, KindGas, KindBlank, KindInitialM}

// SamplePoint is one raw TG sampling row.
type SamplePoint struct {
	Index int    `json:"index"`
	Line  int    `json:"line"`
	Time  Q      `json:"time"`
	Temp  Q      `json:"temp"`
	Mass  Q      `json:"mass"`
	Kept  bool   `json:"kept"`
	Diag  string `json:"diag,omitempty"`
}

// DriftPoint describes the balance baseline drift calibration (time, offset in mg).
type DriftPoint struct {
	Line   int    `json:"line"`
	Time   Q      `json:"time"`
	Offset Q      `json:"offset"`
}

// ProgramSegment is one furnace program segment.
type ProgramSegment struct {
	Line      int    `json:"line"`
	StartTemp Q      `json:"start_temp"`
	EndTemp   Q      `json:"end_temp"`
	Rate      Q      `json:"rate"`
	Duration  Q      `json:"duration"`
}

// GasEvent is a valve switch event.
type GasEvent struct {
	Line      int    `json:"line"`
	Time      Q      `json:"time"`
	FromGas   string `json:"from_gas"`
	ToGas     string `json:"to_gas"`
	Raw       string `json:"raw"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	Line int    `json:"line"`
	Time Q      `json:"time"`
	Mass Q      `json:"mass"`
}

// InitialMassLayer holds every declared sample initial mass (stratified, never overwritten).
type InitialMassLayer struct {
	Line    int    `json:"line"`
	Version int    `json:"version"`
	Mass    Q      `json:"mass"`
	Note    string `json:"note,omitempty"`
	Active  bool   `json:"active"`
}

// Diagnostics is a non-fatal problem found while parsing or normalising.
type Diagnostics struct {
	Line    int    `json:"line,omitempty"`
	Layer   string `json:"layer"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// ImportedLayers is one versioned snapshot of what an import produced.
type ImportedLayers struct {
	Samples   []SamplePoint       `json:"samples"`
	Drift     []DriftPoint        `json:"drift"`
	Program   []ProgramSegment    `json:"program"`
	Gas       []GasEvent          `json:"gas"`
	Blank     []BlankPoint        `json:"blank"`
	InitialM  []InitialMassLayer  `json:"initial_mass"`
	Diags     []Diagnostics       `json:"diags"`
}

// Event kinds written into the append-only log.
const (
	EvtProjectCreated  = "project.created"
	EvtSourceCommitted = "source.committed"
	EvtParamsRevised   = "params.revised"
	EvtAnalysisRun     = "analysis.run"
	EvtDecisionMade    = "decision.made"
	EvtDecisionUndone  = "decision.undone"
	EvtConflictResolved= "conflict.resolved"
	EvtProjectDeleted  = "project.deleted"
)

// Event is one immutable entry in the project journal.
type Event struct {
	Seq       int64     `json:"seq"`
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	Actor     string    `json:"actor"`
	ParentID  string    `json:"parent_id"`
	Payload   []byte    `json:"payload"`
	Hash      string    `json:"hash"`
}

// AlgorithmVersion is pinned on every derived value.
const AlgorithmVersion = "tga-pipeline-1.0.0"

// AnalysisParams are the tunable parameters.
type AnalysisParams struct {
	GapThresholdS      float64 `json:"gap_threshold_s"`
	GasDelayS          float64 `json:"gas_delay_s"`
	TempLagS           float64 `json:"temp_lag_s"`
	DTGWindowS         float64 `json:"dtg_window_s"`
	PeakProminenceMGPM float64 `json:"peak_prominence_mgpm"`
	NoiseThresholdMG   float64 `json:"noise_threshold_mg"`
	ShoulderDepthMGPM  float64 `json:"shoulder_depth_mgpm"`
	BlankSubtract      bool    `json:"blank_subtract"`
}

// Finding is a machine-readable judgement emitted by the pipeline.
type Finding struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	StartS   float64 `json:"start_s,omitempty"`
	EndS     float64 `json:"end_s,omitempty"`
	Ref      string  `json:"ref,omitempty"`
}
