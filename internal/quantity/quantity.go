// Package quantity 提供带单位与坐标基准的领域量。
// 原始表示（Raw）在导入层保留，规范化（To）只在派生层进行；
// 无法转换的值保留在原位置并附诊断，绝不为了画图而丢弃。
package quantity

import (
	"fmt"
	"strconv"
	"strings"
)

// Unit 是受支持的物理单位。
type Unit string

const (
	UnitSecond     Unit = "s"
	UnitMinute     Unit = "min"
	UnitHour       Unit = "h"
	UnitCelsius    Unit = "C"
	UnitKelvin     Unit = "K"
	UnitMg         Unit = "mg"
	UnitGram       Unit = "g"
	UnitPercent    Unit = "%"
	UnitMgPerMin   Unit = "mg/min"
	UnitMgPerS     Unit = "mg/s"
	UnitPctPerMin  Unit = "%/min"
	UnitMlPerMin   Unit = "ml/min"
	UnitCPerMin    Unit = "C/min"
	UnitDimensionl Unit = "1"
)

// Dim 是物理量纲类别。
type Dim string

const (
	DimTime        Dim = "time"
	DimTemperature Dim = "temperature"
	DimMass        Dim = "mass"
	DimPercent     Dim = "percent"
	DimMassRate    Dim = "mass_rate"
	DimPctRate     Dim = "pct_rate"
	DimFlow        Dim = "flow"
	DimHeatingRate Dim = "heating_rate"
	DimNone        Dim = "dimensionless"
)

// unitDim 记录每个单位所属量纲。
var unitDim = map[Unit]Dim{
	UnitSecond: DimTime, UnitMinute: DimTime, UnitHour: DimTime,
	UnitCelsius: DimTemperature, UnitKelvin: DimTemperature,
	UnitMg: DimMass, UnitGram: DimMass,
	UnitPercent:  DimPercent,
	UnitMgPerMin: DimMassRate, UnitMgPerS: DimMassRate,
	UnitPctPerMin:  DimPctRate,
	UnitMlPerMin:   DimFlow,
	UnitCPerMin:    DimHeatingRate,
	UnitDimensionl: DimNone,
}

// DimOf 返回单位所属量纲；未知单位返回空串。
func DimOf(u Unit) Dim { return unitDim[u] }

// Value 是一个带单位、坐标基准与原始表示的量。
type Value struct {
	Raw   string  `json:"raw"`             // 导入时的原始文本表示
	Val   float64 `json:"val"`             // 数值（未规范化）
	Unit  Unit    `json:"unit"`            // 原始单位
	Basis string  `json:"basis,omitempty"` // 坐标基准，如 furnace_time / sample_temp
	OK    bool    `json:"ok"`              // 数值是否可解析
	Diag  string  `json:"diag,omitempty"`  // 诊断信息（不可解析/不可转换时填写）
}

// Parse 解析原始文本为指定单位的量；失败时保留 Raw 并附诊断。
func Parse(raw string, unit Unit) Value {
	v := Value{Raw: raw, Unit: unit, OK: true}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		v.OK = false
		v.Diag = fmt.Sprintf("数值不可解析: %q", raw)
		return v
	}
	v.Val = f
	if _, known := unitDim[unit]; !known {
		v.OK = false
		v.Diag = fmt.Sprintf("未知单位: %q", string(unit))
	}
	return v
}

// To 把量转换到目标单位（派生层规范化）。
// 量纲不符或单位未知时返回 OK=false 的副本，原始值保留在 Raw/Val/Unit 中。
func (v Value) To(target Unit) Value {
	out := v
	if !v.OK {
		return out
	}
	srcDim, okSrc := unitDim[v.Unit]
	dstDim, okDst := unitDim[target]
	if !okSrc || !okDst || srcDim != dstDim {
		out.OK = false
		out.Diag = fmt.Sprintf("无法从 %q 转换到 %q", string(v.Unit), string(target))
		return out
	}
	out.Val = convert(v.Val, v.Unit, target)
	out.Unit = target
	return out
}

func convert(x float64, from, to Unit) float64 {
	if from == to {
		return x
	}
	// 先转到该量纲的基准单位，再转到目标。
	base := toBase(x, from)
	return fromBase(base, to)
}

func toBase(x float64, u Unit) float64 {
	switch u {
	case UnitMinute:
		return x * 60
	case UnitHour:
		return x * 3600
	case UnitKelvin:
		return x - 273.15
	case UnitGram:
		return x * 1000
	case UnitMgPerS:
		return x * 60
	default:
		return x
	}
}

func fromBase(x float64, u Unit) float64 {
	switch u {
	case UnitMinute:
		return x / 60
	case UnitHour:
		return x / 3600
	case UnitKelvin:
		return x + 273.15
	case UnitGram:
		return x / 1000
	case UnitMgPerS:
		return x / 60
	default:
		return x
	}
}

// String 给出人类可读的 "值 单位" 形式。
func (v Value) String() string {
	if !v.OK {
		return fmt.Sprintf("%s [%s; %s]", v.Raw, string(v.Unit), v.Diag)
	}
	return fmt.Sprintf("%.6g %s", v.Val, string(v.Unit))
}
