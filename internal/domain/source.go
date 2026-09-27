package domain

import "time"

// Raw source kinds. Each keeps its own layer; none may overwrite another.
const (
	SourceSamples  = "samples"
	SourceDrift    = "balance_drift"
	SourceFurnace  = "furnace_program"
	SourceGas      = "gas_valve_events"
	SourceBlank    = "blank_curve"
	SourceInitial  = "initial_mass"
	SourceParams   = "analysis_params"
)

// SourceKinds is the import order/layer list.
var SourceKinds = []string{
	SourceSamples, SourceDrift, SourceFurnace,
	SourceGas, SourceBlank, SourceInitial, SourceParams,
}

// RawDoc is an immutable submitted source document. RawRows is the exact
// parsed CSV representation; Diags holds per-cell diagnostics; Rejected
// documents stay in the log as import-half-failure evidence.
type RawDoc struct {
	Kind       string      `json:"kind"`
	Version    int         `json:"version"`
	Parent     string      `json:"parent_id,omitempty"`
	Format     string      `json:"format"`           // "csv"
	Header     []string    `json:"header"`
	RawRows    [][]string  `json:"raw_rows"`
	Notes      string      `json:"notes,omitempty"`
	Diags      []Diag      `json:"diags,omitempty"`
	Rejected   bool        `json:"rejected,omitempty"`
	RejectCode string      `json:"reject_code,omitempty"`
	Submitted  time.Time   `json:"submitted_at"`
	Filename   string      `json:"filename,omitempty"`
	// Typed carries the parsed, domain-typed view (nil when rejected).
	Typed any `json:"typed,omitempty"`
}

// SourceID is the stable identity of a submitted source version.
func (d RawDoc) SourceID() string {
	return ShortID("src_", d.Kind, d.Version, d.Header, d.RawRows, d.Notes, d.Rejected, d.RejectCode, d.Format)
}

// SamplePoint is one raw balance acquisition row.
type SamplePoint struct {
	Row          int      `json:"row"`
	FurnaceTimeS Quantity `json:"furnace_time_s"`
	RawMassMg    Quantity `json:"raw_mass_mg"`
	SampleTempC  Quantity `json:"sample_temp_c"`
}

// SamplesDoc is the raw sampling layer.
type SamplesDoc struct {
	Points []SamplePoint `json:"points"`
}

// DriftDoc is the linear balance baseline drift correction layer, anchored at
// the first sample time: m_corrected = m_raw - (slope*(t-t0)+offset).
type DriftDoc struct {
	OffsetMg Quantity `json:"offset_mg"`
	Slope    Quantity `json:"slope_mg_per_s"`
	AnchorTS Quantity `json:"anchor_furnace_time_s"`
	Enabled  bool     `json:"enabled"`
	BlankRef string   `json:"blank_ref,omitempty"`
}

// FurnaceStep is one programmed segment.
type FurnaceStep struct {
	Row       int      `json:"row"`
	StartTS   Quantity `json:"start_furnace_time_s"`
	EndTS     Quantity `json:"end_furnace_time_s"`
	StartTemp Quantity `json:"start_temp_C"`
	EndTemp   Quantity `json:"end_temp_C"`
	RampRate  Quantity `json:"ramp_C_per_min"`
}

// FurnaceDoc describes the furnace program on the furnace clock.
type FurnaceDoc struct {
	Steps []FurnaceStep `json:"steps"`
}

// GasValveEvent is one gas valve switch (furnace clock).
type GasValveEvent struct {
	Row       int      `json:"row"`
	AtTS      Quantity `json:"at_furnace_time_s"`
	FromGas   string   `json:"from_gas"`
	ToGas     string   `json:"to_gas"`
	FlowMLMin Quantity `json:"flow_mL_per_min"`
	Note      string   `json:"note,omitempty"`
}

// GasDoc is the gas valve timeline layer.
type GasDoc struct {
	Events []GasValveEvent `json:"events"`
}

// BlankPoint is one blank (empty-pan) reference acquisition.
type BlankPoint struct {
	Row         int      `json:"row"`
	FurnaceTime Quantity `json:"furnace_time_s"`
	MassMg      Quantity `json:"mass_mg"`
	TempC       Quantity `json:"temp_c"`
}

// BlankDoc is the blank curve layer (diagnostic reference).
type BlankDoc struct {
	Points []BlankPoint `json:"points"`
}

// InitialMassDoc is the stratified initial weighing layer. Revision is
// expressed as a new version with a parent, never by overwriting.
type InitialMassDoc struct {
	MassMg        Quantity `json:"mass_mg"`
	BalanceID     string   `json:"balance_id"`
	WeighingBasis string   `json:"weighing_basis"` // e.g. "wet", "dry"
	RevisedFrom   string   `json:"revised_from,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// ParamsDoc wraps analysis parameters as a versioned source layer.
type ParamsDoc struct {
	Params Params `json:"params"`
}
