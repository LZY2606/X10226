// Package compute 实现派生层：规范化、漂移/空白校正、DTG、
// 气氛有效时间线、阶段候选与残留率计算链。
// 候选、拒绝原因、误差与未知状态都是一等数据。
package compute

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"tga-review/internal/quantity"
)

// AlgoVersion 是派生算法版本；任何派生值都携带它。
const AlgoVersion = "tga-algo/1.0.0"

// Params 是全部可调参数；指纹用于派生结果的参数指纹。
type Params struct {
	TransportDelayS       float64 `json:"transport_delay_s"`         // 气氛传输延迟：阀到样品
	ThermalLagS           float64 `json:"thermal_lag_s"`             // 温度滞后：炉温到样品温度
	GapFactor             float64 `json:"gap_factor"`                // 采样缺口判定：dt > GapFactor*中位dt
	SmoothWindow          int     `json:"smooth_window"`             // DTG 平滑窗口（奇数）
	PeakThresholdMgPerMin float64 `json:"peak_threshold_mg_per_min"` // 失重速率阈值
	OverlapReturnFraction float64 `json:"overlap_return_fraction"`   // 峰谷回落比例，低于才可分离
	NoiseMaxInversions    int     `json:"noise_max_inversions"`      // 非单调噪声：允许的符号反转数
	NoiseAmplitudeMg      float64 `json:"noise_amplitude_mg"`        // 计入噪声的最小幅度
}

// DefaultParams 给出默认参数。
func DefaultParams() Params {
	return Params{
		TransportDelayS:       45,
		ThermalLagS:           12,
		GapFactor:             3,
		SmoothWindow:          5,
		PeakThresholdMgPerMin: 0.05,
		OverlapReturnFraction: 0.3,
		NoiseMaxInversions:    8,
		NoiseAmplitudeMg:      0.005,
	}
}

// Fingerprint 计算参数指纹（规范化 JSON 的哈希）。
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// ChainNode 是计算链上的一个节点：每个显示值都能展开读回来源。
type ChainNode struct {
	Label    string          `json:"label"`
	Formula  string          `json:"formula,omitempty"`
	Value    *quantity.Value `json:"value,omitempty"`
	Sources  []string        `json:"sources,omitempty"` // 直接来源文档版本 ID
	Children []ChainNode     `json:"children,omitempty"`
}
