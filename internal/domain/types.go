// Package domain defines the immutable, source-faithful TGA audit model.
//
// The package deliberately separates raw representations (exactly as the
// thermal analysis team typed them) from normalized representations used by
// derived layers. Values that cannot be converted are never dropped: they stay
// attached to their record with a diagnostic.
package domain

import "time"

// Kind identifies one of the layered source streams imported into a project.
type Kind string

const (
	KindPoints  Kind = "points"  // raw sampled mass/temperature points
	KindDrift   Kind = "drift"   // balance baseline drift correction model
	KindFurnace Kind = "furnace" // furnace program (set-point segments)
	KindGas     Kind = "gas"     // gas-valve events
	KindBlank   Kind = "blank"   // blank-curve points
	KindM0      Kind = "m0"      // initial sample mass, stratified by analyst
)

// AllKinds is the canonical import-layer order shown on the review page.
var AllKinds = []Kind{KindPoints, KindDrift, KindFurnace, KindGas, KindBlank, KindM0}

// Dimension of a measured quantity. Every displayed value carries a unit or a
// coordinate basis, so dimensions are first-class data.
type Dimension string

const (
	DimMass        Dimension = "mass"
	DimTemperature Dimension = "temperature"
	DimTime        Dimension = "time"
	DimPercent     Dimension = "percent" // residue / mass-loss percentages
	DimGasFlow     Dimension = "gas_flow"
	DimNone        Dimension = "none"
)

// CanonicalUnit returns the SI/internal unit a dimension normalizes to.
func CanonicalUnit(d Dimension) string {
	switch d {
	case DimMass:
		return UnitMg
	case DimTemperature:
		return UnitC
	case DimTime:
		return UnitS
	case DimPercent:
		return UnitPct
	case DimGasFlow:
		return UnitMLMin
	default:
		return ""
	}
}

// Known units.
const (
	UnitMg    = "mg"
	UnitUg    = "ug"
	UnitG     = "g"
	UnitC     = "C"
	UnitK     = "K"
	UnitS     = "s"
	UnitMin   = "min"
	UnitPct   = "%"
	UnitMLMin = "mL/min"
)

// RawValue preserves the original textual representation of a measurement
// alongside its normalized value. When conversion fails Normalized is false,
// Numeric holds the zero value and Diagnostic explains why; the raw token is
// retained in place and never removed "to make the chart work".
type RawValue struct {
	Raw        string    `json:"raw"`
	Unit       string    `json:"unit"`
	Dim        Dimension `json:"dim"`
	Numeric    float64   `json:"numeric"`
	Normalized bool      `json:"normalized"`
	Diagnostic string    `json:"diagnostic,omitempty"`
}

// IsUsable reports whether derived layers may consume the value numerically.
func (v RawValue) IsUsable() bool { return v.Normalized }

// SamplePoint is one raw sampled observation from the balance/thermocouple.
type SamplePoint struct {
	Index    int      `json:"index"`
	Elapsed  RawValue `json:"elapsed"`   // furnace time coordinate
	FurnaceT RawValue `json:"furnace_t"` // measured furnace/set temperature
	SampleT  RawValue `json:"sample_t"`  // sample temperature (may lag furnace)
	Mass     RawValue `json:"mass"`      // balance reading, drift NOT yet removed
	RawLine  string   `json:"raw_line,omitempty"`
}

// DriftModel is the analyst's balance baseline drift correction. It is applied
// as corrected = mass - (slope*elapsed_s + intercept_mg).
type DriftModel struct {
	Index       int      `json:"index"`
	Slope       RawValue `json:"slope"`        // mg/s
	Intercept   RawValue `json:"intercept"`    // mg
	EffectiveAt RawValue `json:"effective_at"` // furnace time s
	Basis       string   `json:"basis"`        // analyst note, retained verbatim
}

// FurnaceSegment is one set-point segment of the furnace program.
type FurnaceSegment struct {
	Index    int      `json:"index"`
	Start    RawValue `json:"start"`     // segment start, furnace time s
	End      RawValue `json:"end"`       // segment end, furnace time s
	StartT   RawValue `json:"start_t"`   // deg C
	EndT     RawValue `json:"end_t"`     // deg C
	RampRate RawValue `json:"ramp_rate"` // deg C / min
}

// GasEvent is a gas-valve switching command as issued to the manifold.
type GasEvent struct {
	Index   int      `json:"index"`
	At      RawValue `json:"at"` // valve command time (furnace time s)
	FromGas string   `json:"from_gas"`
	ToGas   string   `json:"to_gas"`
	Flow    RawValue `json:"flow"` // mL/min
	Note    string   `json:"note,omitempty"`
}

// BlankPoint is one point of the empty-pan blank run used for drift checks.
type BlankPoint struct {
	Index   int      `json:"index"`
	Elapsed RawValue `json:"elapsed"`
	Mass    RawValue `json:"mass"`
}

// M0Estimate is one stratified initial-mass determination. Multiple analysts
// may submit m0; revisions supersede earlier ones via parent-version links.
type M0Estimate struct {
	Index   int      `json:"index"`
	Analyst string   `json:"analyst"`
	Mass    RawValue `json:"mass"`
	At      RawValue `json:"at"`
	Basis   string   `json:"basis"`
}

// Diagnostic is an import- or normalization-level problem attached to a
// record without removing the record itself.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
	Field   string `json:"field,omitempty"`
}

// ImportedRecord is one source record exactly as it arrived (or failed to
// arrive) for a given stream version.
type ImportedRecord struct {
	Key         string            `json:"key"`
	Seq         int               `json:"seq"`
	Accepted    bool              `json:"accepted"`
	Typed       any               `json:"typed,omitempty"`
	Raw         map[string]string `json:"raw"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
}

// SourceBundle is one submitted batch across any subset of the six streams.
type SourceBundle struct {
	Label   string                    `json:"label"`
	Streams map[Kind][]ImportedRecord `json:"streams"`
	// SemiFailure records rows rejected at import. They are retained verbatim
	// and block dependent derivation, but do not abort the whole import.
	Summary string `json:"summary,omitempty"`
}

// Clock is the virtual time source used everywhere so replay is deterministic.
type Clock interface {
	Now() time.Time
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// NewFixedClock returns a Clock frozen at t (used by tests and replay).
func NewFixedClock(t time.Time) Clock { return fixedClock{t: t} }
