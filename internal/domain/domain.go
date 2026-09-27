// Package domain defines the immutable core types of the TGA evidence chain.
//
// Every measured quantity keeps its original textual representation and unit.
// Normalisation to SI/coordinate bases happens only in the derived layer;
// values that cannot be converted stay in place and carry a diagnostic.
package domain

// Layers that an import payload may contain.
const (
	LayerSamples    = "samples"    // raw sampled points: time, temperature, mass
	LayerDrift      = "drift"      // balance drift correction model
	LayerFurnace    = "furnace"    // furnace temperature program
	LayerGas        = "gas"        // gas valve events
	LayerBlank      = "blank"      // blank (empty pan) curve
	LayerInitialMass = "initmass"  // stated sample initial mass
)

// AllLayers returns the six strata stored separately.
func AllLayers() []string {
	return []string{
		LayerSamples, LayerDrift, LayerFurnace,
		LayerGas, LayerBlank, LayerInitialMass,
	}
}

// Quantity is a value as imported. Raw is the original textual representation;
// Unit is the declared unit; Basis is the coordinate/reference basis (e.g. "min",
// "K", "mg(balance:B1)"). Canonical is filled only when the derived layer can
// normalise it.
type Quantity struct {
	Raw        string  `json:"raw"`
	Unit       string  `json:"unit"`
	Basis      string  `json:"basis,omitempty"`
	Canonical  float64 `json:"canonical,omitempty"`
	Normalised bool    `json:"normalised,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// SamplePoint is one raw acquisition point.
type SamplePoint struct {
	Index       int      `json:"index"`
	Time        Quantity `json:"time"`
	Temperature Quantity `json:"temperature"`
	Mass        Quantity `json:"mass"`
}

// DriftModel is the balance baseline-drift correction statement.
type DriftModel struct {
	Kind         string   `json:"kind"` // "linear" | "points" | "none"
	SlopePerMin  Quantity `json:"slope_per_min,omitempty"`
	Intercept    Quantity `json:"intercept,omitempty"`
	AnchorTime   Quantity `json:"anchor_time,omitempty"`
	Points       []struct {
		Time  Quantity `json:"time"`
		Mass  Quantity `json:"mass"`
	} `json:"points,omitempty"`
}

// FurnaceSegment is one segment of the furnace program.
type FurnaceSegment struct {
	Index     int      `json:"index"`
	Kind      string   `json:"kind"` // "isothermal" | "ramp"
	StartTime Quantity `json:"start_time"`
	EndTime   Quantity `json:"end_time"`
	T0        Quantity `json:"t0"`
	T1        Quantity `json:"t1"`
}

// GasEvent is one gas valve switch record.
type GasEvent struct {
	Index  int      `json:"index"`
	Time   Quantity `json:"time"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Flow   Quantity `json:"flow,omitempty"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	Index       int      `json:"index"`
	Time        Quantity `json:"time"`
	Temperature Quantity `json:"temperature"`
	Mass        Quantity `json:"mass"`
}

// InitialMassStatement is the stated starting mass of the sample.
type InitialMassStatement struct {
	Mass Quantity `json:"mass"`
	Note string   `json:"note,omitempty"`
}
