package domain

import "encoding/json"

// Layer kinds are the six stratified raw input layers. Their order defines
// the dependency scope of local source revisions.
type LayerKind string

const (
	LayerSample   LayerKind = "sample"   // 原始采样点 mass/temperature vs furnace time
	LayerDrift    LayerKind = "drift"    // 天平漂移校正模型
	LayerFurnace  LayerKind = "furnace"  // 炉程序
	LayerGas      LayerKind = "gas"      // 气体阀事件
	LayerBlank    LayerKind = "blank"    // 空白曲线
	LayerInitial  LayerKind = "initial"  // 样品初重
)

var AllLayers = []LayerKind{LayerSample, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitial}

// Node kinds are the derived layers.
type NodeKind string

const (
	NodeTG        NodeKind = "tg"        // 漂移校正后的 TG/DTG 曲线
	NodeStages    NodeKind = "stages"    // 阶段候选批次
	NodeResidual  NodeKind = "residual"  // 最终残留率计算链
)

// Quantity carries every domain value together with its unit or coordinate basis.
// Raw keeps the original textual representation from import; Normalized is only
// present in the derived layer. Values that cannot be converted stay at their
// original position with a diagnostic instead of being dropped.
type Quantity struct {
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Raw        string  `json:"raw,omitempty"`
	Basis      string  `json:"basis,omitempty"`
	Converted  bool    `json:"converted,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// AtTime tags a value with the furnace-time coordinate basis (s, furnace clock).
type AtTime struct {
	Quantity
	T     float64 `json:"t"`
	TUnit string  `json:"t_unit"`
}

// SamplePoint is one raw sampled row: furnace time, balance mass, furnace temperature.
type SamplePoint struct {
	Idx   int      `json:"idx"`
	T     float64  `json:"t"`      // s, furnace clock
	Mass  Quantity `json:"mass"`   // mg (or raw if unconvertible)
	Temp  Quantity `json:"temp"`   // degC / K / degF (or raw)
	Note  string   `json:"note,omitempty"`
}

type FurnaceSeg struct {
	Idx    int      `json:"idx"`
	T0     float64  `json:"t0"`
	T1     float64  `json:"t1"`
	Target Quantity `json:"target"` // 目标温度
	Rate   Quantity `json:"rate"`   // K/min
	Mode   string   `json:"mode"`   // ramp / hold
}

type GasEvent struct {
	Idx      int      `json:"idx"`
	T        float64  `json:"t"`     // 阀切换时间 s
	Gas      string   `json:"gas"`   // N2 / Air / ...
	Action   string   `json:"action"` // on/off/switch
	Flow     Quantity `json:"flow"`  // mL/min
	Note     string   `json:"note,omitempty"`
}

type InitialMass struct {
	Mass     Quantity `json:"mass"`
	AtT      float64  `json:"at_t"` // 初重锚点的炉时间
	TUnit    string   `json:"t_unit"`
	Note     string   `json:"note,omitempty"`
}

// DriftModel is the balance baseline drift correction as imported:
// mass_corrected = mass - (b0 + b1 * t_min). A blank curve may additionally
// be referenced for pointwise subtraction.
type DriftModel struct {
	B0          Quantity `json:"b0"`
	B1          Quantity `json:"b1"` // mg/min
	UseBlank    bool     `json:"use_blank"`
	BlankSpline bool     `json:"blank_spline"`
	Note        string   `json:"note,omitempty"`
}

// Diagnostic records why a value/position could not be processed.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Layer   string `json:"layer,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// LayerVersion is an immutable committed version of one raw input layer.
// Revisions are expressed as a new version carrying ParentSeq; the original
// input can never be overwritten.
type LayerVersion struct {
	Project   string          `json:"project"`
	Kind      LayerKind       `json:"kind"`
	Seq       int64           `json:"seq"`
	ParentSeq int64           `json:"parent_seq,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	ImportID  string          `json:"import_id,omitempty"`
	RawText   json.RawMessage `json:"raw_text,omitempty"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`

	Points   []SamplePoint `json:"points,omitempty"`
	Furnace  []FurnaceSeg  `json:"furnace,omitempty"`
	Gas      []GasEvent    `json:"gas,omitempty"`
	Initial  *InitialMass  `json:"initial,omitempty"`
	Drift    *DriftModel   `json:"drift,omitempty"`
	Blank    []SamplePoint `json:"blank,omitempty"`
}

func (lv LayerVersion) ID() string { return lv.Project + "/L/" + string(lv.Kind) + "#v" + itoa(lv.Seq) }

// SourceRef points at exactly one direct source (a raw layer version or a derived node).
type SourceRef struct {
	Kind   string `json:"kind"`             // layer | node | event
	ID     string `json:"id"`
	Seq    int64  `json:"seq,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Params is the revision-controlled parameter set used by the decomposition algorithm.
type Params struct {
	Version             int     `json:"version"`
	AtmosDelayS         float64 `json:"atmos_delay_s"`          // 气氛延迟: 阀->样品反应
	TempLagS            float64 `json:"temp_lag_s"`             // 温度滞后
	Prominence          float64 `json:"prominence_mg_per_min"`  // DTG 峰显著性阈值
	MinPeakSepS         float64 `json:"min_peak_sep_s"`         // 重叠峰合并窗
	GapThresholdS       float64 `json:"gap_threshold_s"`        // 超过即采样缺口
	NonMonotonicMg      float64 `json:"nonmonotonic_mg"`        // 非单调噪声判定幅度
	OverlapGrouping     string  `json:"overlap_grouping"`       // a | b | unresolved
	MaxOverlapMsPerMin  float64 `json:"max_overlap_ms_per_min"` // 重叠判定: 谷深小于峰高的比例阈值
}

func DefaultParams() Params {
	return Params{
		Version: 1, AtmosDelayS: 45, TempLagS: 30,
		Prominence: 0.02, MinPeakSepS: 120, GapThresholdS: 60,
		NonMonotonicMg: 0.005, OverlapGrouping: "unresolved", MaxOverlapMsPerMin: 0.5,
	}
}

// DerivedPoint is one plotted TG/DTG point with stable identity.
type DerivedPoint struct {
	PID    string   `json:"pid"`
	T      float64  `json:"t"`
	Mass   float64  `json:"mass_corr_mg"`
	TGPct  *float64 `json:"tg_pct,omitempty"`
	DTG    *float64 `json:"dtg_mg_per_min,omitempty"`
	GapAfter bool   `json:"gap_after,omitempty"`
	NonMonotonic bool `json:"nonmonotonic,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Stage is one decomposition stage candidate; stages may overlap and mass
// losses are never forced to sum to 100%.
type Stage struct {
	CID         string   `json:"cid"`
	Index       int      `json:"index"`
	TStart      float64  `json:"t_start_s"`
	TEnd        float64  `json:"t_end_s"`
	PeakT       float64  `json:"peak_t_s"`
	PeakValveT  *float64 `json:"peak_valve_t_s,omitempty"` // 反推阀切换时间(!=反应时间)
	PeakTempC   *float64 `json:"peak_temp_c,omitempty"`
	Atmos       string   `json:"atmos_at_reaction,omitempty"`
	MassStartMg *float64 `json:"mass_start_mg,omitempty"`
	MassEndMg   *float64 `json:"mass_end_mg,omitempty"`
	LossMg      *float64 `json:"loss_mg,omitempty"`
	LossPct     *float64 `json:"loss_pct,omitempty"`
	OverlapsWith []int   `json:"overlaps_with,omitempty"`
	Unknown     bool     `json:"unknown,omitempty"`
	Note        string   `json:"note,omitempty"`
}

type RejectedPeak struct {
	T      float64 `json:"t"`
	Reason string  `json:"reason"`
}

// CandidateBatch is a derived node holding all legal stage decompositions;
// when overlap cannot be uniquely resolved all legal groupings are retained.
type CandidateBatch struct {
	Groupings     [][]Stage      `json:"groupings"`
	RejectedPeaks []RejectedPeak `json:"rejected_peaks,omitempty"`
	GapRanges     [][2]float64   `json:"gap_ranges,omitempty"`
	Note          string         `json:"note,omitempty"`
}

// ResidualChain lets every residual rate expand to its full calculation chain.
type ResidualChain struct {
	FinalT        float64          `json:"final_t_s"`
	FinalMassMg   *float64         `json:"final_mass_mg,omitempty"`
	InitialMg     *float64         `json:"initial_mg,omitempty"`
	ResidualPct   *float64         `json:"residual_pct,omitempty"`
	Unknown       bool             `json:"unknown,omitempty"`
	Diagnostic    string           `json:"diagnostic,omitempty"`
	Steps         []ChainStep      `json:"steps"`
}

type ChainStep struct {
	Label  string  `json:"label"`
	Detail string  `json:"detail"`
	Value  string  `json:"value,omitempty"`
	Sources []SourceRef `json:"sources,omitempty"`
}

// DerivedNode is one versioned derived result with algorithm/parameter/source provenance.
type DerivedNode struct {
	Project     string          `json:"project"`
	Kind        NodeKind        `json:"kind"`
	Seq         int64           `json:"seq"` // node lineage id = kind#seq
	Version     int64           `json:"version"`
	AlgoVersion string          `json:"algo_version"`
	ParamFingerprint string     `json:"param_fingerprint"`
	Params      Params          `json:"params"`
	Sources     []SourceRef     `json:"sources"`
	BornEvent   int64           `json:"born_event"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`

	Points   []DerivedPoint  `json:"points,omitempty"`
	Batch    *CandidateBatch `json:"batch,omitempty"`
	Residual *ResidualChain  `json:"residual,omitempty"`
}

func (n DerivedNode) ID() string { return n.Project + "/N/" + string(n.Kind) + "#n" + itoa(n.Seq) }
func (n DerivedNode) VersionID() string { return n.ID() + "/v" + itoa(n.Version) }

// ReviewState is the core state machine for one project's review session.
type ReviewState string

const (
	ReviewImported   ReviewState = "imported"
	ReviewCandidates ReviewState = "candidates"
	ReviewInConflict ReviewState = "in_conflict"
	ReviewFinalized  ReviewState = "finalized"
	ReviewReopened   ReviewState = "reopened"
)
