// Package domain defines the core, persistence-neutral data model of the
// thermogravimetric analysis (TGA) review system.
//
// Design rules applied throughout the repository:
//   - every domain quantity carries a unit or a coordinate basis;
//   - original textual representations are preserved on import and
//     normalization happens only in the derived layer;
//   - values that cannot be converted stay in place with a Diagnostic and
//     are never silently dropped to make a chart drawable.
package domain

import "time"

// Kind identifies a submitted source document category.
type Kind string

const (
	KindSamples    Kind = "samples"     // raw sampled points (mass, temperature, furnace time)
	KindDrift      Kind = "drift"       // balance drift correction table
	KindFurnace    Kind = "furnace"     // furnace temperature program
	KindGas        Kind = "gas"         // gas valve events
	KindBlank      Kind = "blank"       // blank (empty-pan) curve
	KindInitialMass Kind = "initial_mass" // sample initial mass stratification
)

// SamplePoint is one raw acquisition row. Raw fields keep the original
// representation; Parsed fields hold the normalized, canonical values.
type SamplePoint struct {
	Row       int     `json:"row"`
	TimeRaw   string  `json:"time_raw"`
	MassRaw   string  `json:"mass_raw"`
	TempRaw   string  `json:"temp_raw"`
	TimeS     float64 `json:"time_s"`
	MassMg    float64 `json:"mass_mg"`
	TempC     float64 `json:"temp_c"`
	// Invalid is the list of cell-level diagnostics (bad number, unknown unit,
	// missing required cell). Invalid points are kept, never interpolated away.
	Invalid []Diagnostic `json:"invalid,omitempty"`
}

// CurvePoint is a generic time-series row used by drift, blank and furnace docs.
type CurvePoint struct {
	Row     int          `json:"row"`
	TimeRaw string       `json:"time_raw"`
	ValueRaw string      `json:"value_raw"`
	TimeS   float64      `json:"time_s"`
	Value   float64      `json:"value"` // canonical: mg for drift/blank, °C for furnace
	Invalid []Diagnostic `json:"invalid,omitempty"`
}

// GasEvent is one gas valve switch with its original representation.
type GasEvent struct {
	Row        int          `json:"row"`
	TimeRaw    string       `json:"time_raw"`
	Atmosphere string       `json:"atmosphere"` // e.g. N2, O2, Ar
	Action     string       `json:"action"`     // on|off|switch
	TimeS      float64      `json:"time_s"`
	Invalid    []Diagnostic `json:"invalid,omitempty"`
}

// InitialMass is a stratified initial-mass statement. Revisions are expressed
// as new documents with parent references, never by overwriting a prior one.
type InitialMass struct {
	Row       int          `json:"row"`
	MassRaw   string       `json:"mass_raw"`
	Basis     string       `json:"basis"` // gross|net|tare|pan
	TimeRaw   string       `json:"time_raw,omitempty"`
	TimeS     float64      `json:"time_s,omitempty"`
	MassMg    float64      `json:"mass_mg"`
	Invalid   []Diagnostic `json:"invalid,omitempty"`
}

// Document is an immutable, layered source record.
type Document struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Kind      Kind      `json:"kind"`
	Version   int       `json:"version"`
	ParentID  string    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"` // text/csv | application/json
	Raw       string    `json:"raw"`       // verbatim submitted bytes (UTF-8)
	// DeclaredUnits records the per-column units as submitted; derived layers
	// convert to canonical units (s, mg, °C).
	DeclaredUnits map[string]string `json:"declared_units,omitempty"`
	Samples       []SamplePoint     `json:"samples,omitempty"`
	Curve         []CurvePoint      `json:"curve,omitempty"`
	Gas           []GasEvent        `json:"gas,omitempty"`
	Masses        []InitialMass     `json:"masses,omitempty"`
	Diagnostics  []Diagnostic      `json:"diagnostics,omitempty"`
	SubmittedAt   time.Time         `json:"submitted_at"`
}

// Severity classifies an import/diagnostic outcome.
type Severity string

const (
	SeverityError   Severity = "error"   // cell/file rejected from derivation
	SeverityWarning Severity = "warning" // usable but noteworthy
	SeverityInfo    Severity = "info"
)

// Diagnostic stays attached to the object whose value could not be trusted.
type Diagnostic struct {
	Scope   string   `json:"scope"` // document|row|cell|batch
	Row     int      `json:"row,omitempty"`
	Field   string   `json:"field,omitempty"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Raw     string   `json:"raw,omitempty"`
	Severity Severity `json:"severity"`
}

// Parameters are the algorithm controls. They are versioned independently of
// source documents; revisions create parameter events with a parent chain.
type Parameters struct {
	// AlgorithmVersion identifies the derivation code generation.
	AlgorithmVersion string `json:"algorithm_version"`
	// AtmosphereDelayS shifts gas valve time -> sample reaction time.
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	// TempLagS shifts furnace program time -> measured sample temperature.
	TempLagS float64 `json:"temp_lag_s"`
	// DriftMethod is linear|table|none.
	DriftMethod string `json:"drift_method"`
	// DriftLinearMicrogPerMin applies when DriftMethod == "linear".
	DriftLinearMicrogPerMin float64 `json:"drift_linear_ug_per_min"`
	// SmoothWindow is the Savitzky-Golay-like half-window for DTG noise.
	SmoothWindow int `json:"smooth_window"`
	// MinProminenceMgPerMin is the minimum DTG peak prominence.
	MinProminenceMgPerMin float64 `json:"min_prominence_mg_per_min"`
	// GapThresholdS: a time delta above this is a sampling gap (no interpolation).
	GapThresholdS float64 `json:"gap_threshold_s"`
	// ResidueTailS is the trailing window used for final residue.
	ResidueTailS float64 `json:"residue_tail_s"`
	// BlankCorrection enables empty-pan curve subtraction.
	BlankCorrection bool `json:"blank_correction"`
}

// DefaultParameters returns the seeded algorithm controls.
func DefaultParameters() Parameters {
	return Parameters{
		AlgorithmVersion:        "tg-2026.09.1",
		AtmosphereDelayS:        0,
		TempLagS:                0,
		DriftMethod:             "linear",
		DriftLinearMicrogPerMin: 0,
		SmoothWindow:            2,
		MinProminenceMgPerMin:   0.02,
		GapThresholdS:           20,
		ResidueTailS:            60,
		BlankCorrection:         true,
	}
}
