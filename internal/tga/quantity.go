package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// Q 是带单位与原始表示的领域量。Raw 永不改写；Value 仅在派生层规范化时填充。
// 无法转换时 Status=="unknown"，Value==0，数据留在原位置并携带 Diagnostic。
type Q struct {
	Value      float64 `json:"v"`
	Unit       string  `json:"u"`
	Raw        string  `json:"raw"`
	Status     string  `json:"status"` // ok | unknown
	Diagnostic string  `json:"diag,omitempty"`
}

func QOK(v float64, unit, raw string) Q {
	return Q{Value: v, Unit: unit, Raw: raw, Status: "ok"}
}
func QUnknown(raw, unit, why string) Q {
	return Q{Unit: unit, Raw: raw, Status: "unknown", Diagnostic: why}
}

func (q Q) String() string {
	if q.Status == "unknown" {
		return fmt.Sprintf("%s(?%s:%s)", q.Raw, q.Unit, q.Diagnostic)
	}
	return fmt.Sprintf("%g %s", q.Value, q.Unit)
}

// ---- 规范化换算（派生层）。规范单位：时间 s，温度 °C，质量 mg，百分比 % ----

var timeToSec = map[string]float64{
	"ms": 0.001, "s": 1, "sec": 1, "secs": 1, "min": 60, "minute": 60, "minutes": 60, "h": 3600, "hr": 3600, "hour": 3600, "hours": 3600,
}
var massToMg = map[string]float64{
	"ug": 0.001, "µg": 0.001, "mg": 1, "g": 1000, "kg": 1e6,
}
var tempToC = map[string]float64{"c": 1, "°c": 1, "degc": 1, "k": 1, "f": 1}
var pctOne = map[string]float64{"%": 1, "pct": 1, "percent": 1, "fraction": 100}

// ParseTime 接受 "12.5 min"、"300s" 等，规范化为秒。
func ParseTime(raw string) Q {
	v, u, err := splitNU(raw)
	if err != nil {
		return QUnknown(raw, "s", err.Error())
	}
	if u == "" {
		u = "s"
	}
	f, ok := timeToSec[strings.ToLower(u)]
	if !ok {
		return QUnknown(raw, "s", "unknown time unit "+u)
	}
	return QOK(v*f, "s", strings.TrimSpace(raw))
}

// ParseMass 规范化为 mg。
func ParseMass(raw string) Q {
	v, u, err := splitNU(raw)
	if err != nil {
		return QUnknown(raw, "mg", err.Error())
	}
	if u == "" {
		return QUnknown(raw, "mg", "mass requires unit")
	}
	f, ok := massToMg[strings.ToLower(u)]
	if !ok {
		return QUnknown(raw, "mg", "unknown mass unit "+u)
	}
	return QOK(v*f, "mg", strings.TrimSpace(raw))
}

// ParseTemp 规范化为 °C（支持 K、F）。
func ParseTemp(raw string) Q {
	v, u, err := splitNU(raw)
	if err != nil {
		return QUnknown(raw, "°C", err.Error())
	}
	if u == "" {
		return QUnknown(raw, "°C", "temperature requires unit")
	}
	switch strings.ToLower(u) {
	case "c", "°c", "degc":
		return QOK(v, "°C", strings.TrimSpace(raw))
	case "k":
		return QOK(v-273.15, "°C", strings.TrimSpace(raw))
	case "f":
		return QOK((v-32)*5/9, "°C", strings.TrimSpace(raw))
	}
	return QUnknown(raw, "°C", "unknown temperature unit "+u)
}

// ParsePercent 支持 "12%" 或 "0.12 fraction"（0~1 的小数乘 100）。
func ParsePercent(raw string) Q {
	v, u, err := splitNU(raw)
	if err != nil {
		return QUnknown(raw, "%", err.Error())
	}
	if u == "" {
		return QUnknown(raw, "%", "percent requires unit")
	}
	f, ok := pctOne[strings.ToLower(u)]
	if !ok {
		return QUnknown(raw, "%", "unknown percent unit "+u)
	}
	return QOK(v*f, "%", strings.TrimSpace(raw))
}

func splitNU(raw string) (v float64, unit string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "", fmt.Errorf("empty value")
	}
	i := 0
	for i < len(s) && (isDigit(s[i]) || s[i] == '.' || s[i] == '-' || s[i] == '+' || s[i] == 'e' || s[i] == 'E') {
		i++
	}
	num := strings.TrimSpace(s[:i])
	unit = strings.TrimSpace(s[i:])
	if num == "" {
		return 0, "", fmt.Errorf("no numeric prefix in %q", raw)
	}
	v, err = strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, "", fmt.Errorf("bad number in %q", raw)
	}
	return v, unit, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// Valid 报告 Q 是否可参与派生计算。
func (q Q) Valid() bool { return q.Status == "ok" }
