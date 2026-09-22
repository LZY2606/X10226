// Package domain contains the language of the TGA evidence chain: the six
// immutable raw layers, the derived layers, decisions and the event ledger.
//
// Layering rule (see cases/tga-lineage):
//
//	layer 1 raw submissions: sample points, balance drift, furnace program,
//	    gas valve events, blank curve, initial mass  (never overwritten;
//	    revisions are expressed as new versions linked by parent_version)
//	layer 2 normalization/correction (drift-corrected mass, furnace timeline,
//	    gas timeline with reaction-time estimates)
//	layer 3 analysis (TG/DTG series, stage candidate set, residual rates)
//
// Every derived value carries algorithm version, parameter fingerprint and
// the stable IDs of its direct sources.
package domain

// Kind is the layer/entity kind of a record.
type Kind string

const (
	KindRawBundle     Kind = "raw_bundle"     // an import batch submission
	KindSamplePoints  Kind = "sample_points"  // raw sampling points
	KindDrift         Kind = "drift"          // balance drift correction spec
	KindFurnace       Kind = "furnace"        // furnace program
	KindGasEvents     Kind = "gas_events"     // gas valve switch events
	KindBlankCurve    Kind = "blank_curve"    // blank run curve
	KindInitialMass   Kind = "initial_mass"   // stratified initial sample mass
	KindParams        Kind = "params"         // algorithm parameter revision
	KindCorrectedMass Kind = "corrected_mass"
	KindFurnaceTL     Kind = "furnace_timeline"
	KindGasTL         Kind = "gas_timeline"
	KindTGSeries      Kind = "tg_series"
	KindStages        Kind = "stages"
	KindResidual      Kind = "residual"
	KindDecision      Kind = "decision"
)

// Status marks the lifecycle state of derived results.
type Status string

const (
	StatusCurrent    Status = "current"    // recomputed against the newest sources
	StatusStale      Status = "stale"      // a direct source was superseded
	StatusSuperseded Status = "superseded" // replaced by a newer computation
	StatusUnknown    Status = "unknown"    // cannot be uniquely determined
	StatusError      Status = "error"      // computation failed with diagnostics
)

// AlgoVersion tags each piece of derived data with the algorithm that made it.
const (
	AlgoDriftCorrection = "drift-correction/v1"
	AlgoFurnaceTimeline = "furnace-timeline/v1"
	AlgoGasTimeline     = "gas-timeline/v1"
	AlgoTGSeries        = "tg-series/v1"
	AlgoStages          = "stage-decomposition/v1"
	AlgoResidual        = "residual-rate/v1"
)

// Diagnostic is a machine-readable problem attached to an import or a derived
// value. Unconvertible input never silently disappears: it stays at its origin
// and carries a diagnostic instead.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "error" | "warning"
	Layer    Kind   `json:"layer"`
	Ref      string `json:"ref,omitempty"` // e.g. row:12 or point index
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
}

// Q is a quantity that always remembers its original textual representation
// and unit/coordinate basis. Raw layer stores it verbatim; only the derived
// layer fills Value via normalization. Unconvertible values keep Value=nil.
type Q struct {
	Raw   string   `json:"raw"`
	Value *float64 `json:"value,omitempty"`
	Unit  string   `json:"unit,omitempty"`
}

// Num is a convenience constructor for an already numeric quantity.
func Num(v float64, unit string) Q {
	vv := v
	return Q{Raw: formatFloat(v), Value: &vv, Unit: unit}
}

func (q Q) Valid() bool { return q.Value != nil }

// SamplePoint is one raw sampling record. Each field keeps its raw text; an
// unparseable field stays in place with a nil value plus an import diagnostic.
type SamplePoint struct {
	Index int  `json:"index"`
	Time  Q    `json:"time_s"`
	Temp  Q    `json:"temp_c"`
	Mass  Q    `json:"mass_mg"`
	Gap   bool `json:"gap,omitempty"` // marks the first point after a sampling gap
}

type SamplePoints struct {
	Points []SamplePoint `json:"points"`
	// GapIntervals are raw-index ranges [start,end) with no acquired points.
	// Derived curves never interpolate across these intervals.
	GapIntervals [][2]int `json:"gap_intervals,omitempty"`
}

// DriftSegment describes one segment of the balance baseline drift model,
// given in furnace time: mass_bias(t) = slope*(t-t0)+offset.
type DriftSegment struct {
	T0     Q `json:"t0_s"`
	Offset Q `json:"offset_mg"`
	Slope  Q `json:"slope_mg_per_s"`
	Until  Q `json:"until_s"`
}

type Drift struct {
	Source   string        `json:"source"`             // e.g. "blank_curve" | "manual"
	Segments []DriftSegment `json:"segments"`
}

// FurnaceSegment is one programmed ramp or hold in furnace/program time.
type FurnaceSegment struct {
	Index  int    `json:"index"`
	Kind   string `json:"kind"` // "ramp" | "hold"
	T0     Q      `json:"t0_s"`
	T1     Q      `json:"t1_s"`
	Temp0  Q      `json:"temp0_c"`
	Temp1  Q      `json:"temp1_c"`
	Rate   Q      `json:"rate_c_per_min,omitempty"`
}

type Furnace struct {
	Program []FurnaceSegment `json:"program"`
}

// GasEvent is one valve switch as recorded by the instrument clock.
type GasEvent struct {
	Index  int    `json:"index"`
	Time   Q      `json:"valve_time_s"`
	FromAt string `json:"from_atmosphere"`
	ToAt   string `json:"to_atmosphere"`
	Note   string `json:"note,omitempty"`
}

type GasEvents struct {
	Events []GasEvent `json:"events"`
}

// BlankPoint / BlankCurve is the empty-pan run used to characterize drift.
type BlankPoint struct {
	Time Q `json:"time_s"`
	Mass Q `json:"mass_mg"`
}

type BlankCurve struct {
	Points []BlankPoint `json:"points"`
}

// InitialMass is the stratified record of the starting sample mass.
type InitialMass struct {
	Mass        Q      `json:"mass_mg"`
	Basis       string `json:"basis"`          // "as-loaded" | "tare-corrected"
	RevisionOf  string `json:"revision_of,omitempty"`
	Reason      string `json:"reason,omitempty"`
}
