// Command gen-case regenerates the offline TGA domain case pack from a fixed
// seed and fixed virtual origin. Run from the repository root:
//
//	go run ./cmd/gen-case
//
// Generated material is deliberately not a generic spreadsheet: it encodes a
// moisture loss, two overlapping decomposition peaks (shoulder), an oxidation
// stage that follows a delayed gas switch, linear balance drift, a blank
// curve, a declared sampling gap and isolated non-monotonic noise.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const outDir = "internal/cases/data/tga-stage-decomposition"

type stage struct {
	name      string
	loss, w, c float64
}

func logisticLoss(t float64, s stage) float64 {
	// cumulative fraction lost at time t
	z := (t - s.c) / s.w
	return s.loss / (1 + math.Exp(-z))
}

func main() {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}

	stages := []stage{
		{"moisture", 0.5, 10.5, 120},
		{"decomp_a", 3.2, 16.0, 700},
		{"decomp_b_shoulder", 0.7, 8.75, 745},
		{"oxidation", 0.9, 14.0, 1700},
	}
	const m0 = 10.0
	const driftRate = 0.0004 // mg/s positive baseline drift
	const blankEnd = 0.02    // mg at t=2700
	const end = 2700.0
	const step = 30.0
	noiseSpikes := map[float64]float64{2100: 0.005, 2130: -0.003}

	f, err := os.Create(filepath.Join(outDir, "v1_bundle.txt"))
	if err != nil {
		fail(err)
	}
	defer f.Close()

	fmt.Fprintln(f, "# raw_samples — original sampling points, every value keeps its unit")
	fmt.Fprintln(f, "time_s\ttemp_C\tmass_mg")
	for t := 0.0; t <= end+0.1; t += step {
		t = math.Round(t)
		if t == 1230 {
			continue // interior of the declared gap: row absent on purpose
		}
		temp := measuredTempC(t)
		lost := 0.0
		for _, s := range stages {
			lost += logisticLoss(t, s)
		}
		raw := m0 - lost + driftRate*t + blankEnd*t/end
		if d, ok := noiseSpikes[t]; ok {
			raw += d
		}
		if t == 1200 {
			// declared gap: sentinel row, never interpolated
			fmt.Fprintf(f, "%.0f\t%.2f\tGAP\n", t, temp)
			continue
		}
		fmt.Fprintf(f, "%.0f\t%.2f\t%.4f\n", t, temp, raw)
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "# drift_correction — balance baseline drift (constant rate model)")
	fmt.Fprintln(f, "rate: 0.0004 mg/s")
	fmt.Fprintln(f, "anchor_at: 0 s")
	fmt.Fprintln(f, "note: empty-pan linear baseline drift recorded before the run")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "# furnace_program — reference temperature program")
	fmt.Fprintln(f, "start: 30 C at 0 min")
	fmt.Fprintln(f, "ramp 30 C -> 600 C over 0 min -> 30 min")
	fmt.Fprintln(f, "hold 600 C 30 min -> 45 min")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "# gas_valve_events — as recorded at the valve, not at the sample")
	fmt.Fprintln(f, "0 s N2 50 mL/min initial inert atmosphere")
	fmt.Fprintln(f, "1620 s air 50 mL/min switch to oxidizing")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "# blank_curve — empty pan run")
	fmt.Fprintln(f, "time_s mass_mg")
	fmt.Fprintln(f, "0 s 0.000 mg")
	fmt.Fprintln(f, "1350 s 0.010 mg")
	fmt.Fprintln(f, "2700 s 0.020 mg")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "# initial_mass — stratified starting mass with basis")
	fmt.Fprintln(f, "mass: 10.000 mg")
	fmt.Fprintln(f, "at: 0 s")
	fmt.Fprintln(f, "basis: tared balance reading immediately before furnace start")

	writeFile("03_drift_revision.txt", `# drift_correction — revised baseline slope (local source revision)
rate: 0.0006 mg/s
anchor_at: 0 s
note: recalibrated from the pre-run hold; supersedes 0.0004 mg/s
`)
	writeFile("04_initial_mass_revision.txt", `# initial_mass — sample starting mass corrected after re-taring
mass: 10.012 mg
at: 0 s
basis: buoyancy-corrected tared reading; supersedes 10.000 mg
`)
	writeFile("05_half_failed_valves.txt", `# gas_valve_events — half-failed fragment: unparsable times, no usable event
yesterday N2 50 mL/min
soon air maybe
`)
	writeFile("06_partial_bundle.txt", `# gas_valve_events — valid replacement fragment
0 s Ar 50 mL/min initial inert atmosphere
1620 s air 50 mL/min switch to oxidizing

# unknown_instrument_dump — parser cannot accept this layer; fragment is kept
0xDEADBEEF ?? -- raw instrument fragment preserved with diagnostics
`)
	writeFile("README.md", readme)
	writeFile("manifest.json", manifestJSON)
	writeFile("REPLAY.md", replayDoc)
	fmt.Println("case pack written to", outDir)
}

func measuredTempC(t float64) float64 {
	// furnace reference
	var ref float64
	switch {
	case t <= 1800:
		ref = 30 + (600-30)*t/1800
	default:
		ref = 600
	}
	// measured sample temperature lags the reference during the ramp (about
	// 6 K), catching up within 100 s of the hold
	lag := 0.0
	switch {
	case t < 1800:
		lag = 6
	case t < 1900:
		lag = 6 * (1900 - t) / 100
	}
	return ref - lag
}

