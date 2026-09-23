// Package domain holds the core, storage-free types of the TGA evidence system.
//
// Layering contract (see docs/case-narrative.md):
//   - Raw layer: original textual representation is always preserved; rows that
//     cannot be normalized are retained in place with diagnostics.
//   - Derived layer: every value carries a unit or coordinate basis and knows
//     its algorithm version, parameter fingerprint and direct sources.
package domain

// SourceType identifies one of the seven stratification layers an import
// bundle is composed of.
type SourceType string

const (
	SrcTG         SourceType = "tg_samples"       // raw sampled points (mass vs furnace time)
	SrcDrift      SourceType = "drift_correction" // balance drift correction
	SrcProgram    SourceType = "furnace_program"  // furnace temperature program
	SrcGasEvent   SourceType = "gas_valve_event"  // gas valve switch events
	SrcBlank      SourceType = "blank_curve"      // blank run curve
	SrcInitial    SourceType = "initial_mass"     // sample initial mass
	SrcReplayNote SourceType = "replay_note"      // optional machine-readable note
)

// SourceTypes is the canonical ordered list used for deterministic ordering.
var SourceTypes = []SourceType{
	SrcTG, SrcDrift, SrcProgram, SrcGasEvent, SrcBlank, SrcInitial, SrcReplayNote,
}

// Quantity is a physical value carrying the unit in which it was expressed.
// Raw is the exact original token (e.g. "10,25 mg" style input is kept by the
// parser); Value is set only after a lossless SI-compatible normalization.
type Quantity struct {
	Value float64 `json:"value,omitempty"`
	Unit  string  `json:"unit,omitempty"`
	Raw   string  `json:"raw"`
	Valid bool    `json:"valid"`
}

// Diagnostic explains why a raw row could not be normalized. The row stays in
// place; nothing is dropped for plotting convenience.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// RawRow is one line of an imported file, kept verbatim with its parse result.
type RawRow struct {
	Line        int               `json:"line"`
	Raw         string            `json:"raw"`
	Accepted    bool              `json:"accepted"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// Source is one versioned, immutable layer of an import.
type Source struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Type       SourceType `json:"type"`
	Version    int        `json:"version"`
	ParentID   string     `json:"parent_id,omitempty"`
	Filename   string     `json:"filename"`
	MediaType  string     `json:"media_type"`
	Encoding   string     `json:"encoding"`
	ContentSHA string     `json:"content_sha"`
	Size       int64      `json:"size"`
	Partial    bool       `json:"partial"`
	CreatedAt  string     `json:"created_at"`
	Note       string     `json:"note,omitempty"`
}

// TGSample is one normalized raw TG sampling point.
type TGSample struct {
	ID       string   `json:"id"`
	Line     int      `json:"line"`
	TimeS    Quantity `json:"time_s"`
	TempC    Quantity `json:"temp_c"`
	MassMg   Quantity `json:"mass_mg"`
	Accepted bool     `json:"accepted"`
}

// DriftPoint is a balance-baseline drift correction anchor.
type DriftPoint struct {
	ID       string   `json:"id"`
	Line     int      `json:"line"`
	TimeS    Quantity `json:"time_s"`
	OffsetMg Quantity `json:"offset_mg"`
	Accepted bool     `json:"accepted"`
}

// BlankPoint is one point of the blank (empty pan) curve.
type BlankPoint struct {
	ID       string   `json:"id"`
	Line     int      `json:"line"`
	TimeS    Quantity `json:"time_s"`
	MassMg   Quantity `json:"mass_mg"`
	Accepted bool     `json:"accepted"`
}

// FurnaceSegment is one programmed furnace ramp/hold segment.
type FurnaceSegment struct {
	ID        string   `json:"id"`
	Line      int      `json:"line"`
	FromS     Quantity `json:"from_s"`
	ToS       Quantity `json:"to_s"`
	FromTempC Quantity `json:"from_temp_c"`
	ToTempC   Quantity `json:"to_temp_c"`
	Kind      string   `json:"kind"`
	Accepted  bool     `json:"accepted"`
}

// GasValveEvent is one valve switch (instrument log time).
type GasValveEvent struct {
	ID       string   `json:"id"`
	Line     int      `json:"line"`
	TimeS    Quantity `json:"time_s"`
	FromGas  string   `json:"from_gas"`
	ToGas    string   `json:"to_gas"`
	FlowSccm Quantity `json:"flow_sccm"`
	Accepted bool     `json:"accepted"`
}

// InitialMass is a versioned sample initial mass declaration.
type InitialMass struct {
	ID       string   `json:"id"`
	Line     int      `json:"line"`
	MassMg   Quantity `json:"mass_mg"`
	Basis    string   `json:"basis"`
	Accepted bool     `json:"accepted"`
}

// ParameterFingerprint bundles an algorithm version with a canonical
// fingerprint of its parameter set.
type ParameterFingerprint struct {
	AlgorithmVersion string `json:"algorithm_version"`
	Fingerprint      string `json:"fingerprint"`
	ParamsJSON       string `json:"params_json"`
}
