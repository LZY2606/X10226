package domain

import "time"

// Algorithm versions are pinned constants. A change to any algorithm must bump
// the corresponding version so old derived results can never masquerade as
// results of the new code.
const (
	AlgoCorrect = "correct/v1"
	AlgoDTG     = "dtg/v1"
	AlgoStages  = "stages/v1"
	AlgoResidue = "residue/v1"
)

// Params is the reviewer-tunable parameter set. It is content-addressed:
// Fingerprint hashes the normalized struct, so identical revisions keep stable
// derived identities.
type Params struct {
	AtmDelayS       float64 `json:"atm_delay_s"`        // atmosphere transport delay
	TempLagS        float64 `json:"temp_lag_s"`         // furnace->sample temperature lag
	SmoothWindowPts int     `json:"smooth_window_pts"`  // DTG moving-average window
	PeakPromPct     float64 `json:"peak_prom_pct"`      // min DTG prominence, %/min
	PeakMinDistS    float64 `json:"peak_min_dist_s"`    // min peak separation, s
	OverlapSepPct   float64 `json:"overlap_sep_pct"`    // valley depth fraction for split
	GapThresholdS   float64 `json:"gap_threshold_s"`    // larger time spacing => gap
	NoiseSpikePct   float64 `json:"noise_spike_pct"`    // |local jump| / m0 threshold
	UnknownAreaPct  float64 `json:"unknown_area_pct"`   // area near gap/noise beyond this => unknown
	DriftCorrect    bool    `json:"drift_correct"`      // apply balance drift correction
	BlankCorrect    bool    `json:"blank_correct"`      // apply blank curve subtraction
}

// DefaultParams is the starting parameter set of every project.
func DefaultParams() Params {
	return Params{
		AtmDelayS:       30,
		TempLagS:        20,
		SmoothWindowPts: 3,
		PeakPromPct:     0.02,
		PeakMinDistS:    30,
		OverlapSepPct:   0.25,
		GapThresholdS:   90,
		NoiseSpikePct:   0.4,
		UnknownAreaPct:  0.5,
		DriftCorrect:    true,
		BlankCorrect:    true,
	}
}

