// Package cases holds the offline, domain-specific TGA case pack. Data are
// generated with a fixed seed and virtual time so replay is deterministic and
// never reaches the network or the host machine's database/files.
package cases

import (
	"encoding/json"
	"fmt"
	"math"
)

// Step is one replay action in a scenario.
type Step struct {
	Kind    string          `json:"kind"` // import | params | decide | undo | note
	Title   string          `json:"title"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// Scenario is a self-contained TGA narrative.
type Scenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Expectation string `json:"expectation"`
	Steps       []Step `json:"steps"`
}

type importDoc struct {
	Layers []map[string]any `json:"layers"`
	Params *map[string]any  `json:"params,omitempty"`
}

const seedStepS = 30.0

// tgaData is the fixed synthetic sample: 30 min run, 30 s cadence, one drift
// component, three loss lobes (two overlapping), one noise blip and one wider
// sampling gap.
type tgaData struct {
	times   []float64
	rawMass []float64 // includes positive balance drift
	trueMass []float64
	temp    []float64
	present []bool
}

func genData() tgaData {
	var d tgaData
	loss := func(t, c, w, amp float64) float64 {
		return amp * 0.5 * (1 + math.Tanh((t-c)/w))
	}
	for i := 0; i <= 60; i++ {
		t := float64(i) * seedStepS
		trueMass := 100 -
			loss(t, 150, 15, 6) -     // moisture lobe, clearly separated
			loss(t, 750, 120, 42) -   // main decomposition (broad tail)
			loss(t, 1250, 70, 10)     // overlapping oxidative lobe (tail merges)
		drift := 0.002 * t // upward baseline drift, removed at derivation
		raw := trueMass + drift
		temp := 30.0
		if t > 300 {
			temp = 30 + (t-300)/6.0
		}
		// non-monotonic noise blip that recovers within three samples
		if i == 48 { // t=1440, inside the oxidative lobe
			raw += 0.09
		}
		d.times = append(d.times, t)
		d.trueMass = append(d.trueMass, trueMass)
		d.rawMass = append(d.rawMass, raw)
		d.temp = append(d.temp, temp)
		present := true
		// sampling gap of 3 missing points on the dry plateau 330-390 s
		// (wider than GapMaxBridgeN=1, so it is never interpolated across)
		if i >= 11 && i <= 13 {
			present = false
		}
		d.present = append(d.present, present)
	}
	return d
}

func samplesLayer(d tgaData) map[string]any {
	rows := []map[string]any{}
	for i := range d.times {
		row := map[string]any{
			"index": i,
			"time":  fmt.Sprintf("%.1f", d.times[i]), "time_unit": "s",
			"furnace_time": fmt.Sprintf("%.1f", d.times[i]), "furnace_time_unit": "s",
			"temp": fmt.Sprintf("%.2f", d.temp[i]), "temp_unit": "C",
			"present": d.present[i],
		}
		if d.present[i] {
			row["mass"] = fmt.Sprintf("%.4f", d.rawMass[i])
			row["mass_unit"] = "mg"
		} else {
			row["mass"] = ""
			row["mass_unit"] = "mg"
		}
		rows = append(rows, row)
	}
	return map[string]any{"type": "samples", "label": "raw TG acquisition (30 s cadence)", "samples": rows}
}

func driftLinearLayer() map[string]any {
	return map[string]any{"type": "drift", "label": "balance baseline drift (linear)",
		"drift": map[string]any{"kind": "linear", "slope": "0.002", "intercept": "0"}}
}

func driftBlankLayer() map[string]any {
	return map[string]any{"type": "drift", "label": "balance baseline drift (blank curve)",
		"drift": map[string]any{"kind": "blank_curve", "blank_id": "blank-v1"}}
}

func furnaceLayer() map[string]any {
	return map[string]any{"type": "furnace", "label": "furnace temperature program",
		"program": []map[string]any{
			{"from": "0", "to": "300", "start_temp": "30", "target_temp": "30"},
			{"from": "300", "to": "1800", "start_temp": "30", "target_temp": "280", "rate": "10"},
		}}
}

func gasLayer() map[string]any {
	return map[string]any{"type": "gas", "label": "gas valve log",
		"gas": []map[string]any{
			{"at": "1200", "from_gas": "N2", "to_gas": "Air", "flow": "50"},
		}}
}

func blankLayer() map[string]any {
	rows := []map[string]any{}
	for i := 0; i <= 12; i++ {
		t := float64(i) * 150
		rows = append(rows, map[string]any{
			"time": fmt.Sprintf("%.1f", t), "mass": fmt.Sprintf("%.4f", 0.0005*t),
			"temp": "25",
		})
	}
	return map[string]any{"type": "blank", "label": "empty-pan blank curve", "blank": rows}
}

func initMassLayer(v, basis, note string) map[string]any {
	return map[string]any{"type": "initial_mass", "label": "sample initial mass",
		"initial_mass": map[string]any{"value": v, "unit": "mg", "basis": basis, "note": note}}
}

func baselineDoc(initV string) importDoc {
	d := genData()
	return importDoc{Layers: []map[string]any{
		samplesLayer(d), driftLinearLayer(), furnaceLayer(), gasLayer(), blankLayer(),
		initMassLayer(initV, "as_loaded", "balance reading at load"),
	}}
}

func paramsDoc(p map[string]any) map[string]any { return p }

func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func importStep(title string, doc importDoc) Step {
	return Step{Kind: "import", Title: title, Body: marshal(doc)}
}

// All returns the offline scenarios.
func All() []Scenario {
	base := baselineDoc("100")

	// 01 full narrative
	narrative := Scenario{
		ID: "01-mass-loss-lineage", Name: "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Summary: "六分层基线导入，随后参数修订、局部源数据修订、互相冲突的人工决定与撤销。",
		Expectation: "TG/DTG/炉程序/气氛/阶段同屏；重叠峰保留全部候选；残留率可展开计算链；回到旧事件恢复旧结论。",
		Steps: []Step{
			importStep("导入基线：原始采样点、漂移校正、炉程序、气阀、空白、初重", base),
			{Kind: "note", Title: "审阅：核对六分层与漂移校正后的 TG/DTG"},
			{Kind: "params", Title: "参数修订：气氛延迟 90 s、温度滞后 12 °C", Body: marshal(paramsDoc(map[string]any{
				"smoothing_window_n": 2, "min_prominence_mg_per_min": 0.05,
				"min_stage_loss_pct": 1.0, "gas_delay_s": 90, "temp_lag_c": 12,
				"gap_max_bridge_n": 1, "noise_monotonic_window_n": 3}))},
			{Kind: "import", Title: "局部源数据改动：初重修订 100 -> 102 mg（仅残留率应过期）",
				Body: marshal(importDoc{Layers: []map[string]any{
					initMassLayer("102", "corrected", "re-tared against reference weight")}})},
			{Kind: "decide", Title: "人工决定：接受主分解阶段",
				Body: marshal(map[string]any{"target_type": "stage", "match_label": "Stage 2",
					"action": "accept", "reason": "主峰与 N2/Air 切换证据一致"})},
			{Kind: "decide", Title: "冲突决定：否定同一阶段（产生冲突，不覆盖前者）",
				Body: marshal(map[string]any{"target_type": "stage", "match_label": "Stage 2",
					"action": "reject", "reason": "怀疑与重叠峰未分开"})},
			{Kind: "undo", Title: "撤销否定，恢复接受状态（撤销本身入历史）",
				Body: marshal(map[string]any{"match_label": "Stage 2"})},
		},
	}

	// 02 atmosphere delay standalone
	delay := Scenario{
		ID: "02-atmosphere-delay", Name: "气氛延迟与温度滞后",
		Summary: "阀切换时间不等于样品反应时间；阶段可重叠且不强制总和 100%。",
		Expectation: "出现 atmosphere_delay / temperature_lag 判定，气氛时间线显示阀时间与样品时间两个坐标。",
		Steps: []Step{
			importStep("导入基线", baselineDoc("100")),
			{Kind: "params", Title: "设定延迟 90 s、滞后 12 °C", Body: marshal(map[string]any{
				"gas_delay_s": 90, "temp_lag_c": 12})},
		},
	}

	// 03 drift switch
	drift := Scenario{
		ID: "03-baseline-drift", Name: "天平基线漂移",
		Summary: "线性漂移改为空白曲线扣除，series 及其下游过期。",
		Expectation: "出现 drift_blank 判定；漂移方式与直接来源可追溯。",
		Steps: []Step{
			importStep("导入基线（线性漂移）", baselineDoc("100")),
			{Kind: "import", Title: "改用空白曲线漂移校正", Body: marshal(importDoc{
				Layers: []map[string]any{driftBlankLayer()}})},
		},
	}

	// 04 half failure
	bad := map[string]any{
		"type": "samples", "label": "损坏的采样点批次",
		"samples": []map[string]any{
			{"index": 1000, "time": "3000", "time_unit": "s", "mass": "9.0", "mass_unit": "banana",
				"temp": "25", "temp_unit": "C", "present": true},
		},
	}
	half := Scenario{
		ID: "04-import-half-failure", Name: "导入半失败",
		Summary: "同一文档中气体阀修订有效、采样点因未知单位被拒。",
		Expectation: "气体层成为新版本并重算；采样片段与格式错误被保留为 import_rejected。",
		Steps: []Step{
			importStep("导入基线", baselineDoc("100")),
			{Kind: "import", Title: "半失败批次：气体有效 + 采样点单位错误", Body: marshal(importDoc{
				Layers: []map[string]any{gasLayer(), bad}})},
		},
	}

	// 05 initial mass locality
	local := Scenario{
		ID: "05-initial-mass-locality", Name: "初重修订局部失效",
		Summary: "初重从 100 改为 102 mg。",
		Expectation: "series/dtg/stages 身份不变，仅 residue 产生新版本。",
		Steps: []Step{
			importStep("导入基线", baselineDoc("100")),
			{Kind: "import", Title: "初重修订为 102 mg", Body: marshal(importDoc{
				Layers: []map[string]any{initMassLayer("102", "corrected", "revised after re-tare")}})},
		},
	}

	// 06 explicit document format error
	badDoc := Scenario{
		ID: "06-format-error", Name: "明确的格式错误",
		Summary: "提交不是 JSON 的样例，验证文档级拒绝。",
		Expectation: "整条文档被拒并保留片段，不产生任何源版本。",
		Steps: []Step{{Kind: "import_raw", Title: "损坏 JSON",
			Body: marshal(map[string]any{"raw": `{"layers": [ }`})}},
	}

	return []Scenario{narrative, delay, drift, half, local, badDoc}
}

// ByID finds a scenario.
func ByID(id string) *Scenario {
	all := All()
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	return nil
}
