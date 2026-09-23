package domain

// Unit identifies the canonical physical unit used after normalization.
type Unit string

const (
	UnitMilligram Unit = "mg"
	UnitSecond    Unit = "s"
	UnitCelsius   Unit = "degC"
	UnitKelvin    Unit = "K"
	UnitPercent   Unit = "%"
	UnitPerMinute Unit = "K/min"
	UnitPerSecond Unit = "mg/s"
	UnitPerKelvin Unit = "mg/K"
	UnitNone      Unit = ""
)

// Q is a quantity carrying its value, canonical unit and the original textual
// representation exactly as imported. When the original value could not be
// converted/parsed, Valid is false and the text stays in place with diagnostics.
type Q struct {
	Value    float64 `json:"value"`
	Unit     Unit    `json:"unit"`
	Original string  `json:"original"`
	Valid    bool    `json:"valid"`
	Note     string  `json:"note,omitempty"`
}

// SamplePoint is one raw TGA acquisition point. The sample thermocouple
// reading is optional: when missing, TSampleC falls back to the furnace trace.
// TempRaw preserves the original token; non-convertible values never drop the
// row, they stay on the point with Valid=false plus a diagnostic.
type SamplePoint struct {
	T       float64 `json:"t_s"`
	MassRaw string  `json:"mass_raw"`
	Mass    Q       `json:"mass"`
	TempRaw string  `json:"temp_raw,omitempty"`
	Temp    Q       `json:"temp"`
	Row     int     `json:"row"`
	Diag    string  `json:"diag,omitempty"`
}

// FurnaceSegment is one ramp/hold program segment.
type FurnaceSegment struct {
	FromC      float64 `json:"from_c"`
	ToC        float64 `json:"to_c"`
	RateKmin   float64 `json:"rate_k_per_min"`
	DurationS  float64 `json:"duration_s"`
	Gas        string  `json:"gas,omitempty"`
	StartS     float64 `json:"start_s"`
	Kind       string  `json:"kind"` // ramp | hold
	Original   string  `json:"original,omitempty"`
	Diag       string  `json:"diag,omitempty"`
}

// ValveEvent is a gas valve switch at controller clock time t_s.
type ValveEvent struct {
	TS       float64 `json:"t_s"`
	Gas      string  `json:"gas"`
	Original string  `json:"original,omitempty"`
	Diag     string  `json:"diag,omitempty"`
}

// BlankPoint is one blank (empty pan) baseline curve point.
type BlankPoint struct {
	T       float64 `json:"t_s"`
	MassRaw string  `json:"mass_raw"`
	Mass    Q       `json:"mass"`
}

// DriftPoint models a linear balance baseline drift calibration
// mass_bias(t) = intercept_mg + slope_mg_per_s * t.
type DriftPoint struct {
	TS        float64 `json:"t_s"`
	MassRaw   string  `json:"mass_raw"`
	Mass      Q       `json:"mass"`
	Intercept float64 `json:"intercept_mg"`
	Slope     float64 `json:"slope_mg_per_s"`
	Version   int     `json:"version"`
	Original  string  `json:"original,omitempty"`
	Diag      string  `json:"diag,omitempty"`
}

// InitialMass is the stratified sample initial mass decision layer.
type InitialMass struct {
	MassRaw string `json:"mass_raw"`
	Mass    Q      `json:"mass"`
	Version int    `json:"version"`
	Note    string `json:"note,omitempty"`
}
