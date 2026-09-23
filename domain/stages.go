// Package domain: stages 实现 TGA 阶段分解。
//
// 领域规则:
//   - 重叠峰同时生成“合并单阶段”和“拆分多阶段”候选, 全部保留;
//     无法唯一决定时由人工裁定, 拒绝原因是一等数据。
//   - 阶段可重叠; 各段失重百分数不强制总和为 100%(重叠 + 基线漂移 +
//     气固反应延迟使质量账不能闭合)。
//   - 数据缺口不跨越: 边界/残留若落在缺口上, 状态 unknown, 不插值。
//   - 每个候选/残留都带稳定身份、算法版本、参数指纹与直接来源。
package domain

import (
	"math"
)

// StageCandidate 是一个阶段划分候选。
type StageCandidate struct {
	ID            string   `json:"id"`
	Stages        []Stage  `json:"stages"`
	Kind          string   `json:"kind"`   // merged / split / single
	Status        string   `json:"status"` // legal / rejected
	RejectReasons []string `json:"rejectReasons,omitempty"`
	Note          string   `json:"note,omitempty"`
	PeakGroup     int      `json:"peakGroup"`
	Support       []string `json:"support,omitempty"` // 直接来源 id
}

// Stage 是候选中的单个失重阶段。
type Stage struct {
	Index          int     `json:"index"`
	StartMin       float64 `json:"startMin"`
	EndMin         float64 `json:"endMin"`
	PeakTimeMin    float64 `json:"peakTimeMin"`
	PeakTempC      float64 `json:"peakTempC"`
	StartMassMg    float64 `json:"startMassMg"`
	EndMassMg      float64 `json:"endMassMg"`
	MassLossMg     float64 `json:"massLossMg"`
	GasDuring      string  `json:"gasDuring"`
	OverlapsWith   []int   `json:"overlapsWith,omitempty"`
	CrossesGap     bool    `json:"crossesGap"`
	Unknown        bool    `json:"unknown,omitempty"`
	BoundaryStatus string  `json:"boundaryStatus"` // exact / valley / gap_unknown
}

// StageSet 是一次算法运行产生的全部候选(采纳与否都保留)。
type StageSet struct {
	Candidates []StageCandidate `json:"candidates"`
	Windows    []ReactionWindow `json:"windows"`
}

// BuildStages 依据 DTG 峰组生成候选。
func BuildStages(cs CorrectedSeries, dtg DTGSeries, fc FurnaceCurve,
	gas []GasEvent, params Params, reactionTempC float64, deps DependencyRefs) StageSet {

	set := StageSet{Windows: ReactionWindows(gas, fc, params, reactionTempC)}
	seriesID := deps.CorrectedSeriesID
	peaks := dtg.Peaks
	if len(peaks) == 0 {
		return set
	}
	groups := map[int][]DTGPeak{}
	var groupOrder []int
	for _, pk := range peaks {
		if _, ok := groups[pk.MergeGroup]; !ok {
			groupOrder = append(groupOrder, pk.MergeGroup)
		}
		groups[pk.MergeGroup] = append(groups[pk.MergeGroup], pk)
	}
	gasAt := func(t float64) string { return gasNameAt(gas, t) }

	for gi, gID := range groupOrder {
		grp := groups[gID]
		spanStart, spanEnd := peakSpan(grp)
		bounds := groupBounds(dtg, cs, spanStart, spanEnd, params)

		// 候选 1: 合并单阶段
		merged := StageCandidate{
			Kind:      "merged",
			PeakGroup: gID,
			Support:   []string{seriesID, deps.DTGID},
		}
		if len(grp) > 1 {
			merged.Note = "重叠峰(" + itoa(len(grp)) + " 个峰位于合并窗 " +
				ftoa(params.PeakMergeWindowMin) + " min 内), 合并为单一阶段"
		} else {
			merged.Kind = "single"
		}
		mainPeak := grp[0]
		for _, pk := range grp[1:] {
			if pk.RateMgPerMin > mainPeak.RateMgPerMin {
				mainPeak = pk
			}
		}
		mSt := buildStage(1, bounds, mainPeak, cs, gasAt)
		merged.Stages = []Stage{mSt}
		merged.ID = stableID("cand", deps.ParamFingerprint, merged.Kind,
			itoa(gi), ftoa(mSt.StartMin), ftoa(mSt.EndMin))
		validateCandidate(&merged, cs, bounds)
		set.Candidates = append(set.Candidates, merged)

		// 候选 2: 拆分多阶段(重叠峰时才有意义, 单峰时直接拒绝)
		split := StageCandidate{
			Kind:      "split",
			PeakGroup: gID,
			Support:   []string{seriesID, deps.DTGID},
		}
		for si, pk := range grp {
			sb := perPeakBounds(dtg, pk, params)
			st := buildStage(si+1, sb, pk, cs, gasAt)
			split.Stages = append(split.Stages, st)
		}
		// 重叠标注
		for i := range split.Stages {
			for j := range split.Stages {
				if i != j && intervalsOverlap(split.Stages[i], split.Stages[j]) {
					split.Stages[i].OverlapsWith = appendUnique(split.Stages[i].OverlapsWith, split.Stages[j].Index)
				}
			}
		}
		split.ID = stableID("cand", deps.ParamFingerprint, split.Kind,
			itoa(gi), stageKey(split.Stages))
		if len(grp) < 2 {
			split.Status = "rejected"
			split.RejectReasons = append(split.RejectReasons,
				"单峰无法拆分: 拆分候选需要至少两个可分辨峰, 缺少第二个 DTG 极大值")
			split.Note = "保留被拒绝候选以记录为何不拆分"
		} else {
			split.Note = "重叠峰拆分为 " + itoa(len(grp)) + " 个可重叠阶段, 失重和不强制为 100%"
		}
		validateCandidate(&split, cs, bounds)
		set.Candidates = append(set.Candidates, split)
	}
	return set
}

type bounds struct {
	startMin, endMin float64
	startStatus      string
	endStatus        string
	crossesGap       bool
}

func peakSpan(grp []DTGPeak) (float64, float64) {
	a, b := grp[0].TimeMin, grp[0].TimeMin
	for _, pk := range grp[1:] {
		a = math.Min(a, pk.TimeMin)
		b = math.Max(b, pk.TimeMin)
	}
	return a, b
}
