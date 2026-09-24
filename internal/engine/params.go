package engine

import (
	"sort"

	"tgaline/internal/domain"
)

const AlgoVersion = "tga-algo-1.0.0"

// DefaultParams are the baseline decomposition parameters. All carry SI-ish
// canonical units documented in ParamMeta.
func DefaultParams() map[string]float64 {
	return map[string]float64{
		"dtg_window_s":          30,   // central window width (s) for smoothed derivative
		"peak_prominence":       0.05, // mg/min minimum prominence
		"peak_min_separation_s": 20,   // s: within this, peaks form an overlap group
		"overlap_group_s":       45,   // s: window half-width around a peak cluster
		"gap_min_s":             90,   // s: >= this missing span is a sampling gap
		"noise_jump_mg":         0.20, // mg: |non-monotonic jump| above this is flagged
		"noise_neighbors":       3,    // points for local trend test
		"atmo_delay_s":          8,    // minimum transport delay valve->sample
		"atmo_lag_s":            40,   // maximum thermal/composition lag
		"residue_window_s":      30,   // averaging window for final residue
	}
}

// ParamMeta documents unit + meaning for every parameter.
var ParamMeta = map[string]struct{ Unit, Desc string }{
	"dtg_window_s":          {"s", "中心差分/平滑窗口宽度"},
	"peak_prominence":       {"mg/min", "DTG 峰最小显著度"},
	"peak_min_separation_s": {"s", "小于该间隔的峰视为重叠峰组"},
	"overlap_group_s":       {"s", "重叠峰组窗口半宽"},
	"gap_min_s":             {"s", "判定采样缺口的最小缺失时长"},
	"noise_jump_mg":         {"mg", "判为非单调噪声的最小反向跳变"},
	"noise_neighbors":       {"points", "局部趋势检验邻居数"},
	"atmo_delay_s":          {"s", "阀切换到样品环境的最小输运延迟"},
	"atmo_lag_s":            {"s", "温度/组成最大滞后"},
	"residue_window_s":      {"s", "最终残留率平均窗口"},
}

// fingerprintParams produces a stable short hash of a parameter set.
func fingerprintParams(p map[string]float64) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	arr := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		arr = append(arr, k, p[k])
	}
	return domain.ShortID("fp", arr)
}
