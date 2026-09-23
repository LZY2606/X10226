package tga

import "strings"

// CalcVersion identifies the calculation algorithm. Every derived value
// carries it.
const CalcVersion = "tga-calc-1"

// Unit / coordinate bases carried by every domain quantity.
const (
	BasisTimeS   = "s"
	BasisTempC   = "degC"
	BasisMassMg  = "mg"
	BasisPct     = "percent"
	BasisDtg     = "percent_per_min"
)

// Point is a normalized TGA sampling point. Raw representations are preserved.
type Point struct {
	ID        string  `json:"id"`
	Idx       int     `json:"idx"`
	TimeS     float64 `json:"time_s"`
	TempC     float64 `json:"temp_c"`
	MassMg    float64 `json:"mass_mg"`
	RawTime   string  `json:"raw_time"`
	RawTemp   string  `json:"raw_temp"`
	RawMass   string  `json:"raw_mass"`
	TimeUnit  string  `json:"time_unit"`
	TempUnit  string  `json:"temp_unit"`
	MassUnit  string  `json:"mass_unit"`
	SourceID  string  `json:"source_item_id"`
	Valid     bool    `json:"valid"`
	GapAfter  bool    `json:"gap_after"`
	NoiseFlag bool    `json:"noise_flag"`
	Diags     []Diagnostic `json:"diags,omitempty"`
}

// Gap is a sampling hiatus. Nothing interpolates across it.
type Gap struct {
	ID        string  `json:"id"`
	FromS     float64 `json:"from_s"`
	ToS       float64 `json:"to_s"`
	FromPtID  string  `json:"from_pt"`
	ToPtID    string  `json:"to_pt"`
	Reason    string  `json:"reason"`
}

// CorrectedPoint is a drift-corrected sample point.
type CorrectedPoint struct {
	ID        string  `json:"id"`
	PtID      string  `json:"pt_id"`
	TimeS     float64 `json:"time_s"`
	TempC     float64 `json:"temp_c"`
	CorrMg    float64 `json:"corrected_mg"`
	DriftMg   float64 `json:"drift_correction_mg"`
	Valid     bool    `json:"valid"`
	GapAfter  bool    `json:"gap_after"`
	NoiseFlag bool    `json:"noise_flag"`
}

// TgPoint is normalized mass fraction in percent.
type TgPoint struct {
	ID       string  `json:"id"`
	CPtID    string  `json:"cpt_id"`
	TimeS    float64 `json:"time_s"`
	TempC    float64 `json:"temp_c"`
	TgPct    float64 `json:"tg_pct"`
	GapAfter bool    `json:"gap_after"`
	NoiseFlag bool   `json:"noise_flag"`
}

// DtgPoint is the derivative -dm/dt expressed as %/min. It is undefined
// across gaps.
type DtgPoint struct {
	ID      string  `json:"id"`
	TgID    string  `json:"tg_id"`
	TimeS   float64 `json:"time_s"`
	Dtg     float64 `json:"dtg_pct_per_min"`
	NoiseFlag bool  `json:"noise_flag"`
}

// GasSwitch is a normalized valve event with its delayed reactor arrival.
type GasSwitch struct {
	ID        string  `json:"id"`
	SourceID  string  `json:"source_item_id"`
	ValveS    float64 `json:"valve_s"`
	ReactorS  float64 `json:"reactor_s"`
	DelayS    float64 `json:"delay_s"`
	FromGas   string  `json:"from_gas"`
	ToGas     string  `json:"to_gas"`
	RawTime   string  `json:"raw_time"`
}

// ProgramSeg is one furnace program segment.
type ProgramSeg struct {
	ID       string  `json:"id"`
	StartS   float64 `json:"start_s"`
	EndS     float64 `json:"end_s"`
	StartC   float64 `json:"start_c"`
	EndC     float64 `json:"end_c"`
	RateCPerMin float64 `json:"rate_c_per_min"`
	Kind     string  `json:"kind"` // hold | ramp
}

// Peak is a DTG minimum.
type Peak struct {
	ID        string  `json:"id"`
	TimeS     float64 `json:"time_s"`
	Dtg       float64 `json:"dtg"`
	PromPct   float64 `json:"prominence_pct"`
	GroupID   string  `json:"group_id,omitempty"`
}