// Ref points to one direct source of a derived object: another node, a raw
// layer/version, or a parameter set.
type Ref struct {
	Kind   string `json:"kind"` // raw | node | params
	ID     string `json:"id"`
	Ver    int    `json:"ver,omitempty"`
	Label  string `json:"label,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Provenance is the full provenance block every derived value carries.
type Provenance struct {
	Algo      string  `json:"algo"`
	ParamsID  string  `json:"params_id"`
	Sources   []Ref   `json:"sources"`
	InputFP   string  `json:"input_fp"`
	ComputedAt time.Time `json:"computed_at"`
}

// Diagnostic records an algorithmic warning/block, first-class so unknowns and
// rejections never silently disappear.
type Diagnostic struct {
	Code    string  `json:"code"`
	Msg     string  `json:"msg"`
	AtTimeS float64 `json:"at_time_s,omitempty"`
	Detail  any     `json:"detail,omitempty"`
}

// Segment is a contiguous run of valid samples. Gaps split segments and are
// never interpolated across.
type Segment struct {
	ID       string  `json:"id"`
	StartIdx int     `json:"start_idx"`
	EndIdx   int     `json:"end_idx"`
	T0       float64 `json:"t0_s"`
	T1       float64 `json:"t1_s"`
}

// Gap describes one missing span: indices around the hole and missing time span.
type Gap struct {
	BeforeIdx int     `json:"before_idx"`
	AfterIdx  int     `json:"after_idx"`
	TBefore   float64 `json:"t_before_s"`
	TAfter    float64 `json:"t_after_s"`
	SpanS     float64 `json:"span_s"`
	Missing   int     `json:"missing_rows"`
}

// NoiseFlag marks a non-monotonic sample excursion.
type NoiseFlag struct {
	Idx    int     `json:"idx"`
	TimeS  float64 `json:"time_s"`
	JumpMg float64 `json:"jump_mg"`
	PctM0  float64 `json:"pct_m0"`
}

// CorrectPoint is the corrected mass series plus all component traces used to
// reach it, so the calculation chain is readable.
type CorrectPoint struct {
	Idx       int      `json:"idx"`
	TimeS     float64  `json:"time_s"`
	FurnaceC  *float64 `json:"furnace_c,omitempty"`
	SampleC   *float64 `json:"sample_c,omitempty"`
	RawMassMg *float64 `json:"raw_mass_mg,omitempty"`
	BlankMg   *float64 `json:"blank_mg,omitempty"`
	DriftMg   *float64 `json:"drift_mg,omitempty"`
	CorrectedMg *float64 `json:"corrected_mg,omitempty"`
	Valid     bool     `json:"valid"`
	GapBefore bool     `json:"gap_before,omitempty"`
	Noisy     bool     `json:"noisy,omitempty"`
}

// CorrectNode is the corrected-series derived node.
type CorrectNode struct {
	Params Params         `json:"params"`
	Points []CorrectPoint `json:"points"`
	Segs   []Segment      `json:"segments"`
	Gaps   []Gap          `json:"gaps"`
	Noise  []NoiseFlag    `json:"noise"`
	Diags  []Diagnostic   `json:"diags"`
}

// DTGPoint is one derivative sample (computed within segment only).
type DTGPoint struct {
	Idx       int     `json:"idx"`
	TimeS     float64 `json:"time_s"`
	SampleC   float64 `json:"sample_c"`
	Dtg       float64 `json:"dtg"` // mg/min
	DtgPctMin float64 `json:"dtg_pct_min"`
	Smoothed  float64 `json:"smoothed"`
	SegID     string  `json:"seg_id"`
	GapAdj    bool    `json:"gap_adjacent,omitempty"`
	Noisy     bool    `json:"noisy,omitempty"`
}

// DTGNode is the derivative series per segment.
type DTGNode struct {
	Params Params     `json:"params"`
	M0Mg   *float64   `json:"m0_mg,omitempty"`
	Points []DTGPoint `json:"points"`
	Diags  []Diagnostic `json:"diags,omitempty"`
}

// GasWindow is the sample-experience window of a gas event after delay.
type GasWindow struct {
	EventIdx  int     `json:"event_idx"`
	ValveTimeS float64 `json:"valve_time_s"`
	ReachTimeS float64 `json:"reach_time_s"`
	Gas       string  `json:"gas"`
}

// StageCandidate is one legal decomposition candidate. Overlapping stages are
// allowed; ambiguous peaks yield multiple alternatives.
type StageCandidate struct {
	CID          string   `json:"cid"`
	PeakIdx      int      `json:"peak_idx"`
	PeakTimeS    float64  `json:"peak_time_s"`
	PeakTempC    float64  `json:"peak_temp_c"`
	DtgPeak      float64  `json:"dtg_peak"`
	StartS       float64  `json:"start_s"`
	EndS         float64  `json:"end_s"`
	StartC       float64  `json:"start_c"`
	EndC         float64  `json:"end_c"`
	StartIdx     int      `json:"start_idx"`
	EndIdx       int      `json:"end_idx"`
	MassLossMg   float64  `json:"mass_loss_mg"`
	MassLossPct  *float64 `json:"mass_loss_pct,omitempty"`
	AttributedGas string  `json:"attributed_gas,omitempty"`
	GasNote      string   `json:"gas_note,omitempty"`
	Status       string   `json:"status"` // proposed | accepted | rejected | unknown
	Alternative  int      `json:"alternative"` // 1-based decomposition alternative
	Group        string   `json:"group,omitempty"` // overlap group id
	RejectReason string   `json:"reject_reason,omitempty"`
	UnknownReason string  `json:"unknown_reason,omitempty"`
	Confidence   string   `json:"confidence"` // high | medium | low
	SegID        string   `json:"seg_id"`
	Evidence     []string `json:"evidence"`
}

// StagesNode holds all legal candidates and rejection reasons.
type StagesNode struct {
	Params     Params           `json:"params"`
	Windows    []GasWindow      `json:"gas_windows"`
	Candidates []StageCandidate `json:"candidates"`
	Rejections []StageCandidate `json:"rejections"`
	Diags      []Diagnostic     `json:"diags"`
}

// ChainStep is one line of a residue calculation chain.
type ChainStep struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	ValueS string  `json:"value"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Ref    Ref     `json:"ref"`
}

// Residue is one final-residue result (per tail segment).
type Residue struct {
	RID       string      `json:"rid"`
	TimeS     float64     `json:"time_s"`
	TempC     float64     `json:"temp_c"`
	FinalMg   float64     `json:"final_mg"`
	M0Mg      float64     `json:"m0_mg"`
	ResiduePct *float64   `json:"residue_pct,omitempty"`
	Status    string      `json:"status"` // ok | unknown
	UnknownReason string  `json:"unknown_reason,omitempty"`
	Chain     []ChainStep `json:"chain"`
	Diags     []Diagnostic `json:"diags,omitempty"`
}

// ResidueNode is the final residue derived node.
type ResidueNode struct {
	Params    Params      `json:"params"`
	Residues  []Residue   `json:"residues"`
	TotalLossPct *float64 `json:"total_loss_pct,omitempty"`
	Diags     []Diagnostic `json:"diags,omitempty"`
}

// Derived is a generic envelope for one persisted immutable derived node.
type Derived struct {
	ID         string    `json:"id"` // content-addressed
	Project    string    `json:"project"`
	Slot       string    `json:"slot"` // corrected | dtg | stages | residue
	Kind       string    `json:"kind"`
	ParamsID   string    `json:"params_id"`
	Prov       Provenance `json:"prov"`
	Correct    *CorrectNode `json:"correct,omitempty"`
	DTG        *DTGNode     `json:"dtg,omitempty"`
	Stages     *StagesNode  `json:"stages,omitempty"`
	Residue    *ResidueNode `json:"residue,omitempty"`
	Created    time.Time  `json:"created"`
}

// Node slots.
const (
	SlotCorrect = "corrected"
	SlotDTG     = "dtg"
	SlotStages  = "stages"
	SlotResidue = "residue"
)
