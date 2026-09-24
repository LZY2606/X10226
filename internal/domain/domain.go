// Package domain holds the immutable domain vocabulary of the TGA review
// system: stable identities, strata, dimensioned quantities and the raw
// entities imported from the thermal-analysis team.
package domain

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Identity conventions
//
// Every visible value carries a stable identity. Identities are content
// addressed: the same project, the same direct-source set, the same algorithm
// version and the same parameter fingerprint always produce the same id. That
// is why an export/import round-trip and an "empty local database" rebuild
// preserve identity, order and provenance.
//
//	raw:    raw_<sha256 of canonical payload>            (imported record)
//	batch:  b_<sha256>                                    (one import batch)
//	der:    d_<kind>_<sha256(sources|algoVersion|fp)>     (derived result)
//	event:  e_<seq>_<sha256(payload,parent)>              (append-only event)
//	disp:   v_<container>_<field>@<target-id>             (a shown value)

func ContentID(prefix string, payload any) (string, error) {
	b, err := json.Marshal(Canonical{V: payload})
	if err != nil {
		return "", err
	}
	sum := SHA256(b)
	return prefix + "_" + hex.EncodeToString(sum[:12]), nil
}

// MustID panics on hashing failure; only used with in-process structs.
func MustID(prefix string, payload any) string {
	id, err := ContentID(prefix, payload)
	if err != nil {
		panic(err)
	}
	return id
}

// Canonical forces deterministic JSON encoding for fingerprinting.
type Canonical struct{ V any }

// MarshalJSON renders maps with sorted keys.
func (c Canonical) MarshalJSON() ([]byte, error) {
	return json.Marshal(sortedClone(c.V))
}

func sortedClone(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return sortedMap(t)
	default:
		return v
	}
}

func sortedMap(t map[string]any) any {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, _ := json.Marshal(Canonical{V: t[k]})
		b.Write(vb)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// Stratum is the preservation layer a raw record belongs to. The six strata
// are kept separate so a reviewer can audit each source tier on its own.
type Stratum string

const (
	StratumRawSample Stratum = "raw_sample_points" // 原始采样点
	StratumDrift     Stratum = "balance_drift"     // 天平漂移校正
	StratumProgram   Stratum = "furnace_program"   // 炉程序
	StratumGasEvent  Stratum = "gas_valve_event"   // 气体阀事件
	StratumBlank     Stratum = "blank_curve"       // 空白曲线
	StratumInitial   Stratum = "initial_mass"      // 样品初重分层保存
)

// AllStrata returns layers in review order.
func AllStrata() []Stratum {
	return []Stratum{
		StratumRawSample, StratumDrift, StratumProgram,
		StratumGasEvent, StratumBlank, StratumInitial,
	}
}

func (s Stratum) Valid() bool {
	for _, x := range AllStrata() {
		if x == s {
			return true
		}
	}
	return false
}

// ImportStatus marks the fate of an individual imported fragment.
type ImportStatus string

const (
	StatusAccepted ImportStatus = "accepted" // 采纳
	StatusRejected ImportStatus = "rejected" // 未采纳（格式错误等）
	StatusStaged   ImportStatus = "staged"   // 半失败：原始字节保留，等待诊断
)

// Quantity is a value with its original textual representation, unit and the
// coordinate basis required to interpret it. Raw import keeps Original; only
// the derived layer normalizes via Normalize(). Unconvertible values stay at
// their original position with Diagnostics instead of being dropped.
type Quantity struct {
	Original   string    `json:"original"`                  // 原始表示，逐字保留
	Value      float64   `json:"value,omitempty"`           // 解析值（可能为 NaN）
	Valid      bool      `json:"valid"`                     // 是否成功解析
	Unit       string    `json:"unit"`                      // 单位（原始拼写）
	Basis      string    `json:"basis,omitempty"`           // 坐标基准
	Normalized string    `json:"normalized_unit,omitempty"` // 规范化后单位
	SIVector   []float64 `json:"si_vector,omitempty"`       // 规范化值（多数单位为单元素）
	Diagnostic string    `json:"diagnostic,omitempty"`      // 无法转换时的诊断
}

// Q builds a parsed quantity from free text, preserving the original token.
func Q(text, unit, basis string) Quantity {
	text = strings.TrimSpace(text)
	q := Quantity{Original: text, Unit: unit, Basis: basis}
	if text == "" {
		q.Valid = false
		q.Diagnostic = "empty token"
		return q
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		q.Valid = false
		q.Diagnostic = "not a number: " + err.Error()
		return q
	}
	q.Valid = true
	q.Value = v
	si, nu, ok := normalizeUnit(v, unit)
	if !ok {
		q.Diagnostic = "unconvertible unit: " + unit
		return q
	}
	q.SIVector = si
	q.Normalized = nu
	return q
}

// SI returns the normalized scalar and false when conversion is impossible.
func (q Quantity) SI() (float64, bool) {
	if !q.Valid || q.Diagnostic != "" || len(q.SIVector) != 1 {
		return 0, false
	}
	return q.SIVector[0], true
}

func normalizeUnit(v float64, unit string) ([]float64, string, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mg":
		return []float64{v * 1e-6}, "kg", true
	case "g":
		return []float64{v * 1e-3}, "kg", true
	case "ug", "µg":
		return []float64{v * 1e-9}, "kg", true
	case "k", "c", "°c", "degc":
		c := v
		if strings.EqualFold(unit, "k") {
			c = v - 273.15
		}
		return []float64{c + 273.15}, "K", true
	case "s", "sec", "secs":
		return []float64{v}, "s", true
	case "min":
		return []float64{v * 60}, "s", true
	case "mm/min", "mm·min⁻¹":
		return []float64{v / 60}, "mm/s", true
	case "%", "pct", "wt%", "wt-%":
		return []float64{v / 100}, "fraction", true
	case "%/min":
		return []float64{v / 100 / 60}, "fraction/s", true
	case "ml/min":
		return []float64{v / 60}, "mL/s", true
	case "":
		return []float64{v}, "", true
	default:
		return nil, "", false
	}
}

