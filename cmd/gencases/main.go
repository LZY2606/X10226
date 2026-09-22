// Command gencases regenerates the embedded offline case pack and the
// top-level casepack/ working copies. Output is deterministic: no random
// seeds, no host clock, no network.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const m0 = 38.0

func loss(t, c, l, k float64) float64 { return l / (1 + math.Exp(-k*(t-c))) }

func temperature(t float64) float64 {
	switch {
	case t < 5:
		return 30
	case t < 45:
		return 30 + (t-5)*10
	default:
		return 430
	}
}

func rawMass(t float64) float64 {
	m := m0
	m -= loss(t, 6.0, 1.2, 2.5)
	m -= loss(t, 18.0, 10.0, 1.0)
	m -= loss(t, 26.0, 14.0, 1.2)
	m -= loss(t, 40.0, 5.5, 1.0)
	return m
}

func main() {
	// regenerate from scratch so stale outputs never linger
	for _, d := range []string{"casepack/baseline", "casepack/mutations", "internal/casepack/data"} {
		if err := os.RemoveAll(d); err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			fatal(err)
		}
	}
	roots := []string{"casepack/baseline", "casepack/mutations"}
	emit := func(mut bool, name, body string) {
		dirs := append([]string{"internal/casepack/data"}, roots...)
		for i, r := range dirs {
			path := filepath.Join(r, name)
			if i > 0 && mut {
				path = filepath.Join(r, "mutations", name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				fatal(err)
			}
		}
	}

	emit(false, "samples.csv", samples(0.5, nil, nil))
	emit(false, "blank.csv", blank())
	emit(false, "drift.csv", drift(0.002, 0.0, 1))
	emit(false, "furnace.csv", furnace())
	emit(false, "valves.csv", valves())
	emit(false, "initial.csv",
		"t_min[min],initial_mg[mg],basis,note\n0,38.0000,balance_mg,initial weighing before furnace program\n")

	emit(true, "drift_v2.csv", drift(0.006, 0.05, 2))
	emit(true, "initial_v2.csv",
		"t_min[min],initial_mg[mg],basis,note\n0,38.5000,balance_mg,revised weighing after balance recalibration\n")
	emit(true, "samples_noise.csv", samples(0.5, map[float64]float64{18.5: 0.25, 19.0: 0.22}, nil))
	emit(true, "samples_gap.csv", samples(0.5, nil, func(t float64) bool { return t >= 28.0 && t <= 31.0 }))
	emit(true, "samples_partial_bundle.txt", partialBundle())

	emit(false, "MANIFEST.md", manifestMD())
	emit(false, "README.md", readme())
	fmt.Println("case pack generated")
}

func samples(step float64, bumps map[float64]float64, skip func(float64) bool) string {
	var b strings.Builder
	b.WriteString("t_min[min],mass_mg[mg],temp_c[degC]\n")
	for i := 0; i <= int(60/step); i++ {
		t := float64(i) * step
		if skip != nil && skip(t) {
			continue
		}
		m := rawMass(t)
		if add, ok := bumps[t]; ok {
			m += add
		}
		fmt.Fprintf(&b, "%.1f,%.4f,%.2f\n", t, m, temperature(t))
	}
	return b.String()
}

func partialBundle() string {
	var b strings.Builder
	b.WriteString("== file: samples_partial.csv ==\n")
	b.WriteString("t_min[min],mass_mg[mg],temp_c[degC],note\n")
	for i := 0; i <= 120; i++ {
		t := float64(i) * 0.5
		if t >= 28.0 && t <= 31.0 {
			continue
		}
		if i == 79 {
			fmt.Fprintf(&b, "%.1f,%s,%.2f,probe artifact kept verbatim\n", t, "N/A", temperature(t))
			continue
		}
		fmt.Fprintf(&b, "%.1f,%.4f,%.2f,\n", t, rawMass(t), temperature(t))
	}
	b.WriteString("\n== file: stray_notes.csv ==\n")
	b.WriteString("operator,shift,comment\nli,day,unrelated file that cannot map to a TGA layer\n")
	return b.String()
}

func blank() string {
	var b strings.Builder
	b.WriteString("t_min[min],blank_mg[mg]\n")
	for t := 0.0; t <= 60.0; t += 1 {
		fmt.Fprintf(&b, "%.0f,%.4f\n", t, 0.05+0.001*t)
	}
	return b.String()
}

func drift(slope, offset float64, version int) string {
	var b strings.Builder
	b.WriteString("t_min[min],drift_mg[mg],note\n")
	for t := 0.0; t <= 60.0; t += 4 {
		fmt.Fprintf(&b, "%.0f,%.4f,linear balance baseline drift v%d\n", t, offset+slope*t, version)
	}
	return b.String()
}

func furnace() string {
	return "t_min[min],setpoint_c[degC],phase\n" +
		"0,30,hold\n5,30,hold_end\n45,430,ramp_end\n60,430,final_hold\n"
}

func valves() string {
	return "t_min[min],gas,note\n" +
		"0,N2,start under nitrogen\n" +
		"32,Air,valve switches before oxidative burn-off; reaction responds later\n"
}

func manifestMD() string {
	return `# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

离线案例包：六类原始层 + 七个单一来源反例。坐标基准：炉时间
furnace_elapsed_min，质量 balance_mg，温度 sensor_degC。

## 基线叙事
1. 0-5 min 30 °C 恒温；5-45 min 10 °C/min 升至 430 °C；45-60 min 恒温。
2. 四个质量损失阶段：水分(约 6 min)、半纤维素样肩峰(约 18 min)、纤维素样
   主峰(约 26 min，与前一阶段在 20-24 min 重叠)、空气下氧化烧蚀(约 40 min)。
   m0 = 38.0000 mg，理论残留约 19.21%。
3. 阀门 32 min 切 Air，氧化反应约 34 min 才开始：阀时间 ≠ 反应时间。

## 机器可读规则（由派生层以 judgment 事件写出）
- atmosphere.delay.v1：阀切换时间 + 气体延迟 = 反应区时间。
- stages.independent_sums.v1：阶段可重叠，损耗百分比不强制和为 100。
- gap.no_interpolation.v1：采样缺口分段，不做跨缺口插值。
- noise.flag_dont_smooth.v1：非单调点保留并标记，不做单调平滑。
- residual.independent_chain.v1：残留率独立成链，不按阶段和反推。

## 反例（每次只改一个直接来源）
| 场景 | 来源 | 期望受影响的派生类 |
|---|---|---|
| atmosphere_delay | params.atmosphere | stages, valve_effective, program_sample_t |
| drift_baseline | mutations/drift_v2.csv | corrected_tg, dtg, stages, residuals |
| overlapping_peaks | params.peak (prominence 3.0) | stages |
| nonmonotonic_noise | mutations/samples_noise.csv | stages, residuals |
| sampling_gap | mutations/samples_gap.csv | corrected_tg, dtg, stages, residuals |
| initial_mass_revision | mutations/initial_v2.csv | stages, residuals, mass_fraction |
| partial_import | mutations/samples_partial_bundle.txt | 诊断 + corrected_tg 等；炉程序/阀不受影响 |
`
}

func readme() string {
	return `# baseline layer files
原始采样点 samples.csv；天平漂移校正 drift.csv；空白曲线 blank.csv；
炉程序 furnace.csv；气体阀事件 valves.csv；样品初重 initial.csv。
原始表示原样保留，派生层才做规范化。
`
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
