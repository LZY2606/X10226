package tga

// 派生结果（artifacts）：每个结果都带算法版本、参数指纹、直接来源。
// 候选/拒绝原因/误差/未知状态都是一等数据；缺口两侧不插值。

// ArtifactKind 派生结果种类。
const (
	ArtCorrected    = "corrected_series" // 漂移校正后的 TG 序列
	ArtSegments     = "segments"         // 数据缺口分段（不跨缺口插值）
	ArtDTG          = "dtg_series"       // DTG（仅在段内中心差分）
	ArtAtmosphere   = "atmosphere_windows" // 气氛时间线（含延迟/滞后）
	ArtPeaks        = "peaks"            // DTG 峰（含重叠峰组与候选）
	ArtStages       = "stage_decomposition" // 阶段分解候选（可重叠、不强制100%）
	ArtResidue      = "residue"          // 残留率候选（含计算链）
)

// Point 派生序列点（规范化单位）。Interpolated 永远不为 true：缺口不插值。
type Point struct {
	PointID    string  `json:"point_id"` // P#<layer>#<rowseq>，来自原始点身份
	T          float64 `json:"t_s"`
	TempC      float64 `json:"temp_c"`
	MassMg     float64 `json:"mass_mg"`
	DTGMgMin   *float64 `json:"dtg_mg_min,omitempty"`
	Segment    int     `json:"segment"`
}

// Gap 数据缺口：a→b 之间不插值。
type Gap struct {
	AfterPointID string  `json:"after_point_id"`
	BeforePointID string `json:"before_point_id"`
	FromS        float64 `json:"from_s"`
	ToS          float64 `json:"to_s"`
}

// Window 气氛/炉程序时间窗（反应侧 = 阀事件 + 气氛延迟，样品温度 = 炉温 + 温度滞后）。
type Window struct {
	ID            string  `json:"id"`
	Gas           string  `json:"gas"`
	ValveAtS      float64 `json:"valve_at_s"`      // 阀切换时间
	SampleFromS   float64 `json:"sample_from_s"`   // 样品侧实际生效时间
	SampleToS     float64 `json:"sample_to_s"`
	Note          string  `json:"note"`
}

// Peak DTG 峰。Ambiguous=true 表示与邻近峰构成重叠组，保留多个归属候选。
type Peak struct {
	ID          string   `json:"id"`
	AtS         float64  `json:"at_s"`
	TempC       float64  `json:"temp_c"`
	DTGMgMin    float64  `json:"dtg_mg_min"`
	Prominence  float64  `json:"prominence_mg_min"`
	GroupID     string   `json:"group_id,omitempty"`
	Ambiguous   bool     `json:"ambiguous"`
	GapBounded  bool     `json:"gap_bounded"` // 边界落在缺口上 → 未知
	WindowGas   string   `json:"window_gas"`  // 按样品侧气氛窗口归属
	CandidateOf string   `json:"candidate_of,omitempty"` // 所属阶段候选
	Sources     []string `json:"sources"`
}

// Stage 单个失重阶段（候选的一部分）。
type Stage struct {
	ID        string   `json:"id"`
	StartS    float64  `json:"start_s"`
	EndS      float64  `json:"end_s"`
	StartTemp float64  `json:"start_temp_c"`
	EndTemp   float64  `json:"end_temp_c"`
	StartUnknown bool  `json:"start_unknown"` // 起点跨缺口无法唯一决定
	EndUnknown   bool  `json:"end_unknown"`
	MassLossMg  float64 `json:"mass_loss_mg"`
	MassLossPct *float64 `json:"mass_loss_pct,omitempty"` // 相对初重；初重缺失时为 nil
	Gas        string   `json:"gas"`
	PeakIDs    []string `json:"peak_ids"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
}

// StageCandidate 一个阶段分解候选。
type StageCandidate struct {
	ID          string   `json:"id"`
	Stages      []Stage  `json:"stages"`
	SumPct      *float64 `json:"sum_pct,omitempty"` // 不强制 100%
	Note        string   `json:"note,omitempty"`
	RejectReason string  `json:"reject_reason,omitempty"` // 被拒绝的算法原因（若有）
	Preferred   bool     `json:"preferred"`
}

// ChainStep 残留率计算链中的一步。
type ChainStep struct {
	Step   int      `json:"step"`
	Label  string   `json:"label"`
	Detail string   `json:"detail"`
	Value  string   `json:"value,omitempty"` // 带单位
	Sources []SourceRef `json:"sources,omitempty"`
}

// ResidueCandidate 最终残留率候选（按初重版本/缺口情形可有多个）。
type ResidueCandidate struct {
	ID           string      `json:"id"`
	Basis        string      `json:"basis"` // 初重来源身份
	ValuePct     *float64    `json:"value_pct"`
	Unknown      bool        `json:"unknown"`
	UnknownReason string     `json:"unknown_reason,omitempty"`
	MassMg       *float64    `json:"mass_mg"`
	AtS          *float64    `json:"at_s"`
	RangePct     [2]*float64 `json:"range_pct,omitempty"` // 缺口导致的合法区间
	Chain        []ChainStep `json:"chain"`
}

// Artifact 任一派生结果的信封。
type Artifact struct {
	ID            string          `json:"id"` // 内容寻址
	ProjectID     string          `json:"project_id"`
	Kind          string          `json:"kind"`
	Algorithm     string          `json:"algorithm"`
	ParamFP       string          `json:"param_fp"`
	SourceSubSeqs []int           `json:"source_submission_seqs,omitempty"`
	Sources       []SourceRef     `json:"sources"` // 直接来源：层版本 / 其他派生 / 参数
	ProducedAtEvt int             `json:"produced_at_event_seq"`
	PointIDs      []string        `json:"point_ids,omitempty"`
	Points        []Point         `json:"points,omitempty"`
	Gaps          []Gap           `json:"gaps,omitempty"`
	Windows       []Window        `json:"windows,omitempty"`
	Peaks         []Peak          `json:"peaks,omitempty"`
	StageCands    []StageCandidate `json:"stage_candidates,omitempty"`
	Residues      []ResidueCandidate `json:"residues,omitempty"`
	Diags         []Diagnostic    `json:"diags,omitempty"`
}

// IsCurrent 判断该结果是否仍为当前结果：参数指纹相同，且每个直接来源层的提交
// 版本与当前有效版本一致。局部来源变化只会让引用该层的结果过期。
func (a *Artifact) IsCurrent(currentSubSeqByLayer map[string]int, fp string) bool {
	if a.ParamFP != fp {
		return false
	}
	for _, s := range a.Sources {
		if s.Kind != "cell" || s.Layer == "" {
			continue
		}
		cur, ok := currentSubSeqByLayer[s.Layer]
		if !ok || cur != s.SubmissionSeq {
			return false
		}
	}
	return true
}
