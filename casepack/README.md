# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链

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