// StageCandidate is a legal (possibly non-unique) decomposition stage.
type StageCandidate struct {
	ID          string   `json:"id"`
	GroupID     string   `json:"group_id,omitempty"`
	Alternative bool     `json:"alternative"`
	Label       string   `json:"label"`
	PeakID      string   `json:"peak_id"`
	StartS      float64 `json:"start_s"`
	EndS        float64 `json:"end_s"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	MassLossPct *float64 `json:"mass_loss_pct,omitempty"`
	Status      string   `json:"status"` // candidate | accepted | rejected | unknown
	Attribution Attribution `json:"attribution"`
	Reasons     []string `json:"reasons,omitempty"`
	Unknowns    []string `json:"unknowns,omitempty"`
	DecisionID  string   `json:"decision_id,omitempty"`
	Chain       []ChainStep `json:"chain"`
}

// Attribution links (or deliberately fails to link) a stage to a valve event.
type Attribution struct {
	SwitchID      string   `json:"switch_id,omitempty"`
	ValveS        *float64 `json:"valve_s,omitempty"`
	ReactorS      *float64 `json:"reactor_s,omitempty"`
	LagS          float64  `json:"lag_s"`
	Status        string   `json:"status"` // attributed | ambiguous | none
	Alternatives  []string `json:"alternatives,omitempty"`
	Note          string   `json:"note,omitempty"`
}

// Residue is one final-residue result with an expandable calculation chain.
type Residue struct {
	ID        string   `json:"id"`
	AtS       float64  `json:"at_s"`
	AtTempC   float64  `json:"at_temp_c"`
	Mg        *float64 `json:"mg,omitempty"`
	Pct       *float64 `json:"pct,omitempty"`
	InitialID string   `json:"initial_mass_id"`
	Status    string   `json:"status"` // ok | unknown | gap
	Reasons   []string `json:"reasons,omitempty"`
	DecisionID string  `json:"decision_id,omitempty"`
	Verdict   string   `json:"verdict,omitempty"`
	Chain     []ChainStep `json:"chain"`
}

// ChainStep is one readable calculation step with value references.
type ChainStep struct {
	Step string     `json:"step"`
	Text string     `json:"text"`
	Refs []ValueRef `json:"refs,omitempty"`
}

// ValueRef points at any displayed value by stable id.
type ValueRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// InitialMassChoice records which initial mass layer anchors percentages.
type InitialMassChoice struct {
	BatchID string  `json:"batch_id"`
	ItemID  string  `json:"item_id"`
	Mg      float64 `json:"mg"`
	Raw     string  `json:"raw"`
	Revisions []InitialMassRev `json:"revisions"`
}

// InitialMassRev is one historical initial-mass revision.
type InitialMassRev struct {
	BatchID string  `json:"batch_id"`
	Seq     int64   `json:"seq"`
	Mg      float64 `json:"mg"`
	Raw     string  `json:"raw"`
	Supersedes string `json:"supersedes,omitempty"`
}

// Report is the full derived analysis result for one parameter version.
type Report struct {
	ProjectID      string             `json:"project_id"`
	NodeID         string             `json:"node_id"`
	Current        bool               `json:"current"`
	ParamVersionID string             `json:"param_version_id"`
	ParamFingerprint string           `json:"param_fingerprint"`
	SourceBatches  []string           `json:"source_batches"`
	Initial        *InitialMassChoice `json:"initial,omitempty"`
	Points         []Point            `json:"points"`
	Gaps           []Gap              `json:"gaps"`
	Corrected      []CorrectedPoint   `json:"corrected"`
	Tg             []TgPoint          `json:"tg"`
	Dtg            []DtgPoint         `json:"dtg"`
	Switches       []GasSwitch        `json:"switches"`
	Program        []ProgramSeg       `json:"program"`
	Peaks          []Peak            `json:"peaks"`
	Candidates     []StageCandidate   `json:"candidates"`
	Residues       []Residue          `json:"residues"`
	DriftModel     string             `json:"drift_model"`
	SumConstrained bool               `json:"sum_constrained"`
	Warnings       []string           `json:"warnings"`
}

// hasCaseInsensitive helper for raw maps.
func rawGet(m map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	low := map[string]string{}
	for k, v := range m {
		low[strings.ToLower(strings.TrimSpace(k))] = v
	}
	for _, k := range keys {
		if v, ok := low[strings.ToLower(k)]; ok {
			return v, true
		}
	}
	return "", false
}
