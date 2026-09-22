package domain

// Unit is a physical unit carried by every domain quantity.
type Unit string

const (
	UnitMg       Unit = "mg"
	UnitC        Unit = "degC"
	UnitS        Unit = "s"
	UnitMin      Unit = "min"
	UnitMgPerMin Unit = "mg/min"
	UnitMgPerS   Unit = "mg/s"
	UnitPct      Unit = "%"
	UnitGas      Unit = "gas"
)

// Q is a quantity with value, unit and coordinate basis. Raw preserves the
// original representation at import; unparseable values keep Raw with Bad=true.
type Q struct {
	Value float64 `json:"value"`
	Unit  Unit    `json:"unit"`
	Raw   string  `json:"raw,omitempty"`
	Bad   bool    `json:"bad,omitempty"`
	Diag  string  `json:"diag,omitempty"`
	Basis string  `json:"basis,omitempty"` // coordinate basis, e.g. "furnace_time"
}

// RawPoint is one sample: balance mass, temperature and furnace time.
type RawPoint struct {
	ID      string `json:"id"`
	BatchID string `json:"batch_id"`
	RowNo   int    `json:"row_no"`
	TimeS   Q      `json:"t"`
	TempC   Q      `json:"temp"`
	MassMg  Q      `json:"mass"`
}

// DriftRow is a balance baseline-drift correction (linear tare model).
type DriftRow struct {
	ID           string `json:"id"`
	BatchID      string `json:"batch_id"`
	StartS       Q      `json:"start"`
	RateMgPerMin Q      `json:"rate"`
	Active       bool   `json:"active"`
	Supersedes   string `json:"supersedes,omitempty"`
}

// BlankPoint is one blank/baseline curve point (empty-pan run).
type BlankPoint struct {
	ID      string `json:"id"`
	BatchID string `json:"batch_id"`
	TimeS   Q      `json:"t"`
	MassMg  Q      `json:"mass"`
}

// ProgramStep is one furnace-program segment.
type ProgramStep struct {
	ID          string `json:"id"`
	BatchID     string `json:"batch_id"`
	StartS      Q      `json:"start"`
	EndS        Q      `json:"end"`
	TargetC     Q      `json:"target"`
	RampCperMin Q      `json:"ramp"`
}

// GasEvent is one gas-valve switch on the instrument timeline.
type GasEvent struct {
	ID       string `json:"id"`
	BatchID  string `json:"batch_id"`
	ValveAtS Q      `json:"valve_at"`
	FromGas  string `json:"from_gas"`
	ToGas    string `json:"to_gas"`
}

// WeightRow is a sample initial mass statement; revisions supersede earlier.
type WeightRow struct {
	ID         string `json:"id"`
	BatchID    string `json:"batch_id"`
	Mg         Q      `json:"mg"`
	Active     bool   `json:"active"`
	Supersedes string `json:"supersedes,omitempty"`
}

// Provenance links a derived value to its direct sources.
type Provenance struct {
	AlgorithmVersion  string   `json:"algorithm_version"`
	ParamsFingerprint string   `json:"params_fingerprint"`
	DirectSources     []Source `json:"direct_sources"`
	NoteCodes         []string `json:"note_codes,omitempty"`
}