// Species is a gas species with original unit token preserved.
type Species struct {
	Name     string   `json:"name"`
	Flow     Quantity `json:"flow"`
	Original string   `json:"original"` // 原始行文本
}

// RawRecord is one immutable imported fragment.
type RawRecord struct {
	ID         string       `json:"id"`
	BatchID    string       `json:"batch_id"`
	ProjectID  string       `json:"project_id"`
	Stratum    Stratum      `json:"stratum"`
	Status     ImportStatus `json:"status"`
	LineNo     int          `json:"line_no"`
	Original   string       `json:"original"` // 输入片段原文
	Diagnostic string       `json:"diagnostic,omitempty"`
	Supersedes string       `json:"supersedes,omitempty"` // 初重修订：父版本 id
	Payload    any          `json:"payload,omitempty"`
	ReceivedAt string       `json:"received_at"` // 虚拟时间
}

// SamplePoint is one raw TGA sampling row (原始采样点).
type SamplePoint struct {
	FurnaceTime Quantity `json:"furnace_time"` // 炉时间 (min)
	Mass        Quantity `json:"mass"`         // 质量 (mg)
	Temperature Quantity `json:"temperature"`  // 样品/实测温度 (°C)
	Seq         int      `json:"seq"`
}

// DriftCorrection is a linear balance baseline-drift model (天平漂移校正).
type DriftCorrection struct {
	Slope     Quantity `json:"slope"`     // mg/min
	Intercept Quantity `json:"intercept"` // mg
	Source    string   `json:"source"`    // e.g. blank curve / manual
}

// BlankPoint is one row of the blank run (空白曲线).
type BlankPoint struct {
	FurnaceTime Quantity `json:"furnace_time"`
	Mass        Quantity `json:"mass"` // 空坩埚读数 mg
	Temperature Quantity `json:"temperature"`
	Seq         int      `json:"seq"`
}

// ProgramSegment is one furnace program segment (炉程序).
type ProgramSegment struct {
	FromTime  Quantity `json:"from_time"` // min
	ToTime    Quantity `json:"to_time"`
	Setpoint0 Quantity `json:"setpoint_0"` // °C at FromTime
	Setpoint1 Quantity `json:"setpoint_1"` // °C at ToTime
	Kind      string   `json:"kind"`       // ramp | hold
	Seq       int      `json:"seq"`
}

// GasValveEvent is one gas valve switch (气体阀事件).
type GasValveEvent struct {
	FurnaceTime Quantity  `json:"furnace_time"` // min —— 阀切换时间
	Action      string    `json:"action"`       // open|close|switch
	Species     []Species `json:"species"`
	Note        string    `json:"note,omitempty"`
	Seq         int       `json:"seq"`
}

// InitialMass is a stratum-stored sample initial mass (样品初重分层保存).
type InitialMass struct {
	Revision   int      `json:"revision"` // 1-based; revisions are versioned
	Mass       Quantity `json:"mass"`
	Reason     string   `json:"reason,omitempty"`
	Supersedes string   `json:"supersedes,omitempty"`
}

// Batch is the result record of one import attempt (含半失败).
type Batch struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"project_id"`
	Kind       string   `json:"kind"` // multi_strata_csv | initial_mass | ...
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	Total      int      `json:"total"`
	Accepted   int      `json:"accepted"`
	Rejected   int      `json:"rejected"`
	Staged     int      `json:"staged"`
	HalfFailed bool     `json:"half_failed"` // 导入半失败
	Diagnostic string   `json:"diagnostic,omitempty"`
	RecordIDs  []string `json:"record_ids"`
}

func (b *Batch) GoString() string {
	return fmt.Sprintf("Batch(%s acc=%d rej=%d staged=%d half=%v)", b.ID[:10], b.Accepted, b.Rejected, b.Staged, b.HalfFailed)
}
