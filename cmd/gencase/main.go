// Command gencase writes the offline TGA case pack into casepack/.
//


package main

import (
	"os"
	"path/filepath"

	"tga/internal/synth"
)

func write(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(err)
	}
}

func main() {
	_, sampleText := synth.BaselineSamples()
	write("casepack/baseline/mass_samples.txt", sampleText)
	_, blankText := synth.BaselineBlank()
	write("casepack/baseline/blank_curve.txt", blankText)
	write("casepack/baseline/furnace_program.txt", synth.BaselineProgram())
	write("casepack/baseline/gas_valve_events.txt", synth.BaselineGas())
	write("casepack/baseline/initial_mass.txt", synth.BaselineM0())
	write("casepack/baseline/params.txt", synth.BaselineParams())

	write("casepack/scenarios/01_drift_blank.txt", synth.DriftBlank())
	_, overlap := synth.OverlapSamples()
	write("casepack/scenarios/02_overlap_peaks_samples.txt", overlap)
	_, nm := synth.NonMonotoneSamples()
	write("casepack/scenarios/03_nonmonotone_samples.txt", nm)
	_, gap := synth.GapSamples()
	write("casepack/scenarios/04_gap_samples.txt", gap)
	write("casepack/scenarios/05_revised_mass.txt", synth.RevisedM0())
	write("casepack/scenarios/06_partial_samples.txt", synth.PartialSamplesText())
	write("casepack/scenarios/07_delayed_params.txt", synth.DelayedParams())

	write("casepack/replay.txt", `# 离线案例重放脚本（失重谱系：TGA 阶段分解、气氛切换与残留物证据链）
# 每一步都是一个可审计事件；每列: seq 增量, 操作, 目标层/工件, 说明
1,import,baseline/*,初始六层基线（原始点/漂移/炉程序/气阀/空白/初重分层保存）
2,params,baseline/params.txt,算法参数版本1（tga-v1）
3,import,scenarios/07_delayed_params.txt,参数修订：气氛延迟 60->180s（仅使依赖延迟的工件过期）
4,import,scenarios/06_partial_samples.txt,导入半失败：两行不可转换，保留诊断
5,import,scenarios/04_gap_samples.txt,采样缺口：900..1080s 不插值
6,import,scenarios/03_nonmonotone_samples.txt,非单调噪声：400..440s 浮力回升
7,import,scenarios/02_overlap_peaks_samples.txt,重叠峰：阶段边界保留多候选
8,import,scenarios/01_drift_blank.txt,天平基线漂移：线性->二次（只影响校正及下游）
9,import,scenarios/05_revised_mass.txt,初重修订：10.0000->9.9800mg（父版本关系）
10,decide,stage_candidate,接受 唯一/首选 切分组合
11,decide,stage_candidate,否定 重叠峰的 d2zero 切分（冲突决定，写历史）
12,undo,seq=11,撤销否定：恢复当时依赖版本与候选
13,review,head,回到任意旧 seq 复核：显示当时结论，不用最新结果覆盖
14,export,-,导出档案；删除本地数据库后导入，身份/顺序/来源关系保持
`)
	write("casepack/README.md", `# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

本目录是**领域专用离线案例包**（固定 seed=20260919），不是通用表格。

## 基线 (baseline/)
- mass_samples.txt — 原始采样点（time,temp,mass，保留原始表示与单位）
- blank_curve.txt — 空白曲线（天平基线）
- furnace_program.txt — 炉程序（hold/ramp）
- gas_valve_events.txt — 气体阀事件（阀门时间，非反应时间）
- initial_mass.txt — 样品初重（独立称量层）
- params.txt — tga-v1 算法参数版本 1

## 反例 (scenarios/)
每个文件只改动一个直接来源：
1. 01_drift_blank — 天平基线漂移（线性→二次）
2. 02_overlap_peaks_samples — 重叠峰（多候选边界）
3. 03_nonmonotone_samples — 非单调噪声
4. 04_gap_samples — 采样缺口（禁止跨缺口插值）
5. 05_revised_mass — 初重修订（版本+父版本）
6. 06_partial_samples — 导入半失败（坏行保留诊断）
7. 07_delayed_params — 气氛延迟/温度滞后（阀切换≠反应时间）

## 重放 (replay.txt)
从 UI 的“载入离线案例”开始，逐步重放全部七种情形并包含
一次参数修订、一次局部源改动、一次互相冲突的人工决定与撤销。
`)
}
