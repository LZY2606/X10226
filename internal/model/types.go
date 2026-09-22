// Package model holds the domain vocabulary of the TGA review system:
// thermogravimetric sample series, furnace programs, gas valve events,
// blank curves, balance drift corrections and initial-mass declarations.
//
// All domain quantities carry a unit or coordinate basis. Raw textual
// representations are preserved on import; normalization happens only in
// derived layers. Values that cannot be converted stay at their origin
// with a diagnostic instead of being dropped for plotting.
package model

import "time"

// SourceKind is one of the separately preserved input layers.
type SourceKind string

const (
	KindSamples  SourceKind = "samples"  // raw sampled points: time/temperature/mass
	KindDrift    SourceKind = "drift"    // balance drift correction
	KindFurnace  SourceKind = "furnace"  // furnace program segments
	KindGas      SourceKind = "gas"      // gas valve events
	KindBlank    SourceKind = "blank"    // blank (empty-pan) curve
	KindInitial  SourceKind = "initial"  // sample initial mass declarations
	KindParams   SourceKind = "params"   // algorithm parameter set
)

// AllSourceKinds is the fixed import-layering order used across the system.
func AllSourceKinds() []SourceKind {
	return []SourceKind{KindSamples, KindDrift, KindFurnace, KindGas, KindBlank, KindInitial, KindParams}
}

func (k SourceKind) Valid() bool {
	for _, x := range AllSourceKinds() {
		if x == k {
			return true
		}
	}
	return false
}

// Units / coordinate bases used by domain quantities.
const (
	UnitMinute     = "min"   // furnace/program time coordinate
	UnitCelsius    = "degC"  // temperature
	UnitMg         = "mg"    // mass
	UnitPercent    = "%"     // percentage (basis recorded on the value)
	UnitMgPerMin   = "mg/min"
	UnitPercentMin = "%/min"
	BasisInitial   = "initial_mass" // percent of initial sample mass
)

// Project is a review workspace.
type Project struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Batch groups all source documents submitted in one import attempt.
// A half-failed import still records a batch; layers that parsed remain
// visible and the rejected layer keeps a diagnostic.
type Batch struct {
	ID        string
	ProjectID string
	CreatedAt time.Time
	Note      string
	// Status is "committed" when at least one layer parsed, "failed" when none did.
	Status string
}

// RawDoc is the immutable, verbatim submitted document for one layer.
type RawDoc struct {
	VersionID string
	Kind      SourceKind
	Format    string // "csv"
	Raw       string // original text, never overwritten
	SHA256    string
}

// SourceVersion is an immutable version of one input layer.
type SourceVersion struct {
	ID        string
	ProjectID string
	Kind      SourceKind
	ParentID  string // previous version of same kind, "" for first
	BatchID   string
	Revision  int    // 1-based per kind
	CreatedAt time.Time
	// ParseOK is false for a half-failed layer: raw text retained, no rows extracted.
	ParseOK bool
	Diagnostic string
	// Active marks the version used as input for new derived runs.
	Active bool
}

// SamplePoint is one raw sampling row. Raw textual fields are kept so
// unconvertible values stay at origin; parsed fields are 0 with parsed=false.
type SamplePoint struct {
	VersionID string
	Seq       int
	TimeRaw, TempRaw, MassRaw string
	TimeS, TempS, MassS *float64
	// TimeP, TempP, MassP are true when the raw representation parsed into units.
	TimeP, TempP, MassP bool
	UnitTime, UnitTemp, UnitMass string
	RowNote string // e.g. "gap" marker
}

// Gap is an explicit sampling gap declared in the samples layer.
// Gaps are never interpolated across.
type Gap struct {
	VersionID     string
	Seq           int
	FromS, ToS    float64
	Unit          string
	Raw           string
}

// DriftPoint is a balance baseline drift correction node (time, drift mass).
type DriftPoint struct {
	VersionID string
	Seq       int
	TimeRaw, DriftRaw string
	TimeS, DriftS *float64
	TimeP, DriftP bool
	UnitTime, UnitDrift string
}

// FurnaceSeg is one furnace program segment: hold or ramp to target at time t.
type FurnaceSeg struct {
	VersionID string
	Seq       int
	TimeRaw, TempRaw string
	TimeS, TempS *float64
	TimeP, TempP bool
	UnitTime, UnitTemp string
	Kind string // "hold" | "ramp" | "start"
}

// GasEvent is a gas valve switch with the commanded time.
type GasEvent struct {
	VersionID string
	Seq       int
	TimeRaw, GasRaw, ActionRaw string
	TimeS *float64
	TimeP bool
	UnitTime string
	Gas string // N2 | air | O2
	Action string // on | off | switch
}

// BlankPoint is one blank-curve row (time, blank mass change).
type BlankPoint struct {
	VersionID string
	Seq       int
	TimeRaw, MassRaw string
	TimeS, MassS *float64
	TimeP, MassP bool
	UnitTime, UnitMass string
}

// InitialMass is a declared sample initial mass; revisions supersede prior ones.
type InitialMass struct {
	VersionID string
	Seq       int
	MassRaw string
	MassS *float64
	MassP bool
	Unit string
	Basis string // e.g. "tared_balance_reading"
}

// ParamSet holds algorithm parameters (raw CSV key,value kept as well).
type ParamSet struct {
	VersionID string
	Values map[string]string
}
