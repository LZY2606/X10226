package domain

// Rule 是机器可读取的领域判断依据。派生结果、候选与事件都引用 Rule.Code。
type Rule struct {
	Code        string
	Title       string
	Statement   string
}

const (
	RuleAtmosphereLag   = "TGA-ATMOS-01" // 阀切换时间 != 样品反应时间
	RuleOverlapStages   = "TGA-STAGE-02" // 阶段可重叠且不强制总和为 100%
	RuleNoGapInterp     = "TGA-GAP-03"   // 数据缺口不跨越插值
	RuleBaselineDrift   = "TGA-DRIFT-04" // 天平基线漂移须校正
	RuleResidualBasis   = "TGA-RESID-05" // 残留率分母 = 样品初重(净重, 同一基准)
	RuleNonMonotonic    = "TGA-NOISE-06" // 非单调噪声不得被解释为增重阶段
	RuleCandidateAll    = "TGA-CAND-07"  // 无法唯一决定时保留全部合法候选
	RulePartialImport   = "TGA-IMPORT-08" // 导入半失败：失败层不污染其他层
	RuleRawImmutable    = "TGA-RAW-09"    // 原始输入不可覆盖，只能修订新版本
	RuleUnitPreserve    = "TGA-UNIT-10"   // 原始表示与单位保留，不可转换留在原位
)

func RuleBook() []Rule {
	return []Rule{
		{RuleAtmosphereLag, "气氛延迟与温度滞后",
			"气体阀切换时间不是样品处反应开始时间；反应窗口 = 阀事件时间 + 气氛延迟 + 温度滞后，且不同气氛的延迟可不同。"},
		{RuleOverlapStages, "阶段重叠与非封闭质量平衡",
			"阶段按反应证据划分，允许时间重叠；所有阶段失重之和不强制等于 100%，残差进入未归属/未知。"},
		{RuleNoGapInterp, "缺口不插值",
			"采样缺口区间保持未知，不得跨越缺口做线性/平滑插值；依赖该区间的候选标记 unknown 并保留缺口证据。"},
		{RuleBaselineDrift, "天平基线漂移",
			"质量通道必须先按空白曲线/漂移点校正；校正量超过容差时给出诊断，未校正曲线不得作为残留率来源。"},
		{RuleResidualBasis, "残留率基准",
			"残留率 = 残留质量 / 同一基准样品净重初值；初重修订后旧版本残留率仍可查看但标记 stale。"},
		{RuleNonMonotonic, "非单调噪声",
			"DTG 小幅非单调抖动若低于峰阈值/显著性则判为噪声，拒绝据此划分增重阶段。"},
		{RuleCandidateAll, "保留全部合法候选",
			"重叠峰/多解时枚举全部合法分解候选，记录拒绝原因，人工选择前不得静默丢弃。"},
		{RulePartialImport, "半失败隔离",
			"一次导入中的格式错误层只记录诊断并拒绝该层，其他层正常提交，批次保留半失败状态。"},
		{RuleRawImmutable, "原始层不可变",
			"已提交的原始文档只读；修订生成新版本文档并指向父版本，派生结果携带直接来源。"},
		{RuleUnitPreserve, "单位与原始表示",
			"导入时保留原始字符串与单位，派生层才做规范化；无法转换的值留在原位置并附诊断。"},
	}
}
