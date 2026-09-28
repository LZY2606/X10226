package cases

type Pack struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Narrative    string        `json:"narrative"`
	Seed         int64         `json:"seed"`
	Baseline     PackInputs    `json:"baseline"`
	Variants     []Variant     `json:"variants"`
	Expectations []Expectation `json:"expectations"`
}

type PackInputs struct {
	SampleTSV  string `json:"sample_tsv"`
	BlankTSV   string `json:"blank_tsv"`
	InitialTSV string `json:"initial_tsv"`
	FurnaceTSV string `json:"furnace_tsv"`
	GasTSV     string `json:"gas_tsv"`
	BadTSV     string `json:"bad_tsv"`
}

type Variant struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	ChangedDirectSource string     `json:"changed_direct_source"`
	Inputs              PackInputs `json:"inputs"`
	ExpectedScope       []string   `json:"expected_affected_scope"`
	ProtectedScope      []string   `json:"protected_scope"`
	ExpectedFindings    []string   `json:"expected_findings"`
}

type Expectation struct {
	Code    string `json:"code"`
	Variant string `json:"variant"`
	Want    string `json:"want"`
}
