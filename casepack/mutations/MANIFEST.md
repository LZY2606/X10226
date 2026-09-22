# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

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
