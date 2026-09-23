// genfixtures produces the committed offline TGA case pack from fixed seeds
// and deterministic clocks. Run: go run ./internal/tools/genfixtures
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

const dir = "internal/fixtures/cases/tga_lineage"

func main() {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	must("sample_points_v1.csv", sampleV1())
	must("sample_points_v2_gas_sensor.csv", sampleV2())
	must("blank_curve_v1.csv", blankV1())
	must("furnace_program_v1.csv", furnaceV1())
	must("gas_valve_events_v1.csv", gasV1())
	must("gas_valve_events_v2_corrected.csv", gasV2())
	must("initial_mass_v1.csv", tareV1())
	must("initial_mass_v2_revision.csv", tareV2())
	must("partial_failed_points.csv", partialPoints())
	must("manifest.json", manifest())
	must("README.md", readme())
	fmt.Println("fixtures generated in", dir)
}

func must(name, content string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// ---- deterministic curve model ---------------------------------------------
//
// Sample mass (mg), t in seconds:
//   moisture loss 1.10 mg centered at 200 s
//   overlapping oxidations: 1.45 mg at 600 s + 0.85 mg at 690 s (saddle)
//   plateau, residue = 15.00 - 1.10 - 1.45 - 0.85 = 11.60 mg
const (
	m0        = 15.000
	drop1     = 1.10
	drop2     = 1.45
	drop3     = 0.85
	c1, w1    = 200.0, 38.0
	c2, w2    = 600.0, 46.0
	c3, w3    = 690.0, 40.0
	tEnd      = 1200.0
	dt        = 2.0
)

func logistic(t, center, width, drop float64) float64 {
	return drop / (1 + math.Exp(-(t - center)/width))
}

func trueMass(t float64) float64 {
	return m0 - logistic(t, c1, w1, drop1) - logistic(t, c2, w2, drop2) - logistic(t, c3, w3, drop3)
}

func driftMg(t float64) float64 {
	// balance baseline drift: slight upward slope + curvature
	return 0.000020*t + 0.000000012*t*t - 0.004
}

func sampleTemp(t float64) float64 {
	// measured sample temperature lags furnace setpoint; held 30..900
	prog := furnaceSetpoint(t)
	lag := 18*math.Exp(-t/650) + 4 // larger lag early on
	return prog - lag
}

func furnaceSetpoint(t float64) float64 {
	switch {
	case t < 120:
		return 30
	case t < 600:
		return 30 + (650-30)*(t-120)/(600-120)
	case t < 900:
		return 650
	default:
		return 650 - (t-900)/300*350
	}
}

// noiseNoise produces reproducible small non-monotonic noise (mg).
func noiseAt(rng *rand.Rand, i int) float64 {
	// gaussian-ish from 3 uniforms, fixed seed
	u := (rng.Float64() + rng.Float64() + rng.Float64()) / 3
	return (u - 0.5) * 0.006
}

func sampleV1() string {
	rng := rand.New(rand.NewSource(20260923))
	b := csvHead("time_s", "raw_mass_mg", "sample_temp_c", "note")
	for i, t := 0, 0.0; t <= tEnd+1e-9; i, t = i+1, t+dt {
		// sampling gap: rows 356..372 (t=712..744 s) entirely missing
		if t >= 712-1e-9 && t <= 744+1e-9 {
			continue
		}
		m := trueMass(t) + driftMg(t) + noiseAt(rng, i)
		temp := sampleTemp(t)
		note := ""
		if i == 200 { // one unconvertible temperature: raw kept, value stays in place
			note = ""
		}
		line := fmt.Sprintf("%.1f,%.5f,%.2f,%s\n", t, m, temp, note)
		if i == 200 {
			line = fmt.Sprintf("%.1f,%.5f,%s,\n", t, m, "NA")
		}
		b.WriteString(line)
	}
	return b.String()
}

func sampleV2() string {
	rng := rand.New(rand.NewSource(20260924))
	b := csvHead("time_s", "raw_mass_mg", "sample_temp_c", "note")
	for i, t := 0, 0.0; t <= tEnd+1e-9; i, t = i+1, t+dt {
		if t >= 712-1e-9 && t <= 744+1e-9 {
			continue
		}
		m := trueMass(t) + driftMg(t) + noiseAt(rng, i)
		b.WriteString(fmt.Sprintf("%.1f,%.5f,%.2f,gas-sensor recalibration trace\n", t, m, sampleTemp(t)))
	}
	return b.String()
}

func blankV1() string {
	rng := rand.New(rand.NewSource(20260925))
	b := csvHead("time_s", "blank_mass_mg")
	for t := 0.0; t <= tEnd+1e-9; t += dt {
		// blank records the same balance drift family, smaller amplitude
		m := driftMg(t) + (rng.Float64()-0.5)*0.002
		b.WriteString(fmt.Sprintf("%.1f,%.5f\n", t, m))
	}
	return b.String()
}

func furnaceV1() string {
	b := csvHead("time_s", "setpoint_c")
	for t := 0.0; t <= tEnd+1e-9; t += 10 {
		b.WriteString(fmt.Sprintf("%.0f,%.1f\n", t, furnaceSetpoint(t)))
	}
	return b.String()
}

// gas: valve switches to O2 at 500 s; the true reaction at the sample starts
// 45 s later (transport + temperature lag), aligned with the second drop.
func gasV1() string {
	b := csvHead("time_s", "valve_state", "flow_ml_min", "note")
	b.WriteString("0,N2,120,初始氮气\n")
	b.WriteString("500,O2,120,阀切氧；记录存在 45 s 气氛延迟\n")
	b.WriteString("900,N2,120,冷却段切回氮\n")
	return b.String()
}

// gasV2 is a LOCAL source revision: the valve timestamp is corrected from
// 500 s to 494 s; only atmosphere-dependent derived nodes go stale.
func gasV2() string {
	b := csvHead("time_s", "valve_state", "flow_ml_min", "note")
	b.WriteString("0,N2,120,初始氮气\n")
	b.WriteString("494,O2,120,阀时校准 −6 s（局部来源修订）\n")
	b.WriteString("900,N2,120,冷却段切回氮\n")
	return b.String()
}

func tareV1() string {
	b := csvHead("layer", "mass_mg", "basis", "note")
	b.WriteString("crucible,2148.60,crucible_only,空坩埚（分层保存，不并入样品初重）\n")
	b.WriteString("initial,15.000,sample_as_loaded,初次称量样品初重\n")
	return b.String()
}

func tareV2() string {
	b := csvHead("layer", "mass_mg", "basis", "note")
	b.WriteString("crucible,2148.60,crucible_only,空坩埚（分层保存，不并入样品初重）\n")
	b.WriteString("initial,15.000,sample_as_loaded,初次称量样品初重\n")
	b.WriteString("revision,14.992,sample_corrected,称量复核：去皮微调 −0.008 mg（追加为新版本层，不覆盖旧值）\n")
	return b.String()
}

// partialPoints mixes valid rows with structural and value errors to exercise
// the half-failed import path; good rows remain usable, bad rows are kept.
func partialPoints() string {
	b := csvHead("time_s", "raw_mass_mg", "sample_temp_c", "note")
	b.WriteString("1000,11.610,300,补充点（有效）\n")
	b.WriteString("1004,11.606,298,补充点（有效）\n")
	b.WriteString("oops,not-a-number,299,无法转换的时间与质量：原始值留在原位并附诊断\n")
	b.WriteString("1008,,295,质量缺失：不插值\n")
	b.WriteString("1010,11.598,294,补充点（有效）\n")
	return b.String()
}

func csvHead(cols ...string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out + "\n"
}

func manifest() string {
	return `{
  "case_id": "tga_lineage_weight_loss",
  "title": "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
  "default_project_id": "demo-tga-lineage",
  "language": "zh-CN",
  "algorithm_version": "tga-stage/1.3.0",
  "steps": [
    {"op": "create_project", "id": "demo-tga-lineage", "name": "失重谱系：TGA 阶段分解、气氛切换与残留物证据链"},
    {"op": "import", "kind": "sample_points", "file": "sample_points_v1.csv", "expect": "ok"},
    {"op": "import", "kind": "blank_curve", "file": "blank_curve_v1.csv", "expect": "ok"},
    {"op": "import", "kind": "furnace_program", "file": "furnace_program_v1.csv", "expect": "ok"},
    {"op": "import", "kind": "gas_valve_events", "file": "gas_valve_events_v1.csv", "expect": "ok"},
    {"op": "import", "kind": "initial_mass", "file": "initial_mass_v1.csv", "expect": "ok"},
    {"op": "set_param", "params": {"gas_delay_s": {"value": 45, "unit": "s"}},
     "reason": "流量计与炉管传输实验测得 45 s 气氛延迟：阀切换时间 ≠ 样品反应时间", "note": "气氛延迟参数修订"},
    {"op": "expect", "asserts": [
       "peak_count==3",
       "overlap_group_count>=1",
       "gap_count>=1",
       "nonmonotonic_count>0",
       "residue_status==current",
       "residue_pct_between=77.0,77.7"
     ]},
    {"op": "import", "kind": "initial_mass", "file": "initial_mass_v2_revision.csv",
     "expect": "ok", "note": "初重修订：仅归一化类节点过期，TG/DTG 不过期"},
    {"op": "expect", "asserts": [
       "initial_mass_mg==14.992",
       "residue_pct_between=77.3,77.8"
     ]},
    {"op": "import", "kind": "gas_valve_events", "file": "gas_valve_events_v2_corrected.csv",
     "expect": "ok", "note": "局部源数据改动：阀时 −6 s，只让气氛归属相关节点过期"},
    {"op": "import", "kind": "sample_points", "file": "partial_failed_points.csv",
     "expect": "half", "note": "导入半失败：好行保留可用，坏行与诊断留档"},
    {"op": "decide", "action": "accept", "select": "stage:split",
     "reason": "热重团队确认重叠峰应拆分为两个氧化阶段"},
    {"op": "decide", "action": "accept", "select": "stage:merged",
     "reason": "另一位审阅者主张合并为单一放热平台——制造互相冲突的人工决定"},
    {"op": "resolve_conflict", "select_winner": "stage:split",
     "reason": "结合气氛延迟后的 O2 到达时间，拆分读法证据更强"},
    {"op": "adjust_stage", "select": "stage:peak1", "start_t": 150, "end_t": 268,
     "reason": "依据平台段手动收紧失水阶段窗口"},
    {"op": "undo_decision_of", "select": "stage:peak1",
     "reason": "复核会上撤回手工窗口，恢复算法候选"},
    {"op": "export_then_reimport", "note": "删除本地数据库后重新导入：身份、顺序、来源关系不变"},
    {"op": "checkout", "event": "before_initial_mass_revision",
     "note": "回到旧事件：看到的是当时的依赖版本与结论，不被最新结果覆盖"}
  ]
}
`
}

func readme() string {
	return `# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

本目录是仓库自带的**离线领域案例包**（固定 seed、可重放），不是通用表格。

## 数据文件（分层来源，提交后不可覆盖）

| 层 | 文件 | 内容 |
|---|---|---|
| 原始采样点 | sample_points_v1.csv | 1200 s、2 s 采样；含固定 seed 的非单调噪声、t=712–744 s 采样缺口、一处温度 NA |
| 空白曲线 | blank_curve_v1.csv | 空坩埚基线（天平漂移族） |
| 炉程序 | furnace_program_v1.csv | 30 °C 保持→升温→650 °C 平台→冷却（设定炉温） |
| 气体阀事件 | gas_valve_events_v1.csv | 阀 t=500 s 切 O2；样品实际 45 s 后才见氧 |
| 初重分层 | initial_mass_v1.csv | 坩埚皮重与样品初重分层；v2 为 14.992 mg 修订 |
| 半失败补充 | partial_failed_points.csv | 好行可用、坏行（NA 时间/质量）留档附诊断 |

## 内置的边界情形（重放必须实际经过）

1. 气氛延迟：gas_delay_s=45 s，阀切换时间不等于样品反应时间。
2. 温度滞后：样品实测温度滞后炉温设定值（早期约 22 °C）。
3. 天平基线漂移：空白扣除 + 平台段多项式拟合。
4. 重叠峰：600 s/690 s 两个氧化峰间存在浅鞍，拆分与合并都是合法候选。
5. 非单调噪声：局部质量回升被标记而不是被平滑删除。
6. 采样缺口：712–744 s 缺段；不跨缺口插值；阶段边界落入缺口时失重为未知。
7. 初重修订：15.000→14.992 mg 追加新版本层，只让归一化结果过期。
8. 导入半失败：partial_failed_points.csv 部分行无法转换，原始表示留在原位。

## 领域判断（机器可读取，见 manifest.json steps）

- 阀切换时间 ≠ 样品反应时间（gas_delay_s 与两条时间线并列展示）。
- 阶段可重叠，失重百分比不强制总和为 100%（重叠区间会重复计质量，剩余项单列）。
- 数据缺口不跨越插值；无法唯一决定时保留全部合法候选（split/merged/gap-ambiguous）。
- 局部输入变化只使依赖它的结果过期；过期结果可查看但不能冒充当前结果。
`
}
