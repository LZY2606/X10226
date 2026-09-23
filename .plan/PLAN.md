# TGA 阶段分解/气氛事件证据链系统

## 架构
- Go module `tgaudit`，唯一依赖 modernc.org/sqlite（纯 Go，离线）
- 事件溯源：events 表为唯一事实源；派生结果带算法版本/参数指纹/直接来源快照，可重放
- 分层：domain(ingest/compute/stages/decision/project) / store(sqlite) / httpapi / web(SPA+SVG) / cmd/server
- 时间注入 Clock（虚拟时间），种子固定

## 步骤
1. go.mod、领域类型（带单位/坐标基准的 Quant、QTime）、ingest 解析器（6 层 + 半失败 + 原样保存）
2. compute：漂移校正/空白扣除/缺口/非单调标记/DTG（固定 seed 噪声）
3. stages：气氛延迟/温度滞后、重叠峰候选（含拒绝原因）、残留率与计算链
4. project 状态机：事件日志、版本/父版本、决策/冲突/撤销、过期局部失效、as-of 回放
5. SQLite store：项目/事件/实体持久化、导出/导入（身份/顺序/来源保持）
6. 离线案例包 casepack：初始 6 层 → 参数修订 → 局部源改动 → 冲突决定；覆盖 7 个验收情形
7. HTTP API + 审查页面（同步 TG/DTG、炉程序、气氛时间线、阶段候选、计算链、历史回放）
8. 自动化测试（ingest/compute/stages/project/store/http/casepack 端到端）
9. go build ./... 与 go test ./... -count=1 全绿；更新 README
