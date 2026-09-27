package domain

// Coordinate bases used by thermogravimetric quantities. Keeping these explicit
// is what lets us say "valve switch time != sample reaction time".
const (
	BasisFurnaceTimeS   = "furnace_elapsed_s"   // controller clock, seconds
	BasisReactionTimeS  = "sample_reaction_s"   // furnace clock shifted by gas delay
	BasisProgramTempC   = "furnace_program_C"   // programmed setpoint
	BasisMeasuredTempC  = "sample_measured_C"   // measured sample temperature
	BasisRawMassMg      = "balance_raw_mg"      // balance reading as imported
	BasisCorrectedMassMg = "balance_corrected_mg"
	BasisInitialMassMg  = "initial_mass_mg"
	BasisPercentInitial = "percent_of_initial_mass"
	BasisPctPerMin      = "percent_initial_per_min"
	BasisMgPerMin       = "mg_per_min"
	BasisCPerMin        = "C_per_min"
	BasisGasFlowMLMin   = "gas_flow_mL_per_min"
	BasisDimensionless  = "dimensionless"
)

// Quantity is a domain value that always carries its unit and coordinate
// basis. Raw preserves the original textual representation from import.
type Quantity struct {
	Value *float64 `json:"value"`
	Raw   string   `json:"raw"`
	Unit  string   `json:"unit"`
	Basis string   `json:"basis"`
	// Code carries a diagnostic code when Value cannot be derived from Raw.
	Code string `json:"code,omitempty"`
}

// Diag is a non-fatal or fatal diagnostic attached at import or derivation.
type Diag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Row      int    `json:"row,omitempty"`
	Column   string `json:"column,omitempty"`
	Message  string `json:"message"`
}
