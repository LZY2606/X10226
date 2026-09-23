package domain

import (
	"math"
)

// Residue 是最终残留率结果。
type Residue struct {
	ID               string   `json:"id"`
	ValuePercent     float64  `json:"valuePercent"`
	Unit             string   `json:"unit"` // percent_of_initial_mass
	FinalMassMg      float64  `json:"finalMassMg"`
	FinalMassUnit    string   `json:"finalMassUnit"`
	InitialMassMg    float64  `json:"initialMassMg"`
	FinalTimeMin     float64  `json:"finalTimeMin"`
	FinalTempC       float64  `json:"finalTempC"`
	Status           string   `json:"status"` // ok / unknown
	UnknownReasons   []string `json:"unknownReasons,omitempty"`
	Chain            Chain    `json:"chain"`
	DependsOn        []string `json:"dependsOn"`
	AlgoVersion      string   `json:"algoVersion"`
	ParamFingerprint string   `json:"paramFingerprint"`
}

// ChainNode 是计算链中的一步, 有稳定身份与单位。
type ChainNode struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Formula    string  `json:"formula,omitempty"`
	SourceRef  string  `json:"sourceRef,omitempty"`  // 直接来源稳定身份
	SourceKind string  `json:"sourceKind,omitempty"` // raw_submission / artifact / param_version
	Status     string  `json:"status,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// Chain 是可展开的计算链(有向无环, 按顺序呈现)。
type Chain struct {
	TargetID string      `json:"targetId"`
	Nodes    []ChainNode `json:"nodes"`
	Edges    [][2]string `json:"edges"`
}

// ResolveInitialMass 选择当前生效初重: 最新一条有效初重声明,
// 返回其在提交中的来源标识信息。
func ResolveInitialMass(layers LayerData) (InitialMass, bool) {
	var best InitialMass
	found := false
	for _, im := range layers.InitialMasses {
		if im.Mass.Status != StatusOK {
			continue
		}
		best, found = im, true // 已按导入顺序, 后者覆盖(修订)
	}
	return best, found
}

// ComputeResidue 计算最终残留率并构造计算链。
// 直接来源: CorrectedSeries(最终点) + 初重(分层) + 参数指纹。
// 缺口/非单调尾部会导致 unknown, 绝不插值。
func ComputeResidue(cs CorrectedSeries, initial InitialMass, initialSourceID string,
	deps DependencyRefs) Residue {
	r := Residue{
		Unit:             "percent_of_initial_mass",
		FinalMassUnit:    "mg",
		InitialMassMg:    initial.Mass.Value,
		AlgoVersion:      AlgoResidue,
		ParamFingerprint: deps.ParamFingerprint,
		DependsOn:        []string{deps.CorrectedSeriesID, initialSourceID},
	}
	// 找到最后一个有效校正点
	var last *CorrectedPoint
	for i := range cs.Points {
		p := &cs.Points[i]
		if !p.Gap && !math.IsNaN(p.TimeMin) && p.RawSampleSeq != 0 {
			last = p
		}
	}
	chain := Chain{}
	m0Node := ChainNode{
		ID:    stableID("node", "m0", initialSourceID, ftoa(initial.Mass.Value)),
		Label: "样品初重 m0 (" + initial.Basis + ")", Value: initial.Mass.Value,
		Unit: "mg", SourceRef: initialSourceID, SourceKind: "raw_submission",
		Note: "初重分层保存; 修订以新版本表达",
	}
	chain.Nodes = append(chain.Nodes, m0Node)
	if last == nil {
		r.Status = "unknown"
		r.UnknownReasons = append(r.UnknownReasons, "没有任何有效校正终点(全部落在缺口上)")
		r.ID = stableID("residue", deps.ParamFingerprint, "no-final-point", initialSourceID)
		r.Chain = chain
		return r
	}
	r.FinalMassMg = last.CorrectedMg
	r.FinalTimeMin = last.TimeMin
	r.FinalTempC = last.TempDegC
	finalNode := ChainNode{
		ID:    stableID("node", "mfinal", deps.CorrectedSeriesID, ftoa(last.TimeMin)),
		Label: "最终校正质量 m_final(最后一个有效采样点)",
		Value: last.CorrectedMg, Unit: "mg",
		Formula:   "m_final = m_raw - drift(t) - blank(t)",
		SourceRef: deps.CorrectedSeriesID, SourceKind: "artifact",
		Note: "取 t=" + ftoa(last.TimeMin) + " min; 缺口不插值",
	}
	pct := last.CorrectedMg / initial.Mass.Value * 100
	pctNode := ChainNode{
		ID: stableID("node", "pct", initialSourceID, deps.CorrectedSeriesID,
			ftoa(last.CorrectedMg)),
		Label: "残留率 = m_final / m0 * 100%",
		Value: pct, Unit: r.Unit,
		Formula: "m_final / m0 * 100%",
	}
	chain.Nodes = append(chain.Nodes, finalNode, pctNode)
	chain.Edges = append(chain.Edges,
		[2]string{m0Node.ID, pctNode.ID},
		[2]string{finalNode.ID, pctNode.ID})
	chain.TargetID = pctNode.ID

	r.ValuePercent = pct
	r.Status = "ok"
	if last.NonMonotonic {
		r.UnknownReasons = append(r.UnknownReasons,
			"终点带非单调噪声标记, 残留率可信但需人工复核")
	}
	// 检查最终点后是否存在未闭合缺口
	for _, g := range cs.Gaps {
		if g.ToMin >= last.TimeMin {
			r.UnknownReasons = append(r.UnknownReasons,
				"终点附近存在采样缺口, 末端质量可能被截断")
		}
	}
	r.ID = stableID("residue", deps.ParamFingerprint, initialSourceID,
		deps.CorrectedSeriesID, ftoa(last.TimeMin), ftoa(last.CorrectedMg))
	r.Chain = chain
	return r
}
