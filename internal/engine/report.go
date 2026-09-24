package engine

import "time"

// Status values shared by stages, candidates and residues.
const (
	StatusOK       = "ok"
	StatusUnknown  = "unknown"
	StatusRejected = "rejected"
)

// Report is the full derived analysis for one effective parameter set.
type Report struct {
	ProjectID string    `json:"project_id"`
	Algo      string    `json:"algo"`
	ParamFP   string    `json:"param_fp"`
	Sources   []string  `json:"sources"`
	At        time.Time `json:"at"`
	// effective input identities used directly
	InitialMassSrc string `json:"initial_mass_src,omitempty"`
	SampleSrc      string `json:"sample_src,omitempty"`
	BlankSrc       string `json:"blank_src,omitempty"`
	DriftSrc       string `json:"drift_src,omitempty"`
	ProgramSrc     string `json:"program_src,omitempty"`
	GasSrc         string `json:"gas_src,omitempty"`

	InitialMassMg     *float64 `json:"initial_mass_mg,omitempty"`
	InitialMassStatus string   `json:"initial_mass_status"`

	Timeline []TimelinePoint  `json:"timeline"`
	Gaps     []GapSegment     `json:"gaps"`
	Switches []GasSwitch      `json:"switches"`
	Program  []ProgStep       `json:"program"`
	Stages   []Stage          `json:"stages"`
	Rejected []StageCandidate `json:"rejected"`
	Residues []ResidueResult  `json:"residues"`
	// diagnostics emitted by the derived layer
	Notes []ReportNote `json:"notes"`
}

type ReportNote struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// TimelinePoint is the synchronized, corrected sample row. Fields are nil
// across gaps or when the raw quantity could not be normalized.
type TimelinePoint struct {
	UID         string   `json:"uid"`
	Index       int      `json:"index"`
	SegIndex    int      `json:"seg_index"`
	T           float64  `json:"t"` // seconds
	FurnaceTC   *float64 `json:"furnace_t_c,omitempty"`
	SampleTC    *float64 `json:"sample_t_c,omitempty"`
	RawMg       *float64 `json:"raw_mg,omitempty"`
	BlankMg     *float64 `json:"blank_mg,omitempty"`
	DriftMg     *float64 `json:"drift_mg,omitempty"`
	CorrectedMg *float64 `json:"corrected_mg,omitempty"`
	Pct         *float64 `json:"pct,omitempty"` // % of initial mass
	DTGPctMin   *float64 `json:"dtg_pct_min,omitempty"`
	NonMono     bool     `json:"non_mono,omitempty"`
	RawTText    string   `json:"raw_t_text,omitempty"`
	RawMText    string   `json:"raw_m_text,omitempty"`
}

// GapSegment is a sampling break; derived series never bridges it.
type GapSegment struct {
	UID       string  `json:"uid"`
	Index     int     `json:"index"`
	AfterIdx  int     `json:"after_idx"`
	BeforeIdx int     `json:"before_idx"`
	StartT    float64 `json:"start_t"`
	EndT      float64 `json:"end_t"`
	DurationS float64 `json:"duration_s"`
}

type GasSwitch struct {
	UID     string  `json:"uid"`
	Index   int     `json:"index"`
	ValveTS float64 `json:"valve_t_s"`
	EffTS   float64 `json:"eff_t_s"`
	From    string  `json:"from"`
	To      string  `json:"to"`
	DelayS  float64 `json:"delay_s"`
	RawText string  `json:"raw_text,omitempty"`
}

type ProgStep struct {
	UID        string  `json:"uid"`
	Index      int     `json:"index"`
	Kind       string  `json:"kind"`
	StartTS    float64 `json:"start_t_s"`
	EndTS      float64 `json:"end_t_s"`
	StartTempC float64 `json:"start_temp_c"`
	EndTempC   float64 `json:"end_temp_c"`
	RateCMin   float64 `json:"rate_c_min"`
	Gas        string  `json:"gas,omitempty"`
}

type Ref struct {
	ID    string `json:"id"`
	Layer string `json:"layer"` // raw | derived | decision
	Label string `json:"label"`
}

// ChainStep is one expandable line in a residue calculation chain.
type ChainStep struct {
	UID   string      `json:"uid"`
	Op    string      `json:"op"`
	Text  string      `json:"text"`
	Value *StepVal    `json:"value,omitempty"`
	Refs  []Ref       `json:"refs,omitempty"`
	Subs  []ChainStep `json:"subs,omitempty"`
}

type StepVal struct {
	Number float64 `json:"number"`
	Unit   string  `json:"unit"`
	Status string  `json:"status,omitempty"`
	Raw    string  `json:"raw,omitempty"`
}

// StageCandidate is one legal interpretation of a mass-loss window.
type StageCandidate struct {
	UID         string   `json:"uid"`
	DerID       string   `json:"der_id"`
	Label       string   `json:"label"`
	StartIdx    int      `json:"start_idx"`
	EndIdx      int      `json:"end_idx"`
	StartTS     float64  `json:"start_t_s"`
	EndTS       float64  `json:"end_t_s"`
	PeakIdx     int      `json:"peak_idx,omitempty"`
	PeakTS      float64  `json:"peak_t_s,omitempty"`
	StartTC     *float64 `json:"start_temp_c,omitempty"`
	EndTC       *float64 `json:"end_temp_c,omitempty"`
	PeakTC      *float64 `json:"peak_temp_c,omitempty"`
	MassLossPct *float64 `json:"mass_loss_pct,omitempty"`
	PeakDTG     *float64 `json:"peak_dtg_pct_min,omitempty"`
	CrossesGap  bool     `json:"crosses_gap,omitempty"`
	Noisy       bool     `json:"noisy,omitempty"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason,omitempty"`
	GasWindow   string   `json:"gas_window,omitempty"`
}

// Stage groups overlapping candidates that cannot be uniquely resolved.
type Stage struct {
	UID        string           `json:"uid"`
	DerID      string           `json:"der_id"`
	Label      string           `json:"label"`
	Candidates []StageCandidate `json:"candidates"`
	// Primary is the preferred interpretation (first candidate), kept even
	// when alternatives are retained.
	Primary StageCandidate `json:"primary"`
	Chain   []ChainStep    `json:"chain"`
	Sources []Ref          `json:"sources"`
}

type ResidueResult struct {
	UID     string      `json:"uid"`
	DerID   string      `json:"der_id"`
	Kind    string      `json:"kind"` // final | stage_end
	Label   string      `json:"label"`
	AtIdx   int         `json:"at_idx"`
	AtTS    float64     `json:"at_t_s"`
	AtTC    *float64    `json:"at_temp_c,omitempty"`
	Percent *float64    `json:"percent,omitempty"`
	MassMg  *float64    `json:"mass_mg,omitempty"`
	Status  string      `json:"status"`
	Reason  string      `json:"reason,omitempty"`
	Chain   []ChainStep `json:"chain"`
	Sources []Ref       `json:"sources"`
}
