// Package domain defines the immutable language of the TGA evidence line:
// raw inputs, versioned parameters, content-addressed derived nodes and
// machine-readable basis events.
package domain

// Layer is a raw input stratum. Raw layers are append-only: a revision
// creates a new version that points at its parent version.
type Layer string

const (
	LayerSamples     Layer = "samples"     // mass vs furnace time (TG trace, raw sampling points)
	LayerTemperature Layer = "temperature" // temperature vs furnace time
	LayerProgram     Layer = "program"     // furnace program segments
	LayerValves      Layer = "valves"      // gas valve events
	LayerBlank       Layer = "blank"       // blank/baseline curve
	LayerDrift       Layer = "drift"       // balance drift correction
	LayerInitialMass Layer = "initial_mass"
	LayerParameters  Layer = "parameters" // versioned analysis parameters
)

func (l Layer) Valid() bool {
	switch l {
	case LayerSamples, LayerTemperature, LayerProgram, LayerValves,
		LayerBlank, LayerDrift, LayerInitialMass, LayerParameters:
		return true
	}
	return false
}

// Kind discriminates persisted documents.
type Kind string

const (
	KindRaw        Kind = "raw"      // unparsed raw submission text
	KindParsed     Kind = "parsed"   // parse result, possibly with diagnostics
	KindParameters Kind = "parameters"
)

// Document is an immutable, content-addressed raw or parsed input record.
type Document struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Layer    Layer  `json:"layer"`
	Version  int    `json:"version"`  // 1-based within (project, layer)
	ParentID string `json:"parentId"` // previous version, empty for v1
	Kind     Kind   `json:"kind"`
	Encoding string `json:"encoding"` // "text/csv", "text/plain", "application/json"
	RawText  string `json:"rawText"`  // verbatim representation as imported
	Note     string `json:"note,omitempty"`
}

// Diagnostic keeps an unconvertible value at its original position instead of
// silently dropping it to make the plot draw.
type Diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Layer    Layer  `json:"layer,omitempty"`
	Row      int    `json:"row,omitempty"`
	Column   string `json:"column,omitempty"`
	Original string `json:"original,omitempty"`
}

// RawCell preserves the original textual representation and coordinate basis of
// one imported field.
type RawCell struct {
	Column   string `json:"column"`
	Original string `json:"original"`
	Unit     string `json:"unit,omitempty"`
	Basis    string `json:"basis,omitempty"`
}

// RawRow is one source record with every cell kept verbatim.
type RawRow struct {
	Index int       `json:"index"` // 1-based row within its layer version
	Cells []RawCell `json:"cells"`
}

// ParsedDoc is the parse result attached to a raw document.
type ParsedDoc struct {
	DocID       string       `json:"docId"`
	Layer       Layer        `json:"layer"`
	Rows        []RawRow     `json:"rows"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Value is a domain quantity. Every derived value carries a unit or an
// explicit coordinate basis; Unknown values keep their reason.
type Value struct {
	V       float64 `json:"v"`
	Unit    string  `json:"unit,omitempty"`
	Basis   string  `json:"basis,omitempty"`
	Unknown bool    `json:"unknown,omitempty"`
	Reason  string  `json:"reason,omitempty"`
	StableID string `json:"stableId"` // identity stable across export/delete/reimport
}

// Point is one TG/T sampling point in the normalized derived layer.
type Point struct {
	T      float64 `json:"t"`     // furnace time, s
	M      Value   `json:"m"`     // mass (mg); Unknown when source value unconvertible
	Temp   Value   `json:"temp"`  // temperature (degC)
	RawRow int     `json:"rawRow"`
}

// Segment is a furnace program ramp/hold segment.
type Segment struct {
	FromT  float64 `json:"fromT"`
	ToT    float64 `json:"toT"`
	FromTm float64 `json:"fromTemp"`
	ToTm   float64 `json:"toTemp"`
	Kind   string  `json:"kind"` // "ramp" | "hold"
}

// ValveEvent is a commanded gas valve switch, in furnace time coordinates.
type ValveEvent struct {
	T          float64 `json:"t"`
	FromGas    string  `json:"fromGas"`
	ToGas      string  `json:"toGas"`
	RawRow     int     `json:"rawRow"`
}

// GasInterval is an effective atmosphere interval at the sample: commanded
// valve time shifted by the atmosphere delay.
type GasInterval struct {
	FromT      float64 `json:"fromT"` // effective time at sample, s
	ToT        float64 `json:"toT"`
	Gas        string  `json:"gas"`
	CommandedT float64 `json:"commandedT"` // valve switch time
	DelayS     float64 `json:"delayS"`
}

// Parameters are versioned analysis settings. All physical fields carry units.
type Parameters struct {
	AtmosDelayS         float64 `json:"atmosDelayS"`
	TempLagS            float64 `json:"tempLagS"`
	BlankScale          float64 `json:"blankScale"`
	DriftUgpMin         float64 `json:"driftUgpMin"` // ug/min correction slope
	DriftInterceptUg    float64 `json:"driftInterceptUg"`
	DTGWindowS          float64 `json:"dtgWindowS"`
	PeakPromPctPerMin   float64 `json:"peakPromPctPerMin"`
	BoundaryTolS        float64 `json:"boundaryTolS"`
	GapThresholdS       float64 `json:"gapThresholdS"`
	NonMonoTolMg        float64 `json:"nonMonoTolMg"`
	OverlapHalfWindowS  float64 `json:"overlapHalfWindowS"`
}

// DefaultParameters returns the seeded parameter set.
func DefaultParameters() Parameters {
	return Parameters{
		AtmosDelayS:        0,
		TempLagS:           0,
		BlankScale:         1.0,
		DriftUgpMin:        0,
		DriftInterceptUg:   0,
		DTGWindowS:         30,
		PeakPromPctPerMin:  0.05,
		BoundaryTolS:       20,
		GapThresholdS:      90,
		NonMonoTolMg:       0.02,
		OverlapHalfWindowS: 25,
	}
}
