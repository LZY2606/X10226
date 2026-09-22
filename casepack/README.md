# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链（离线案例包）

所有样例由 `cmd/gencase` 以固定 seed 与固定时间原点确定性生成，不访问网络。
每行保留原始表示（数值+单位），派生层才规范化。

- `01_baseline/import.txt`：六层基线（原始采样点、天平漂移校正、炉程序、气体阀事件、空白曲线、样品初重）。
- `edge_cases/edge_drift.txt`：天平基线漂移加大。
- `edge_cases/edge_overlap.txt`：重叠峰（氧化峰向分解峰挤压）。
- `edge_cases/edge_nonmonotone.txt`：非单调噪声（正鼓包保留，DTG 降级）。
- `edge_cases/edge_gap.txt`：采样缺口 180–260s，不跨缺口插值/求导。
- `edge_cases/edge_initial_revision.txt`：初重修订（新版本，不覆盖旧值）。
- `edge_cases/edge_half_failure.txt`：导入半失败（program 层格式错误，兄弟层仍接受）。

领域判断：阀切换时间 != 样品反应时间（气体延迟+温度滞后）；阶段可重叠且不强制总和 100%；
缺口不跨越插值；无法唯一归属时保留全部合法候选。
