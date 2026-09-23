// Package domain: atmosphere 计算炉温曲线与气氛“反应窗口”。
//
// 关键判断(领域规则, 机器可读):
//   - 阀切换时间 != 样品反应时间。样品感受到新气氛的时间为
//     t_react = max(t_valve + atmoDelay, 样品达到反应特征温度的时间 + tempLag),
//     两个机制取较晚者, 因此反应窗口是一个范围 [earliest,latest], 不是单点。
package domain

import (
	"math"
	"sort"
)

// FurnacePoint 是炉程序在某时刻的设定/估计温度。
type FurnacePoint struct {
	TimeMin  float64 `json:"timeMin"`
	TempDegC float64 `json:"tempDegC"`
}

// FurnaceCurve 是分段线性炉温曲线。
type FurnaceCurve struct {
	Segs       []FurnaceSeg   `json:"segs"`
	Points     []FurnacePoint `json:"points,omitempty"`
	Valid      bool           `json:"valid"`
	Diagnostic string         `json:"diagnostic,omitempty"`
}

// BuildFurnaceCurve 校验并规范化炉程序。
func BuildFurnaceCurve(segs []FurnaceSeg) FurnaceCurve {
	fc := FurnaceCurve{Segs: segs}
	if len(segs) == 0 {
		fc.Diagnostic = "无炉程序段, 炉温未知"
		return fc
	}
	ordered := append([]FurnaceSeg(nil), segs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start.Minutes < ordered[j].Start.Minutes })
	for _, s := range ordered {
		if s.Start.Diagnostic != "" || s.End.Diagnostic != "" ||
			s.From.Status != StatusOK || s.To.Status != StatusOK {
			fc.Diagnostic = "炉程序含无法转换的值, 温度未知; 原始表示保留"
			return fc
		}
		if s.End.Minutes <= s.Start.Minutes {
			fc.Diagnostic = "炉程序段时长非正, 炉温曲线无法建立"
			return fc
		}
	}
	fc.Valid = true
	return fc
}

// TempAt 返回 t 时刻炉温; 仅在炉程序覆盖区间内有效。
func (fc FurnaceCurve) TempAt(t float64) (float64, bool) {
	if !fc.Valid {
		return 0, false
	}
	for _, s := range fc.Segs {
		if t >= s.Start.Minutes && t <= s.End.Minutes {
			frac := 0.0
			if d := s.End.Minutes - s.Start.Minutes; d > 0 {
				frac = (t - s.Start.Minutes) / d
			}
			return s.From.Value + frac*(s.To.Value-s.From.Value), true
		}
	}
	return 0, false
}

// TimeAtTemp 返回炉温达到 temp 的最早时刻(单调升温假设)。
func (fc FurnaceCurve) TimeAtTemp(temp float64) (float64, bool) {
	if !fc.Valid {
		return 0, false
	}
	segs := append([]FurnaceSeg(nil), fc.Segs...)
	sort.Slice(segs, func(i, j int) bool { return segs[i].Start.Minutes < segs[j].Start.Minutes })
	for _, s := range segs {
		lo, hi := s.From.Value, s.To.Value
		if lo > hi {
			lo, hi = hi, lo
		}
		if temp >= lo && temp <= hi {
			frac := 0.0
			if d := s.To.Value - s.From.Value; d != 0 {
				frac = (temp - s.From.Value) / d
			}
			return s.Start.Minutes + frac*(s.End.Minutes-s.Start.Minutes), true
		}
	}
	return 0, false
}

// ReactionWindow 是一次气氛切换在样品侧的可感知时间范围。
type ReactionWindow struct {
	GasEventSeq    int     `json:"gasEventSeq"`
	ValveTimeMin   float64 `json:"valveTimeMin"`
	FromGas        string  `json:"fromGas"`
	ToGas          string  `json:"toGas"`
	DelayEarliest  float64 `json:"delayEarliest"` // t_valve + atmoDelay
	DelayLatest    float64 `json:"delayLatest"`   // 温度滞后估计(未知时等于 earliest)
	WindowStartMin float64 `json:"windowStartMin"`
	WindowEndMin   float64 `json:"windowEndMin"`
	TempBasis      string  `json:"tempBasis"`
	UnknownLag     bool    `json:"unknownLag"`
	Note           string  `json:"note,omitempty"`
}

// ReactionWindows 依据气体阀事件计算样品反应窗口。
// reactionTempC: 该气氛下氧化/反应的特征温度(本案例以 O2 到达且炉温>=阈值为准),
// 未知时仅用气氛延迟, 窗口退化为单点并标记 unknownLag。
func ReactionWindows(gas []GasEvent, fc FurnaceCurve, params Params, reactionTempC float64) []ReactionWindow {
	var out []ReactionWindow
	for _, g := range gas {
		if g.Time.Diagnostic != "" {
			continue
		}
		w := ReactionWindow{
			GasEventSeq:    g.Seq,
			ValveTimeMin:   g.Time.Minutes,
			FromGas:        g.FromGas,
			ToGas:          g.ToGas,
			DelayEarliest:  g.Time.Minutes + params.AtmoDelayMin,
			DelayLatest:    g.Time.Minutes + params.AtmoDelayMin,
			WindowStartMin: g.Time.Minutes + params.AtmoDelayMin,
			WindowEndMin:   g.Time.Minutes + params.AtmoDelayMin,
		}
		if tt, ok := fc.TimeAtTemp(reactionTempC); ok {
			lagTime := tt + params.TempLagMin
			w.DelayLatest = math.Max(w.DelayEarliest, lagTime)
			w.WindowEndMin = w.DelayLatest
			w.WindowStartMin = w.DelayEarliest
			w.TempBasis = "furnace_ge_" + ftoa(reactionTempC) + "C"
			if lagTime > w.DelayEarliest {
				w.Note = "温度滞后主导: 反应不早于气氛到达, 不晚于热滞后完成"
			} else {
				w.Note = "气氛延迟主导: 阀切换后气体到达晚于热条件"
			}
		} else {
			w.UnknownLag = true
			w.TempBasis = "temperature_unknown"
			w.Note = "炉温曲线未覆盖反应特征温度, 温度滞后未知, 窗口退化为单点"
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ValveTimeMin < out[j].ValveTimeMin })
	return out
}