// Source is one direct input reference for a derived value.
type Source struct {
	Kind   string `json:"kind"` // "event" | "row" | "derived" | "decision"
	ID     string `json:"id"`
	Layer  string `json:"layer,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Rejection records why a candidate was rejected (first-class data).
type Rejection struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	AtSeq   int64  `json:"at_seq,omitempty"`
}

// Status marks derived validity in the current materialized view.
type Status string

const (
	StatusCurrent Status = "current"
	StatusStale   Status = "stale" // dependency changed; still viewable, never current
	StatusUnknown Status = "unknown"
)

// CorrPoint is a corrected-sample-layer point.
type CorrPoint struct {
	TimeS       float64 `json:"t"`
	TempC       float64 `json:"temp"`
	RawMassMg   float64 `json:"raw_mass"`
	DriftMg     float64 `json:"drift_mg"`
	BlankMg     float64 `json:"blank_mg"`
	CorrectedMg float64 `json:"corrected_mg"`
	Flag        string  `json:"flag,omitempty"` // "noise" | "gap_endpoint" ...
}

// DTGPoint is a derivative point; gap-spanning intervals are emitted as nulls.
type DTGPoint struct {
	TimeS    float64 `json:"t"`
	DMgPerS  float64 `json:"dmgs"`
	Spanning bool    `json:"spanning_gap"` // if true, value not used for peaks
}

// Peak is a DTG extremum candidate.
type Peak struct {
	ID            string     `json:"id"`
	TimeS         float64    `json:"t"`
	ReactionTempC float64    `json:"reaction_temp"`
	DMgPerS       float64    `json:"dmgs"`
	Prominence    float64    `json:"prominence"`
	Status        Status     `json:"status"`
	Rejected      *Rejection `json:"rejected,omitempty"`
	Prov          Provenance `json:"prov"`
}

// Gap is a sampling gap; derived layers never interpolate across it.
type Gap struct {
	ID     string     `json:"id"`
	StartS float64    `json:"start"`
	EndS   float64    `json:"end"`
	Status Status     `json:"status"`
	Prov   Provenance `json:"prov"`
}

// AtmosphereInterval is one effective atmosphere window at the sample.
type AtmosphereInterval struct {
	ID         string     `json:"id"`
	StartS     float64    `json:"start"` // valve time + atmosphere delay
	EndS       float64    `json:"end"`
	Gas        string     `json:"gas"`
	ValveEvent string     `json:"valve_event"`
	Status     Status     `json:"status"`
	Prov       Provenance `json:"prov"`
}

// StageCandidate is a decomposition stage. Stages may overlap; loss percents
// are relative to the active initial mass and are not forced to sum to 100%.
type StageCandidate struct {
	ID        string     `json:"id"`
	StartS    float64    `json:"start"`
	EndS      float64    `json:"end"`
	MassStart float64    `json:"mass_start_mg"`
	MassEnd   float64    `json:"mass_end_mg"`
	LossMg    float64    `json:"loss_mg"`
	LossPct   Q          `json:"loss_pct"` // unknown if no active initial mass
	MeanTempC float64    `json:"mean_temp"`
	Gas       string     `json:"gas"`
	PeakID    string     `json:"peak_id,omitempty"`
	Origin    string     `json:"origin"` // "peak" | "gas" | "manual"
	Adjusted  bool       `json:"adjusted"`
	Status    Status     `json:"status"`
	Verdict   string     `json:"verdict,omitempty"` // accepted | rejected
	Rejected  *Rejection `json:"rejected,omitempty"`
	Ambiguous []string   `json:"ambiguous_gas,omitempty"`
	Prov      Provenance `json:"prov"`
}

// ChainNode is one step in an expandable residue calculation chain.
type ChainNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "weight" | "corrected_point" | "blank" ...
	Label    string `json:"label"`
	Value    Q      `json:"value"`
	SourceID string `json:"source_id"`
	Formula  string `json:"formula,omitempty"`
}

// Residue is a residual-rate statement with full calculation chain.
type Residue struct {
	ID        string      `json:"id"`
	AtS       float64     `json:"at_s"`
	MassMg    Q           `json:"mass_mg"`
	InitialMg Q           `json:"initial_mg"`
	Pct       Q           `json:"pct"`
	Status    Status      `json:"status"`
	Verdict   string      `json:"verdict,omitempty"`
	Chain     []ChainNode `json:"chain"`
	Prov      Provenance  `json:"prov"`
}

// DerivedView is the full materialized result set at one event sequence.
type DerivedView struct {
	ProjectID          string               `json:"project_id"`
	AtSeq              int64                `json:"at_seq"`
	Status             Status               `json:"status"` // current view status
	Corr               []CorrPoint          `json:"corrected"`
	DTG                []DTGPoint           `json:"dtg"`
	Peaks              []Peak               `json:"peaks"`
	Gaps               []Gap                `json:"gaps"`
	Atmosphere         []AtmosphereInterval `json:"atmosphere"`
	Stages             []StageCandidate     `json:"stages"`
	Residues           []Residue            `json:"residues"`
	StaleStages        []StageCandidate     `json:"stale_stages,omitempty"`
	StaleResidues      []Residue            `json:"stale_residues,omitempty"`
	StalePeaks         []Peak               `json:"stale_peaks,omitempty"`
	Diagnostics        []string             `json:"diagnostics"`
	InitialWeightKnown bool                 `json:"initial_weight_known"`
	Params             Params               `json:"params"`
	Prov               Provenance           `json:"prov"`
}

// Conflict is an unresolved or recorded clash between human decisions.
type Conflict struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // "stage_overlap" | "residue"
	TargetIDs   []string `json:"target_ids"`
	Open        bool     `json:"open"`
	OpenedSeq   int64    `json:"opened_seq"`
	ChosenID    string   `json:"chosen_id,omitempty"`
	ResolvedSeq int64    `json:"resolved_seq,omitempty"`
}