func writeFile(name, content string) {
	if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

const readme = `# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

离线、领域专用案例包（非通用表格）。所有材料随仓库分发，重放不需要网络。

## 分层基线（v1_bundle.txt，逐节保存）

- raw_samples：原始采样点（time_s / temp_C / mass_mg），1200 s 处为显式 GAP 行，
  缺口两侧禁止插值；2100/2130 s 有孤立非单调噪声。
- drift_correction：天平基线漂移，线性速率 0.0004 mg/s（原始读数已含该漂移）。
- furnace_program：炉程序，30 °C→600 °C/30 min 斜坡 + 15 min 恒温。
- gas_valve_events：气体阀事件（阀侧时间，非样品反应时间）。
- blank_curve：空白曲线（空坩埚），只允许区间内插值。
- initial_mass：样品初重分层保存，记录称量基准。

## 曲线中编码的领域事实

- 120 s 附近：失水峰（约 0.5 mg）。
- 700/745 s：两个重叠分解峰（主峰 + 肩峰），系统必须保留"各自成段"与
  "合并成段"两组合法候选；阶段可重叠，失重百分比不强制总和为 100%。
- 1620 s 切空气，样品在 18 s 传输延迟后才经历新气氛，氧化峰约 1700 s；
  斜坡段样品温度相对参考有约 6 K 滞后。
- 最终残留约 47 %（4.7 mg / 10.000 mg，校正漂移与空白后）。

## 边界情形重放（manifest.json 为机器可读事件，REPLAY.md 为人读叙事）

参数修订、局部源改动（漂移斜率）、初重修订、导入半失败、冲突裁定、冲突解决
与撤销依次发生；每一步带 affectsOnly 预期，用于核对影响是否被限制在正确范围。

固定 seed：20260919；虚拟时间起点：2026-01-01T00:00:00Z。
`

const manifestJSON = `{
  "id": "tga-stage-decomposition",
  "title": "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
  "seed": 20260919,
  "virtualStart": "2026-01-01T00:00:00Z",
  "files": [
    "v1_bundle.txt",
    "03_drift_revision.txt",
    "04_initial_mass_revision.txt",
    "05_half_failed_valves.txt",
    "06_partial_bundle.txt",
    "README.md",
    "REPLAY.md"
  ],
  "replay": [
    {"n": 1, "kind": "import_bundle", "file": "v1_bundle.txt", "expect": "six layers accepted; four peak candidates groups incl. one overlap cluster; final residue ~47%"},
    {"n": 2, "kind": "params", "label": "valve delay 18s -> 30s", "reason": "longer transfer line in this run", "expect": "only atmosphere reaction windows and atmosphere-derived views change"},
    {"n": 3, "kind": "import_layer", "layer": "drift_correction", "file": "03_drift_revision.txt", "expect": "only drift-dependent corrected mass, DTG, stages and residues go stale; valve/furnace identities unchanged"},
    {"n": 4, "kind": "import_layer", "layer": "initial_mass", "file": "04_initial_mass_revision.txt", "expect": "percent values change; mg values and raw points unchanged"},
    {"n": 5, "kind": "import_layer", "layer": "gas_valve_events", "file": "05_half_failed_valves.txt", "expect": "rejected fragment: current valve source and reaction windows unchanged; diagnostics retained"},
    {"n": 6, "kind": "import_bundle", "file": "06_partial_bundle.txt", "expect": "valve layer replaced; unknown layer fragment rejected and preserved in history"},
    {"n": 7, "kind": "rule", "target": "c1_p0", "decision": "accept", "reason": "shoulder resolved as distinct stage", "expect": "candidate marked ruled_accepted"},
    {"n": 8, "kind": "rule", "target": "c1_p0", "decision": "reject", "reason": "reviewer prefers merged stage", "expect": "conflict recorded; both rulings kept active pending resolution"},
    {"n": 9, "kind": "resolve", "target": "c1_p0", "reason": "panel keeps the rejection", "expect": "one ruling deactivated; event retained"},
    {"n": 10, "kind": "undo", "target": "kept ruling of step 9", "reason": "reopened after DTG zoom", "expect": "ruling deactivated by undo event; candidate returns to derived status"}
  ],
  "expectations": {
    "atmosphere_delay": {"note": "reaction windows start at valve time + 18s (later 30s); measured lag ~6K reported"},
    "overlap": {"note": "c1_p0/c1_p1 overlap and c1_m0 is the merged alternative; not exclusive percentage accounting"},
    "gap": {"note": "GAP at 1200s-1260s is never interpolated in TG or DTG"},
    "half_fail": {"status": "fragment rejected, layer unchanged", "affectsOnly": ["diagnostics"]}
  }
}
`

const replayDoc = `# 人读重放叙事（机器可读版见 manifest.json）

1. 导入 v1_bundle.txt：核对六层基线；接受系统产生的全部候选（含重叠峰的
   单段/合并两套合法解释）。
2. 参数修订：传输延迟 18 s → 30 s。只允许气氛反应窗口及依赖它的视图过期。
3. 源修订（局部依赖）：漂移速率 0.0004 → 0.0006 mg/s。只有校正质量、DTG、
   阶段与残留过期；阀门/炉程序节点身份不变。
4. 初重修订：10.000 → 10.012 mg。只有百分比变化，mg 与原始点不变。
5. 半失败导入：损坏的阀门片段被拒绝，原始片段与诊断保留，当前阀门层不变。
6. 混合导入：有效 Ar 阀门层被接受；未知仪器转储层被拒绝并存档。
7/8. 对 c1_p0 先接受后拒绝：冲突被记录，两个裁定都保留，不覆盖历史。
9. 冲突解决：保留拒绝；被放弃的裁定失活但留在历史里。
10. 撤销：撤销第 9 步保留的裁定，候选回到派生状态；撤销本身也是事件。

从最终状态可反查每条差异来自哪份材料（source id）、哪版规则
（derive-v1.0.0 + paramVersion）和哪次决定（ruling/resolve/undo event id）。
`
